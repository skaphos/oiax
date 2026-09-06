package notification

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"text/template"
	"text/template/parse"
	"time"

	v1 "github.com/skaphos/oiax/v2/pkg/api/v1"
)

// TemplateContext contains only captured presentation facts. Times are strings,
// not objects with callable methods. No policy, credentials or effects exist here.
type TemplateContext struct {
	Event, RequestType, Repository, Graph, RequestID, RequestURL, EventID          string
	SourceBranch, DestinationBranch, LogicalSourceBranch, LogicalDestinationBranch string
	SourceEnvironment, DestinationEnvironment, OccurredAt, ObservedAt              string
	Commits                                                                        []CommitSummary
	CommitCount                                                                    int
	CommitCountKnown, CommitsTruncated, CommitsUnavailable                         bool
}

type templatePair struct{ title, body *template.Template }
type TemplateSet struct {
	base         templatePair
	destinations map[string]templatePair
}

var ErrTemplateInvalid = errors.New("notification-template-invalid: check documented fields, helpers and output limits")
var errTemplate = ErrTemplateInvalid

// ResolveTemplates accepts already loaded pinned body files. Diagnostics never
// include source text or template execution errors, which can contain secrets.
func ResolveTemplates(policy *v1.NotificationPolicy) (*TemplateSet, error) {
	s := &TemplateSet{destinations: map[string]templatePair{}}
	if policy == nil {
		return s, nil
	}
	var err error
	s.base, err = overlayTemplate(s.base, policy.Templates)
	if err != nil {
		return nil, fmt.Errorf("spec.notifications.templates: %w", err)
	}
	for i, d := range policy.Destinations {
		p, err := overlayTemplate(s.base, d.Templates)
		if err != nil {
			return nil, fmt.Errorf("spec.notifications.destinations[%d].templates: %w", i, err)
		}
		s.destinations[d.Name] = p
	}
	return s, nil
}

func overlayTemplate(base templatePair, slots *v1.NotificationTemplates) (templatePair, error) {
	if slots == nil {
		return base, nil
	}
	if slots.BodyFile != "" {
		return templatePair{}, errTemplate
	}
	for _, slot := range []struct {
		source *string
		target **template.Template
	}{{slots.Title, &base.title}, {slots.Body, &base.body}} {
		if slot.source == nil {
			continue
		}
		if len(*slot.source) > 1<<20 {
			return templatePair{}, errTemplate
		}
		t, err := template.New("notification").Option("missingkey=error").Funcs(template.FuncMap{
			"trunc":    func(n int, s string) string { r := []rune(s); return string(r[:min(max(n, 0), len(r))]) },
			"shortSHA": func(s string) string { return s[:min(len(s), 7)] },
		}).Parse(*slot.source)
		if err != nil || len(t.Templates()) != 1 || !validTemplateNode(t.Root, reflect.TypeFor[TemplateContext]()) {
			return templatePair{}, errTemplate
		}
		*slot.target = t
	}
	for _, event := range []string{"request-created", "request-merged"} {
		for _, kind := range []string{"promotion", "backflow"} {
			c := TemplateContext{Event: event, RequestType: kind, Repository: "example/repo", Graph: "graph", RequestID: "42", RequestURL: "https://github.com/example/repo/pull/42", EventID: "sample-event", SourceBranch: "dev", DestinationBranch: "test", LogicalSourceBranch: "dev", LogicalDestinationBranch: "test", SourceEnvironment: "development", DestinationEnvironment: "test", OccurredAt: "2026-09-05T00:00:00Z", ObservedAt: "2026-09-05T00:01:00Z", Commits: []CommitSummary{{SHA: strings.Repeat("a", 40), ShortSHA: "aaaaaaa", Subject: "sample subject"}}, CommitCount: 1, CommitCountKnown: true}
			if _, err := renderTemplatePair(base, c, RenderedMessageV1{}); err != nil {
				return templatePair{}, err
			}
		}
	}
	return base, nil
}

