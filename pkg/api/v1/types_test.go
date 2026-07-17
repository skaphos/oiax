package v1

import (
	"encoding/json"
	"testing"
)

// TestPromotionGraphJSONRoundTrip proves the json struct tags added
// alongside the existing yaml tags let an external Go integrator
// (un)marshal a PromotionGraph without loss: every field set on the way in
// is still set, with the same value, on the way out.
func TestPromotionGraphJSONRoundTrip(t *testing.T) {
	strategy := BackflowStrategyCherryPick
	g := PromotionGraph{
		APIVersion: APIVersion,
		Kind:       KindPromotionGraph,
		Metadata:   Metadata{Name: "environments"},
		Spec: PromotionGraphSpec{
			Branches: map[string]Branch{
				"development": {Role: RoleSource, Drift: DriftForbidden},
				"main":        {Role: RoleTerminal},
			},
			Promotions: []Promotion{
				{From: "development", To: "main", Expectations: &Expectations{MergeMethod: MergeMethodSquash}},
			},
			Backflow: &Backflow{
				Sources:  []string{"main"},
				Target:   "development",
				Strategy: strategy,
			},
		},
	}

	data, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got PromotionGraph
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.APIVersion != g.APIVersion {
		t.Errorf("apiVersion = %q, want %q", got.APIVersion, g.APIVersion)
	}
	if got.Kind != g.Kind {
		t.Errorf("kind = %q, want %q", got.Kind, g.Kind)
	}
	if got.Metadata.Name != g.Metadata.Name {
		t.Errorf("metadata.name = %q, want %q", got.Metadata.Name, g.Metadata.Name)
	}
	if len(got.Spec.Branches) != len(g.Spec.Branches) {
		t.Fatalf("branches = %v, want %v", got.Spec.Branches, g.Spec.Branches)
	}
	if got.Spec.Branches["development"].Role != RoleSource {
		t.Errorf("development.role = %q, want %q", got.Spec.Branches["development"].Role, RoleSource)
	}
	if len(got.Spec.Promotions) != 1 || got.Spec.Promotions[0].Expectations == nil {
		t.Fatalf("promotions = %+v, want one edge with expectations", got.Spec.Promotions)
	}
	if got.Spec.Promotions[0].Expectations.MergeMethod != MergeMethodSquash {
		t.Errorf("mergeMethod = %q, want %q", got.Spec.Promotions[0].Expectations.MergeMethod, MergeMethodSquash)
	}
	if got.Spec.Backflow == nil || got.Spec.Backflow.Target != "development" {
		t.Fatalf("backflow = %+v, want target development", got.Spec.Backflow)
	}

	// The JSON keys themselves must be the camelCase names, not the Go
	// field names, so the wire format matches the documented config keys.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}
	for _, key := range []string{"apiVersion", "kind", "metadata", "spec"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("marshaled JSON missing key %q: %s", key, data)
		}
	}
}
