package githubapp

import (
	"context"

	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/dq"
	"github.com/ultraviolet-dev/ultraviolet/internal/impact"
	"github.com/ultraviolet-dev/ultraviolet/internal/lineage"
)

// Analyzer runs the real PR impact pipeline: fetch diff → detect schema changes
// → walk existing lineage for blast radius → cross-reference data-quality status
// → persist. It replaces the prior hardcoded `{drop_column, "unknown"}` stub.
type Analyzer struct {
	store  *Store
	client *Client
	impact *impact.Analyzer
	dq     *dq.Recorder
}

// PRReport is what the GitHub-side renderer needs.
type PRReport struct {
	Changes    []impact.Change
	Hits       []impact.Hit
	DQRefs     []DQRef
	Conclusion string
}

func NewAnalyzer(store *Store, client *Client, writer *lineage.Writer, dqRec *dq.Recorder) *Analyzer {
	return &Analyzer{store: store, client: client, impact: impact.New(writer), dq: dqRec}
}

// AnalyzePR fetches and analyzes a pull request, persists the result, and
// returns the report model. customerID must be the real, resolved tenant.
func (a *Analyzer) AnalyzePR(ctx context.Context, customerID uuid.UUID, installationID int64, repo string, pr int, headSHA, prURL string) (PRReport, error) {
	diff, err := a.client.PRDiff(ctx, installationID, repo, pr)
	if err != nil {
		return PRReport{}, err
	}
	files := ParseDiff(diff)
	changes := DetectChanges(files)

	// Blast radius: walk existing lineage for every detected change, merging by
	// downstream FQN (min hops; escalate to breaking severity if any path is).
	merged := map[string]impact.Hit{}
	for _, ch := range changes {
		hits, err := a.impact.Preview(ctx, customerID, ch, 5)
		if err != nil {
			return PRReport{}, err
		}
		for _, h := range hits {
			if prev, ok := merged[h.FQN]; ok {
				if h.Hops < prev.Hops {
					prev.Hops = h.Hops
				}
				if h.Severity == "breaking" {
					prev.Severity = "breaking"
				}
				merged[h.FQN] = prev
			} else {
				merged[h.FQN] = h
			}
		}
	}
	var hits []impact.Hit
	for _, h := range merged {
		hits = append(hits, h)
	}

	// Data-quality impact: latest DQ status for each impacted/changed table.
	tables := map[string]struct{}{}
	for _, h := range hits {
		tables[tableOf(h.FQN)] = struct{}{}
	}
	for _, ch := range changes {
		tables[tableOf(ch.FQN)] = struct{}{}
	}
	var dqRefs []DQRef
	for t := range tables {
		status, err := a.dq.LatestStatus(ctx, customerID, t)
		if err != nil || status == "" {
			continue
		}
		dqRefs = append(dqRefs, DQRef{TableFQN: t, Status: status})
	}

	conclusion := Conclusion(hits, dqRefs)

	if err := a.store.UpsertPRAnalysis(ctx, PRAnalysisRow{
		CustomerID:     customerID,
		InstallationID: installationID,
		RepoFullName:   repo,
		PRNumber:       pr,
		HeadSHA:        headSHA,
		PullRequestURL: prURL,
		Changes:        changes,
		Hits:           hits,
		DQRefs:         dqRefs,
		Conclusion:     conclusion,
	}); err != nil {
		return PRReport{}, err
	}

	return PRReport{Changes: changes, Hits: hits, DQRefs: dqRefs, Conclusion: conclusion}, nil
}
