// Package forgetest is a shared, provider-agnostic conformance suite for
// forge.Forge implementations. Each provider supplies a factory that returns a
// fresh forge.Forge backed by a stateful in-memory fake of its own REST API;
// Run drives the identical battery of scenarios through the interface, so the
// GitHub and Azure DevOps providers are held to the same observable behavior:
// create, adopt a duplicate, update, close, list filtering, conflict-artifact
// ordering, and the oiax/ namespace refusal.
//
// Assertions are made through the forge.Forge interface alone — never against a
// provider's wire format — which is what makes them portable across providers.
// Wire-level details (auth scheme, marker storage, escaping) stay in each
// provider's own tests; this suite pins the contract they must agree on.
//
// It follows the testing/fstest pattern: a normal package that imports testing,
// imported only from _test.go files, so it never enters a production build.
package forgetest

import (
	"context"
	"sort"
	"strconv"
	"testing"

	"github.com/skaphos/oiax/internal/engine"
	"github.com/skaphos/oiax/internal/forge"
)

// Run executes the conformance battery against providers produced by newSubject.
// newSubject must return a forge.Forge whose backend starts empty; it is called
// once per subtest so scenarios never share state.
func Run(t *testing.T, newSubject func(t *testing.T) forge.Forge) {
	t.Helper()
	t.Run("PromotionRequestLifecycle", func(t *testing.T) { testPromotionLifecycle(t, newSubject(t)) })
	t.Run("AdoptsDuplicateCreate", func(t *testing.T) { testAdoptDuplicate(t, newSubject(t)) })
	t.Run("ListFiltersGraphAndType", func(t *testing.T) { testListFilters(t, newSubject(t)) })
	t.Run("ConflictArtifactLifecycle", func(t *testing.T) { testConflictLifecycle(t, newSubject(t)) })
	t.Run("NamespaceRefusal", func(t *testing.T) { testNamespaceRefusal(t, newSubject(t)) })
}

// testPromotionLifecycle: a request round-trips through create → list → update →
// close, and a closed request drops out of open discovery.
func testPromotionLifecycle(t *testing.T, f forge.Forge) {
	ctx := context.Background()
	cr := mustCreate(t, f, "envs", engine.RequestTypePromotion, "dev", "staging", "head1")
	if cr.ID == "" || cr.Source != "dev" || cr.Target != "staging" || cr.SourceHead != "head1" ||
		cr.Type != engine.RequestTypePromotion {
		t.Fatalf("CreateRequest returned %+v, want a well-formed promotion request", cr)
	}

	got := mustList(t, f, "envs", "")
	if len(got) != 1 || got[0].ID != cr.ID || got[0].SourceHead != "head1" {
		t.Fatalf("after create, list = %+v, want the one created request", got)
	}

	if err := f.UpdateRequest(ctx, forge.UpdateRequest{ID: forge.RequestID(cr.ID), SourceHead: "head2"}); err != nil {
		t.Fatalf("UpdateRequest: %v", err)
	}
	got = mustList(t, f, "envs", "")
	if len(got) != 1 || got[0].SourceHead != "head2" {
		t.Fatalf("after update, list = %+v, want sourceHead head2", got)
	}

	if err := f.CloseRequest(ctx, forge.RequestID(cr.ID), forge.Reason{Summary: "obsolete"}); err != nil {
		t.Fatalf("CloseRequest: %v", err)
	}
	if got = mustList(t, f, "envs", ""); len(got) != 0 {
		t.Fatalf("after close, open discovery = %+v, want empty (a closed request must not appear)", got)
	}
}

// testAdoptDuplicate: creating an equivalent request a second time adopts the
// surviving one (same id) rather than opening a duplicate — the forge is the
// concurrency arbiter for promotion requests.
func testAdoptDuplicate(t *testing.T, f forge.Forge) {
	cr1 := mustCreate(t, f, "envs", engine.RequestTypePromotion, "dev", "staging", "head1")
	cr2 := mustCreate(t, f, "envs", engine.RequestTypePromotion, "dev", "staging", "head1")
	if cr2.ID != cr1.ID {
		t.Fatalf("second create returned id %q, want the adopted id %q", cr2.ID, cr1.ID)
	}
	if got := mustList(t, f, "envs", ""); len(got) != 1 {
		t.Fatalf("after a duplicate create, list = %+v, want exactly one request", got)
	}
}

// testListFilters: discovery is scoped to the requested graph and, when set, the
// requested type; requests for other graphs or types are never returned.
func testListFilters(t *testing.T, f forge.Forge) {
	aProm := mustCreate(t, f, "A", engine.RequestTypePromotion, "dev", "staging", "h")
	aBack := mustCreate(t, f, "A", engine.RequestTypeBackflow, "oiax/backflow/x", "main", "h")
	bProm := mustCreate(t, f, "B", engine.RequestTypePromotion, "dev", "prod", "h")

	onlyAProm := mustList(t, f, "A", engine.RequestTypePromotion)
	if len(onlyAProm) != 1 || onlyAProm[0].ID != aProm.ID {
		t.Fatalf("list(A, promotion) = %+v, want only the A promotion", onlyAProm)
	}
	allA := mustList(t, f, "A", "")
	if len(allA) != 2 || !containsID(allA, aProm.ID) || !containsID(allA, aBack.ID) {
		t.Fatalf("list(A) = %+v, want both A requests", allA)
	}
	onlyB := mustList(t, f, "B", "")
	if len(onlyB) != 1 || onlyB[0].ID != bProm.ID {
		t.Fatalf("list(B) = %+v, want only the B request", onlyB)
	}
}

