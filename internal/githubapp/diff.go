package githubapp

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ultraviolet-dev/ultraviolet/internal/impact"
)

// dropColRe matches ALTER TABLE … DROP COLUMN, tolerating IF EXISTS and quoted
// identifiers. This is the previously-dead regex (cmd/lineage-bot/main.go),
// widened and actually wired into change detection.
var dropColRe = regexp.MustCompile(`(?i)ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?` + "`?" + `"?([\w.]+)` + "`?" + `"?\s+DROP\s+COLUMN\s+(?:IF\s+EXISTS\s+)?` + "`?" + `"?(\w+)`)

// FileChange is one file's delta in a PR diff.
type FileChange struct {
	Path    string // new path
	OldPath string // pre-image path (for renames)
	Status  string // "added" | "modified" | "removed" | "renamed"
	Added   []string
	Removed []string
}

// ParseDiff parses a unified `git diff` into per-file changes.
func ParseDiff(diff string) []FileChange {
	var files []FileChange
	var cur *FileChange
	flush := func() {
		if cur != nil {
			files = append(files, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			cur = &FileChange{Status: "modified"}
			// "diff --git a/x b/y" — capture both paths.
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				cur.OldPath = strings.TrimPrefix(parts[2], "a/")
				cur.Path = strings.TrimPrefix(parts[3], "b/")
			}
		case cur == nil:
			continue
		case strings.HasPrefix(line, "new file mode"):
			cur.Status = "added"
		case strings.HasPrefix(line, "deleted file mode"):
			cur.Status = "removed"
		case strings.HasPrefix(line, "rename from "):
			cur.Status = "renamed"
			cur.OldPath = strings.TrimPrefix(line, "rename from ")
		case strings.HasPrefix(line, "rename to "):
			cur.Path = strings.TrimPrefix(line, "rename to ")
		case strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "--- "):
			// path markers already captured from the diff header
		case strings.HasPrefix(line, "+"):
			cur.Added = append(cur.Added, line[1:])
		case strings.HasPrefix(line, "-"):
			cur.Removed = append(cur.Removed, line[1:])
		}
	}
	flush()
	return files
}

// DetectChanges turns parsed file changes into the schema changes an impact
// walk can act on: dropped columns (regex), removed model files (drop_table),
// renamed models (rename_table). SELECT-projection changes in modified models
// are computed separately in analysis.go (it needs base+head blobs).
func DetectChanges(files []FileChange) []impact.Change {
	var out []impact.Change
	for _, f := range files {
		if !isSQL(f.Path) && !isSQL(f.OldPath) {
			continue
		}
		// DROP COLUMN appearing in the added side of any .sql (migration/model).
		for _, ln := range f.Added {
			for _, m := range dropColRe.FindAllStringSubmatch(ln, -1) {
				out = append(out, impact.Change{Kind: "drop_column", FQN: m[1] + "." + m[2], Detail: f.Path})
			}
		}
		switch f.Status {
		case "removed":
			if isModel(f.Path) {
				out = append(out, impact.Change{Kind: "drop_table", FQN: modelName(f.Path), Detail: f.Path})
			}
		case "renamed":
			if isModel(f.OldPath) {
				out = append(out, impact.Change{Kind: "rename_table", FQN: modelName(f.OldPath), Detail: f.Path})
			}
		}
	}
	return out
}

func isSQL(p string) bool { return strings.EqualFold(filepath.Ext(p), ".sql") }

// isModel is a dbt model file (under a models/ dir), distinct from a migration.
func isModel(p string) bool {
	return isSQL(p) && strings.Contains("/"+strings.ToLower(p), "/models/")
}

func modelName(p string) string {
	base := filepath.Base(p)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
