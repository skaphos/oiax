package reconcile

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/skaphos/oiax/internal/engine"
)

func samplePlan() engine.Plan {
	return engine.Plan{
		PlanFormatVersion: engine.PlanFormatVersion,
		Graph:             "environments",
		Actions: []engine.Action{
			{
				Type: engine.ActionCreatePromotionRequest, From: "development", To: "test",
				Unpromoted: 3, Equivalence: engine.EquivalenceReachability,
				Reason: "3 unpromoted commits and no managed promotion request",
			},
			{
				Type: engine.ActionReportDivergence, From: "test", To: "main",
				Unpromoted: 1, Reason: "main has 1 commits not represented in test",
			},
		},
	}
}

func TestRenderJSONIsPlanFormatVersion1(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderJSON(&buf, samplePlan()); err != nil {
		t.Fatalf("render json: %v", err)
	}
	var decoded struct {
		PlanFormatVersion int `json:"planFormatVersion"`
		Actions           []struct {
			Type string `json:"type"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	if decoded.PlanFormatVersion != 1 {
		t.Errorf("planFormatVersion = %d, want 1", decoded.PlanFormatVersion)
	}
	if len(decoded.Actions) != 2 || decoded.Actions[0].Type != "createPromotionRequest" {
		t.Errorf("actions = %+v", decoded.Actions)
	}
	if !strings.Contains(buf.String(), "\n  ") {
		t.Error("expected indented JSON")
	}
}

func TestRenderTextEmptyPlan(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderText(&buf, engine.Plan{Graph: "environments"}); err != nil {
		t.Fatalf("render text: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Promotion graph: environments") || !strings.Contains(out, "In sync, no actions.") {
		t.Errorf("text = %q", out)
	}
}

func TestRenderTextListsActions(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderText(&buf, samplePlan()); err != nil {
		t.Fatalf("render text: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"create", "development -> test", "report", "test -> main"} {
		if !strings.Contains(out, want) {
			t.Errorf("text missing %q:\n%s", want, out)
		}
	}
}

func TestRenderMarkdownTable(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderMarkdown(&buf, samplePlan()); err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"## Oiax plan: environments", "| Action | From | To |", "| create | development | test |"} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q:\n%s", want, out)
		}
	}
}
