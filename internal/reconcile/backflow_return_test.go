package reconcile

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"testing"

	"github.com/skaphos/oiax/v2/internal/engine"
)

// TestPlanBackflowReturnSurvivesPromotion is the regression for #85: under the
// cherry-pick strategy, a downstream-only commit X whose return X' has since
// promoted forward onto the backflow SOURCE must stay recognized as returned.
//
// Once X' is reachable from the source, merge-base(source, target) moves past
// it, so a scan bounded at that merge base — the pre-fix bound of both the
// provenance rung and the patch-id rung — no longer sees X'. X was then
// re-proposed every reconcile: its replay dropped as empty while the target
// still carried the content (masking the bug) and became a genuine conflict
// once the target moved on. The fix bounds both scans at the candidates'
// common ancestors instead, which X' — written after X — always sits above.
//
// Each case builds the scenario from the issue on a real repository: X on
// main (a backflow source), X' = cherry-pick of X onto development (the
// target), then, when promote is set, development promoted forward through
// test into main so X' is on both sides. The -x variants exercise the
// provenance rung, the plain ones the patch-id rung. The target-only variants
// are today's working case, guarding against regression. The later variants
// add a second candidate committed AFTER the promotion: it descends from X',
// which is what makes a bound at the candidates' union of ancestry
// (target --not X Y) wrong — that walk would exclude X' as well.
func TestPlanBackflowReturnSurvivesPromotion(t *testing.T) {
	tests := []struct {
		name string
		// provenance replays X with cherry-pick -x, so the twin names X; without
		// it only the patch-id can match.
		provenance bool
		// promote merges the twin forward onto main, so it is reachable from
		// the source and merge-base(main, development) moves past it.
		promote bool
		// later adds one more hotfix on main after the promotion: a second,
		// still-unreturned candidate that descends from the twin.
		later      bool
		wantReason engine.BackflowExclusionReason
	}{
		{name: "twin target-only/provenance", provenance: true, wantReason: engine.BackflowExcludedProvenance},
		{name: "twin target-only/patch-id", wantReason: engine.BackflowExcludedPatchID},
		{name: "twin promoted onto source/provenance", provenance: true, promote: true, wantReason: engine.BackflowExcludedProvenance},
		{name: "twin promoted onto source/patch-id", promote: true, wantReason: engine.BackflowExcludedPatchID},
		{name: "later candidate descends from promoted twin/provenance", provenance: true, promote: true, later: true, wantReason: engine.BackflowExcludedProvenance},
		{name: "later candidate descends from promoted twin/patch-id", promote: true, later: true, wantReason: engine.BackflowExcludedPatchID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, commit := gitHarness(t)

			checkout(t, r, "main")
			hotfix := commit("hotfix.txt", "urgent\n", "hotfix on main")

			// An unrelated commit first, so the twin lands on a distinct parent
			// and carries a SHA of its own.
			checkout(t, r, "development")
			commit("dev.txt", "dev work\n", "unrelated dev commit")
			args := []string{"cherry-pick"}
			if tc.provenance {
				args = append(args, "-x")
			}
			gitExec(t, r.Dir, append(args, hotfix)...)
			twin := gitExec(t, r.Dir, "rev-parse", "HEAD")

			var later string
			if tc.promote {
				// Promote the twin forward along the graph: development into
				// test (fast-forward), then test into main by merge commit.
				checkout(t, r, "test")
				gitExec(t, r.Dir, "merge", "-q", "--ff-only", "development")
				checkout(t, r, "main")
				gitExec(t, r.Dir, "merge", "-q", "--no-ff", "-m", "promote test into main", "test")
				// The issue's precondition: the twin is now shared history — it
				// IS the merge base, so the pre-fix target-only range excludes it.
				if mb := gitExec(t, r.Dir, "merge-base", "main", "development"); mb != twin {
					t.Fatalf("merge-base(main, development) = %s, want the twin %s", mb, twin)
				}
				if tc.later {
					later = commit("later.txt", "later\n", "later hotfix on main")
					// And the later candidate descends from the twin (a non-zero
					// exit fails the test).
					gitExec(t, r.Dir, "merge-base", "--is-ancestor", twin, later)
				}
			}

			var buf bytes.Buffer
			c := &Coordinator{Git: r, Forge: &fakeForge{}, Graph: testGraph(),
				Log: slog.New(slog.NewJSONHandler(&buf, nil))}
			plan, err := c.Plan(context.Background())
			if err != nil {
				t.Fatalf("plan: %v", err)
			}

			// The hotfix is excluded, and the diagnostics name the rung.
			assertExclusionReason(t, plan, hotfix, tc.wantReason)

			// Only the later candidate (when present) is still to return.
			var wantReturn []string
			if later != "" {
				wantReturn = []string{later}
			}
			var backflow int
			for _, a := range plan.Actions {
				if a.Type == engine.ActionCreateBackflowRequest {
					backflow++
				}
			}
			if backflow != len(wantReturn) {
				t.Errorf("planned %d backflow actions, want %d: %+v", backflow, len(wantReturn), plan.Actions)
			}
			st, err := c.backflowActionState(context.Background(), engine.Action{
				Type: engine.ActionCreateBackflowRequest, From: "main", To: "development",
			})
			if err != nil {
				t.Fatalf("backflowActionState: %v", err)
			}
			var got []string
			for _, cm := range st.ToReturn {
				got = append(got, cm.SHA)
			}
			if !slices.Equal(got, wantReturn) {
				t.Errorf("ToReturn = %v, want %v", got, wantReturn)
			}

			// The run-level counters agree with the per-edge diagnostics.
			want := map[string]float64{
				"toReturn":           float64(len(wantReturn)),
				"excludedProvenance": 0,
				"excludedPatchID":    0,
			}
			switch tc.wantReason {
			case engine.BackflowExcludedProvenance:
				want["excludedProvenance"] = 1
			case engine.BackflowExcludedPatchID:
				want["excludedPatchID"] = 1
			}
			rec := findLogRecord(t, &buf, "plan built")
			counts, _ := rec["backflow"].(map[string]any)
			for k, v := range want {
				if counts[k] != v {
					t.Errorf("plan built backflow.%s = %v, want %v (record: %v)", k, counts[k], v, counts)
				}
			}
		})
	}
}
