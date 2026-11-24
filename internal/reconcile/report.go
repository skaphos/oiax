package reconcile

import "github.com/skaphos/oiax/v2/internal/forge"

// Divergence reports (spec 002): what each edge that contributed to exit 3
// needs the CLI to tell the operator. Reports are not serialized; the JSON
// plan, request metadata, and exit codes are unchanged.

// DivergenceKind classifies a DivergenceReport.
type DivergenceKind int

const (
	// KindConflict is a backflow replay that conflicted.
	KindConflict DivergenceKind = iota + 1
	// KindOther is any other reported divergence (for example a
	// merge-strategy fence). It is named only in the exit-3 output.
	KindOther
)

// DivergenceReport is one edge that contributed to exit 3, appended to
// Result.Divergences in plan action order. There is at most one report per
// edge per run; a conflict wins over another divergence on the same edge.
type DivergenceReport struct {
	// From and To are the edge (validated, trusted branch names).
	From, To string
	// Kind classifies the divergence.
	Kind DivergenceKind
	// Conflict is set only for KindConflict.
	Conflict *ConflictGuidance
}

// ArtifactStatus is what happened to the durable conflict artifact this run.
type ArtifactStatus int

const (
	// ArtifactKnown means the artifact exists and its address is known.
	ArtifactKnown ArtifactStatus = iota + 1
	// ArtifactUnhostable means the repository cannot host artifacts (GitHub
	// with Issues disabled): the run's summary is the only guidance.
	ArtifactUnhostable
	// ArtifactNotRecorded means the artifact was not recorded or confirmed
	// this run; the next run retries.
	ArtifactNotRecorded
)

// addReport appends r, keeping at most one report per edge: a conflict
// replaces another divergence on the same edge, and a later report never
// replaces a conflict. Order is first appearance, which is plan order.
func (res *Result) addReport(r DivergenceReport) {
	for i, have := range res.Divergences {
		if have.From != r.From || have.To != r.To {
			continue
		}
		if r.Kind == KindConflict && have.Kind != KindConflict {
			res.Divergences[i] = r
		}
		return
	}
	res.Divergences = append(res.Divergences, r)
}

// repointConflictArtifacts points each conflict report at its edge's
// canonical artifact: a known report follows the canonical when
// consolidation closed the one it named, and a not-recorded report becomes
// known when a later list found the edge's artifact (an earlier list or
// create failed this run). An unhostable report is left alone: the forge
// said it cannot host artifacts, so an artifact found anyway is stale.
func (res *Result) repointConflictArtifacts(canon map[[2]string]forge.ConflictArtifact) {
	for _, r := range res.Divergences {
		g := r.Conflict
		if g == nil || (g.Artifact != ArtifactKnown && g.Artifact != ArtifactNotRecorded) {
			continue
		}
		if art, ok := canon[[2]string{r.From, r.To}]; ok && string(art.ID) != g.ArtifactID {
			g.Artifact = ArtifactKnown
			g.ArtifactID = string(art.ID)
			g.ArtifactURL = art.URL
		}
	}
}

// ConflictGuidance is what the CLI needs to print one conflict's warning,
// summary section, and exit-3 line.
type ConflictGuidance struct {
	// Strategy is "cherry-pick" or "merge".
	Strategy string
	// SHA is the failing commit (cherry-pick) or source head (merge).
	SHA string
	// Subject is the failing commit's subject after textsafe.Subject.
	// Still untrusted: each surface applies its own function on output.
	Subject string
	// Applied counts commits cherry-picked cleanly before the conflict.
	Applied int
	// Whole is true for the merge strategy.
	Whole bool
	// Artifact is the artifact's status; ArtifactID and ArtifactURL are set
	// only when it is ArtifactKnown. The URL is built by the provider from
	// configured identity, never from forge text.
	Artifact    ArtifactStatus
	ArtifactID  string
	ArtifactURL string
	// ArtifactNoun is "issue" or "work item", from the forge kind.
	ArtifactNoun string
	// SummaryBody is the guidance body rendered for the summary surface, or
	// empty when it could not be rendered (RenderFailed).
	SummaryBody string
	// RenderFailed is true when the summary-surface render failed.
	RenderFailed bool
	// BuiltinFallback is true when the configured conflict template reads
	// commit text, so SummaryBody is the built-in body.
	BuiltinFallback bool
}
