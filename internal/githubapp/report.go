package githubapp

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ultraviolet-dev/ultraviolet/internal/impact"
)

// CommentMarker is a hidden HTML comment so we find + edit our own comment on
// re-runs instead of appending a new one each push.
const CommentMarker = "<!-- uv-lineage-bot:v1 -->"

// DQRef links an impacted table to its latest data-quality status. This is what
// makes the report "impact on DATA QUALITY" rather than a bare dependency walk.
type DQRef struct {
	TableFQN string `json:"table_fqn"`
	Status   string `json:"status"` // pass|fail|skip|error
}

// Conclusion derives the Check Run conclusion from the change kind (severity),
// the blast radius, and data-quality status — not a substring scan.
func Conclusion(hits []impact.Hit, dq []DQRef) string {
	for _, d := range dq {
		if d.Status == "fail" || d.Status == "error" {
			return "action_required"
		}
	}
	breaking := false
	for _, h := range hits {
		if h.Severity == "breaking" {
			breaking = true
		}
	}
	switch {
	case len(hits) == 0:
		return "success"
	case breaking || len(hits) >= 3:
		return "action_required"
	default:
		return "neutral"
	}
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// RenderComment builds the PR comment markdown.
func RenderComment(changes []impact.Change, hits []impact.Hit, dq []DQRef, sha string) string {
	var b strings.Builder
	b.WriteString(CommentMarker + "\n")
	b.WriteString("## 🔎 Ultraviolet impact analysis\n\n")
	if sha != "" {
		b.WriteString(fmt.Sprintf("_Last analyzed commit: `%s`_\n\n", shortSHA(sha)))
	}
	if len(changes) > 0 {
		b.WriteString("**Detected changes**\n\n")
		for _, c := range changes {
			b.WriteString(fmt.Sprintf("- `%s` — %s\n", c.FQN, c.Kind))
		}
		b.WriteString("\n")
	}
	dqByTable := map[string]string{}
	for _, d := range dq {
		dqByTable[d.TableFQN] = d.Status
	}
	if len(hits) == 0 {
		b.WriteString("✅ **No downstream consumers detected.** Safe to merge.\n")
	} else {
		b.WriteString(fmt.Sprintf("⚠ This change touches **%d** downstream node(s):\n\n", len(hits)))
		b.WriteString("| Downstream | Hops | DQ status | Severity |\n|---|---|---|---|\n")
		for _, h := range hits {
			status := dqByTable[tableOf(h.FQN)]
			if status == "" {
				status = "—"
			}
			b.WriteString(fmt.Sprintf("| `%s` | %d | %s | %s |\n", h.FQN, h.Hops, dqEmoji(status), h.Severity))
		}
	}
	failing := dqFailures(dq)
	if len(failing) > 0 {
		b.WriteString("\n**Data-quality impact** — impacted tables with failing tests:\n\n")
		for _, d := range failing {
			b.WriteString(fmt.Sprintf("- `%s` → **%s**\n", d.TableFQN, d.Status))
		}
	}
	b.WriteString("\n---\n")
	b.WriteString("<sub>Rewritten on every push. Reply `/uv suppress` to silence this PR.</sub>\n")
	return b.String()
}

func dqFailures(dq []DQRef) []DQRef {
	var out []DQRef
	for _, d := range dq {
		if d.Status == "fail" || d.Status == "error" {
			out = append(out, d)
		}
	}
	return out
}

func dqEmoji(status string) string {
	switch status {
	case "fail", "error":
		return "❌ " + status
	case "pass":
		return "✅ pass"
	case "—", "":
		return "—"
	default:
		return status
	}
}

// tableOf trims a column FQN (schema.table.col) to its table (schema.table) so
// a column-level hit can be matched against table-keyed DQ results.
func tableOf(fqn string) string {
	parts := strings.Split(fqn, ".")
	if len(parts) >= 3 {
		return strings.Join(parts[:len(parts)-1], ".")
	}
	return fqn
}

// CheckRunPayload builds the Check Run create body.
func CheckRunPayload(changes []impact.Change, hits []impact.Hit, dq []DQRef, sha string) []byte {
	summary := fmt.Sprintf("%d downstream node(s) impacted.", len(hits))
	if len(hits) == 0 {
		summary = "No downstream consumers detected."
	}
	payload, _ := json.Marshal(map[string]any{
		"name":         "Ultraviolet impact analysis",
		"head_sha":     sha,
		"status":       "completed",
		"conclusion":   Conclusion(hits, dq),
		"completed_at": time.Now().UTC().Format(time.RFC3339),
		"output": map[string]string{
			"title":   "Ultraviolet impact analysis",
			"summary": summary,
			"text":    RenderComment(changes, hits, dq, sha),
		},
	})
	return payload
}
