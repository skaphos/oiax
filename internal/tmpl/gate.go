package tmpl

import (
	"text/template"
	"text/template/parse"
)

// The summary template gate (spec 002 FR-016). Commit text cannot be made
// safe inside an arbitrary custom template: printf "%#v" bypasses any
// wrapper type, a conditional can emit a link only for the real value, and
// string functions can transform a value past any probe. So a customized
// conflict body is used for the CI summary only if its parse tree shows it
// never reads commit text; otherwise the summary uses the built-in body.
//
// The walk is a small abstract interpretation over value kinds. It is
// conservative: anything it does not recognize counts as reading commit
// text.

// valueKind is what a template expression evaluates to, as far as commit
// text is concerned.
type valueKind int

const (
	// kindScalar is a value that cannot carry commit text: a string or
	// number field other than a subject, a literal, or a function result
	// computed only from scalars.
	kindScalar valueKind = iota
	// kindRoot is the whole Context (the top-level dot, or $).
	kindRoot
	// kindConflict is the whole *Conflict value.
	kindConflict
	// kindCommits is the []Commit list.
	kindCommits
	// kindCommit is one Commit element.
	kindCommit
)

// rootFields are the Context fields and the kind each yields.
var rootFields = map[string]valueKind{
	"Graph": kindScalar, "Type": kindScalar, "From": kindScalar, "To": kindScalar,
	"Count": kindScalar, "SourceHead": kindScalar, "SourceHeadShort": kindScalar,
	"Strategy": kindScalar, "Mechanism": kindScalar, "Surface": kindScalar,
	"Conflict": kindConflict, "Commits": kindCommits,
}

// conflictFields are the Conflict fields that cannot carry commit text.
var conflictFields = map[string]bool{"SHA": true, "Applied": true, "Whole": true, "SquashCommits": true}

// commitFields are the Commit fields that cannot carry commit text.
var commitFields = map[string]bool{"SHA": true, "ShortSHA": true}

// readsCommitText reports whether t can read an untrusted value: a
// subject, or a whole value that contains one, printed or passed to a
// function. Defining or invoking other templates counts as reading, since
// the walk does not follow them.
func readsCommitText(t *template.Template) bool {
	if t == nil || t.Tree == nil || t.Root == nil || len(t.Templates()) > 1 {
		return true
	}
	g := &gate{}
	g.list(t.Root, kindRoot, map[string]valueKind{"$": kindRoot})
	return g.reads
}

type gate struct {
	reads bool
}

// list walks a node list with the given dot and variables in scope.
func (g *gate) list(l *parse.ListNode, dot valueKind, vars map[string]valueKind) {
	if l == nil {
		return
	}
	for _, n := range l.Nodes {
		if g.reads {
			return
		}
		g.node(n, dot, vars)
	}
}

func (g *gate) node(n parse.Node, dot valueKind, vars map[string]valueKind) {
	switch n := n.(type) {
	case *parse.TextNode, *parse.CommentNode, *parse.BreakNode, *parse.ContinueNode:
	case *parse.ActionNode:
		// A printed value must be a scalar; an assignment only binds.
		k := g.pipe(n.Pipe, dot, vars)
		if len(n.Pipe.Decl) == 0 && k != kindScalar {
			g.reads = true
		}
	case *parse.IfNode:
		// A variable declared in the condition is scoped to the block, so
		// bind it in a copy, never in the enclosing scope.
		inner := scope(vars)
		g.cond(n.Pipe, dot, inner)
		g.list(n.List, dot, inner)
		g.list(n.ElseList, dot, scope(vars))
	case *parse.WithNode:
		inner := scope(vars)
		k := g.cond(n.Pipe, dot, inner)
		g.list(n.List, k, inner)
		g.list(n.ElseList, dot, scope(vars))
	case *parse.RangeNode:
		inner := scope(vars)
		k := g.cond(n.Pipe, dot, inner)
		elem := kindScalar
		if k == kindCommits {
			elem = kindCommit
		}
		// range $i, $c := .Commits binds the index and the element; a
		// single variable binds the element.
		switch len(n.Pipe.Decl) {
		case 1:
			inner[n.Pipe.Decl[0].Ident[0]] = elem
		case 2:
			inner[n.Pipe.Decl[0].Ident[0]] = kindScalar
			inner[n.Pipe.Decl[1].Ident[0]] = elem
		}
		g.list(n.List, elem, inner)
		g.list(n.ElseList, dot, scope(vars))
	default:
		// TemplateNode, and anything this walk does not know.
		g.reads = true
	}
}

