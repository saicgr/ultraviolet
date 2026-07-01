package githubapp

import (
	"sort"
	"strings"
	"testing"
)

func changeStrings(cs []impactChangeLike) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.kind+":"+c.fqn)
	}
	sort.Strings(out)
	return out
}

// impactChangeLike avoids importing impact into the test's assertion shape.
type impactChangeLike struct{ kind, fqn string }

func TestDetectChanges(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/migrations/0009.sql b/migrations/0009.sql",
		"--- a/migrations/0009.sql",
		"+++ b/migrations/0009.sql",
		"@@ -1 +1,2 @@",
		"+ALTER TABLE analytics.orders DROP COLUMN IF EXISTS legacy_total;",
		"diff --git a/models/marts/old_model.sql b/models/marts/old_model.sql",
		"deleted file mode 100644",
		"--- a/models/marts/old_model.sql",
		"+++ /dev/null",
		"-SELECT 1",
		"diff --git a/models/marts/a.sql b/models/marts/b.sql",
		"similarity index 90%",
		"rename from models/marts/a.sql",
		"rename to models/marts/b.sql",
	}, "\n")

	got := []impactChangeLike{}
	for _, c := range DetectChanges(ParseDiff(diff)) {
		got = append(got, impactChangeLike{kind: c.Kind, fqn: c.FQN})
	}
	want := []string{
		"drop_column:analytics.orders.legacy_total",
		"drop_table:old_model",
		"rename_table:a",
	}
	if strings.Join(changeStrings(got), "|") != strings.Join(want, "|") {
		t.Fatalf("changes mismatch\n got: %v\nwant: %v", changeStrings(got), want)
	}
}

func TestConclusion(t *testing.T) {
	// A failing DQ test on an impacted table forces action_required.
	if c := Conclusion(nil, []DQRef{{TableFQN: "x", Status: "fail"}}); c != "action_required" {
		t.Errorf("failing DQ → got %q", c)
	}
	if c := Conclusion(nil, nil); c != "success" {
		t.Errorf("no hits → got %q", c)
	}
}