// Validate fields even in unreachable branches. Bounded ranges are restricted
// to the captured commit slice; inclusion, method calls and variable assignment
// are not part of the closed notification language.
func validTemplateNode(node parse.Node, scope reflect.Type) bool {
	if node == nil || reflect.ValueOf(node).IsNil() {
		return true
	}
	switch n := node.(type) {
	case *parse.ListNode:
		for _, child := range n.Nodes {
			if !validTemplateNode(child, scope) {
				return false
			}
		}
	case *parse.ActionNode:
		return validTemplateNode(n.Pipe, scope)
	case *parse.PipeNode:
		if len(n.Decl) != 0 {
			return false
		}
		for _, child := range n.Cmds {
			if !validTemplateNode(child, scope) {
				return false
			}
		}
	case *parse.CommandNode:
		for _, child := range n.Args {
			if !validTemplateNode(child, scope) {
				return false
			}
		}
	case *parse.FieldNode:
		return validTemplateField(scope, n.Ident)
	case *parse.VariableNode:
		return len(n.Ident) > 0 && n.Ident[0] == "$" && validTemplateField(reflect.TypeFor[TemplateContext](), n.Ident[1:])
	case *parse.IfNode:
		return validTemplateNode(n.Pipe, scope) && validTemplateNode(n.List, scope) && validTemplateNode(n.ElseList, scope)
	case *parse.RangeNode:
		if len(n.Pipe.Decl) != 0 || len(n.Pipe.Cmds) != 1 || len(n.Pipe.Cmds[0].Args) != 1 {
			return false
		}
		field, ok := n.Pipe.Cmds[0].Args[0].(*parse.FieldNode)
		return ok && scope == reflect.TypeFor[TemplateContext]() && len(field.Ident) == 1 && field.Ident[0] == "Commits" && validTemplateNode(n.List, reflect.TypeFor[CommitSummary]()) && validTemplateNode(n.ElseList, scope)
	case *parse.IdentifierNode:
		return n.Ident != "call"
	case *parse.TextNode, *parse.StringNode, *parse.NumberNode, *parse.BoolNode, *parse.NilNode, *parse.DotNode, *parse.BreakNode, *parse.ContinueNode:
	default:
		return false
	}
	return true
}

func validTemplateField(scope reflect.Type, path []string) bool {
	for _, name := range path {
		if scope.Kind() != reflect.Struct {
			return false
		}
		field, ok := scope.FieldByName(name)
		if !ok || !field.IsExported() {
			return false
		}
		scope = field.Type
	}
	return true
}

type templateBuffer struct{ strings.Builder }

func (b *templateBuffer) Write(p []byte) (int, error) {
	if len(p) > (12<<10)-b.Len() {
		return 0, errTemplate
	}
	return b.Builder.Write(p)
}

func renderTemplatePair(p templatePair, c TemplateContext, m RenderedMessageV1) (RenderedMessageV1, error) {
	for _, slot := range []struct {
		t         *template.Template
		out       *string
		multiline bool
	}{{p.title, &m.Title, false}, {p.body, &m.Body, true}} {
		if slot.t == nil {
			continue
		}
		var b templateBuffer
		if err := slot.t.Execute(&b, c); err != nil {
			return RenderedMessageV1{}, errTemplate
		}
		*slot.out = SafeDisplayText(b.String(), slot.multiline, c.RequestURL)
		if slot.multiline && len(*slot.out) > 12<<10 {
			return RenderedMessageV1{}, errTemplate
		}
		if !slot.multiline {
			r := []rune(strings.TrimSpace(*slot.out))
			*slot.out = string(r[:min(len(r), 256)])
		}
	}
	return m, nil
}

func (s *TemplateSet) Render(destination string, e EventV1) (RenderedMessageV1, error) {
	m, err := RenderBuiltin(e)
	if err != nil || s == nil {
		return m, err
	}
	p := s.base
	if override, ok := s.destinations[destination]; ok {
		p = override
	}
	c := TemplateContext{Event: string(e.Kind), RequestType: string(e.Request.Type), Repository: e.Repository.Name, Graph: e.Graph, RequestID: e.Request.ID, RequestURL: e.Request.URL, EventID: e.ID, SourceBranch: e.Request.Source, DestinationBranch: e.Request.Destination, LogicalSourceBranch: e.Request.LogicalSource, LogicalDestinationBranch: e.Request.LogicalDestination, SourceEnvironment: e.SourceEnvironment, DestinationEnvironment: e.DestinationEnvironment, OccurredAt: e.OccurredAt.UTC().Format(time.RFC3339), ObservedAt: e.ObservedAt.UTC().Format(time.RFC3339), Commits: e.Snapshot.Commits, CommitCount: e.Snapshot.CommitCount, CommitCountKnown: e.Snapshot.CommitCountKnown, CommitsTruncated: e.Snapshot.CommitsTruncated, CommitsUnavailable: e.Snapshot.CommitsUnavailable}
	if c.SourceEnvironment == "" {
		c.SourceEnvironment = e.Request.Source
	}
	if c.DestinationEnvironment == "" {
		c.DestinationEnvironment = e.Request.Destination
	}
	return renderTemplatePair(p, c, m)
}