// testConflictLifecycle: durable conflict artifacts create, list ascending by id
// (deterministic canonical order), update in place, and close out of the list.
func testConflictLifecycle(t *testing.T, f forge.Forge) {
	ctx := context.Background()
	a1 := mustCreateArtifact(t, f, "g", "dev", "main", "h1")
	a2 := mustCreateArtifact(t, f, "g", "dev", "main", "h2")
	a3 := mustCreateArtifact(t, f, "g", "dev", "main", "h3")

	arts := mustListArtifacts(t, f, "g")
	if len(arts) != 3 {
		t.Fatalf("list artifacts = %d, want 3", len(arts))
	}
	assertAscending(t, arts)

	if err := f.UpdateConflictArtifact(ctx, a2.ID, forge.ConflictArtifactSpec{
		Graph: "g", Source: "dev", Target: "main", SourceHead: "h2-new", Title: "t", Body: "b",
	}); err != nil {
		t.Fatalf("UpdateConflictArtifact: %v", err)
	}
	arts = mustListArtifacts(t, f, "g")
	if got := findArtifact(arts, a2.ID); got == nil || got.SourceHead != "h2-new" {
		t.Fatalf("after update, artifact %s = %+v, want sourceHead h2-new", a2.ID, got)
	}

	if err := f.CloseConflictArtifact(ctx, a1.ID, forge.Reason{Summary: "resolved"}); err != nil {
		t.Fatalf("CloseConflictArtifact: %v", err)
	}
	arts = mustListArtifacts(t, f, "g")
	if len(arts) != 2 || findArtifact(arts, a1.ID) != nil {
		t.Fatalf("after close, list = %+v, want the closed artifact %s gone", arts, a1.ID)
	}
	assertAscending(t, arts)
	_ = a3
}

// testNamespaceRefusal: mutation of any ref outside the oiax/ namespace is
// refused before the forge is touched.
func testNamespaceRefusal(t *testing.T, f forge.Forge) {
	ctx := context.Background()
	if err := f.PushBranch(ctx, forge.BranchPush{Name: "main", SHA: "abc1234", Force: true}); err == nil {
		t.Error("PushBranch(main) must be refused outside the oiax/ namespace")
	}
	if err := f.DeleteBranch(ctx, "release/1.0"); err == nil {
		t.Error("DeleteBranch(release/1.0) must be refused outside the oiax/ namespace")
	}
}

func mustCreate(t *testing.T, f forge.Forge, graph string, typ engine.RequestType, source, target, head string) engine.ChangeRequest {
	t.Helper()
	cr, err := f.CreateRequest(context.Background(), forge.CreateRequest{
		Graph: graph, Type: typ, Source: source, Target: target, SourceHead: head,
		Title: "title", Body: "Automated.",
	})
	if err != nil {
		t.Fatalf("CreateRequest(%s %s→%s): %v", graph, source, target, err)
	}
	return cr
}

func mustList(t *testing.T, f forge.Forge, graph string, typ engine.RequestType) []engine.ChangeRequest {
	t.Helper()
	got, err := f.ListManagedRequests(context.Background(), forge.RequestFilter{Graph: graph, Type: typ})
	if err != nil {
		t.Fatalf("ListManagedRequests(%s, %q): %v", graph, typ, err)
	}
	return got
}

func mustCreateArtifact(t *testing.T, f forge.Forge, graph, source, target, head string) forge.ConflictArtifact {
	t.Helper()
	art, err := f.CreateConflictArtifact(context.Background(), forge.ConflictArtifactSpec{
		Graph: graph, Source: source, Target: target, SourceHead: head, Title: "Conflict", Body: "details",
	})
	if err != nil {
		t.Fatalf("CreateConflictArtifact(%s %s→%s): %v", graph, source, target, err)
	}
	return art
}

func mustListArtifacts(t *testing.T, f forge.Forge, graph string) []forge.ConflictArtifact {
	t.Helper()
	got, err := f.ListConflictArtifacts(context.Background(), graph)
	if err != nil {
		t.Fatalf("ListConflictArtifacts(%s): %v", graph, err)
	}
	return got
}

func containsID(reqs []engine.ChangeRequest, id string) bool {
	for _, r := range reqs {
		if r.ID == id {
			return true
		}
	}
	return false
}

func findArtifact(arts []forge.ConflictArtifact, id forge.ConflictArtifactID) *forge.ConflictArtifact {
	for i := range arts {
		if arts[i].ID == id {
			return &arts[i]
		}
	}
	return nil
}

// assertAscending fails when the artifacts are not in ascending numeric-id
// order, the deterministic canonical order forge.Forge requires so the reconcile
// layer's lowest-id duplicate-consolidation rule is stable.
func assertAscending(t *testing.T, arts []forge.ConflictArtifact) {
	t.Helper()
	ids := make([]int, len(arts))
	for i, a := range arts {
		n, err := strconv.Atoi(string(a.ID))
		if err != nil {
			t.Fatalf("artifact id %q is not numeric: %v", a.ID, err)
		}
		ids[i] = n
	}
	if !sort.IntsAreSorted(ids) {
		t.Fatalf("conflict artifacts are not sorted ascending by id: %v", ids)
	}
}