// cond evaluates the pipeline of an if, with, or range. A bare value is
// allowed there in whole (truthiness, iteration, or rebinding dot reveals
// no commit text); anything more is evaluated as a general pipeline.
func (g *gate) cond(p *parse.PipeNode, dot valueKind, vars map[string]valueKind) valueKind {
	if p != nil && len(p.Cmds) == 1 && len(p.Cmds[0].Args) == 1 {
		if _, isFunc := p.Cmds[0].Args[0].(*parse.IdentifierNode); !isFunc {
			k := g.arg(p.Cmds[0].Args[0], dot, vars)
			if p.IsAssign && k != kindScalar {
				g.reads = true
			}
			// Shadowing an outer variable in a block condition is not
			// modelled; count it as a read rather than guess.
			for _, d := range p.Decl {
				if _, exists := vars[d.Ident[0]]; exists && !p.IsAssign {
					g.reads = true
				}
			}
			if len(p.Decl) == 1 {
				vars[p.Decl[0].Ident[0]] = k
			}
			return k
		}
	}
	k := g.pipe(p, dot, vars)
	if k != kindScalar {
		g.reads = true
	}
	return k
}

// pipe evaluates a pipeline and binds any declared variable.
func (g *gate) pipe(p *parse.PipeNode, dot valueKind, vars map[string]valueKind) valueKind {
	if p == nil {
		g.reads = true
		return kindScalar
	}
	k := kindScalar
	for i, c := range p.Cmds {
		k = g.command(c, dot, vars, i > 0, k)
	}
	// "$x = value" reassigns a variable that may belong to an enclosing
	// scope; the walk copies scopes per block, so the outer scope would not
	// see the new kind. Treat any non-scalar reassignment as a read.
	if p.IsAssign && k != kindScalar {
		g.reads = true
	}
	if len(p.Decl) == 1 {
		vars[p.Decl[0].Ident[0]] = k
	}
	return k
}

// command evaluates one pipeline stage. piped is true when the previous
// stage's result (prev) is passed as the final argument.
func (g *gate) command(c *parse.CommandNode, dot valueKind, vars map[string]valueKind, piped bool, prev valueKind) valueKind {
	if len(c.Args) == 0 {
		g.reads = true
		return kindScalar
	}
	fn, isFunc := c.Args[0].(*parse.IdentifierNode)
	if !isFunc {
		// A value, not a call: field chains, dot, variables, literals.
		// Extra arguments (a method call) or a piped input are not
		// something the walk models.
		if len(c.Args) > 1 || piped {
			g.reads = true
		}
		return g.arg(c.Args[0], dot, vars)
	}
	args := make([]valueKind, 0, len(c.Args))
	for _, a := range c.Args[1:] {
		args = append(args, g.arg(a, dot, vars))
	}
	if piped {
		args = append(args, prev)
	}
	// len of the commit list reveals only a count.
	if fn.Ident == "len" && len(args) == 1 && args[0] == kindCommits {
		return kindScalar
	}
	for _, a := range args {
		if a != kindScalar {
			g.reads = true
		}
	}
	return kindScalar
}

// arg evaluates one operand.
func (g *gate) arg(n parse.Node, dot valueKind, vars map[string]valueKind) valueKind {
	switch n := n.(type) {
	case *parse.DotNode:
		return dot
	case *parse.FieldNode:
		return g.fields(dot, n.Ident)
	case *parse.VariableNode:
		k, ok := vars[n.Ident[0]]
		if !ok {
			g.reads = true
			return kindScalar
		}
		return g.fields(k, n.Ident[1:])
	case *parse.ChainNode:
		return g.fields(g.arg(n.Node, dot, vars), n.Field)
	case *parse.PipeNode:
		return g.pipe(n, dot, scope(vars))
	case *parse.StringNode, *parse.NumberNode, *parse.BoolNode, *parse.NilNode:
		return kindScalar
	case *parse.IdentifierNode:
		// A niladic function used as an operand.
		return kindScalar
	default:
		g.reads = true
		return kindScalar
	}
}

// fields resolves a field chain from a starting kind. Reaching a subject,
// or any field the walk does not know, counts as reading commit text.
func (g *gate) fields(k valueKind, idents []string) valueKind {
	for _, id := range idents {
		switch k {
		case kindRoot:
			next, ok := rootFields[id]
			if !ok {
				g.reads = true
				return kindScalar
			}
			k = next
		case kindConflict:
			if !conflictFields[id] {
				g.reads = true
				return kindScalar
			}
			k = kindScalar
		case kindCommit:
			if !commitFields[id] {
				g.reads = true
				return kindScalar
			}
			k = kindScalar
		default:
			g.reads = true
			return kindScalar
		}
	}
	return k
}

// scope copies the variables in scope for a nested block.
func scope(vars map[string]valueKind) map[string]valueKind {
	out := make(map[string]valueKind, len(vars))
	for k, v := range vars {
		out[k] = v
	}
	return out
}
