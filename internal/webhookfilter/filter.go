// Package webhookfilter compiles and evaluates sandboxed filter expressions
// over webhook delivery payloads (spec requirement "Filter expression over
// the payload", design decision D3).
//
// The engine is github.com/expr-lang/expr (pinned v1.17.8). Expressions are
// compiled exactly once when the endpoint is saved and evaluated per delivery
// against the delivery environment: the parsed JSON payload's TOP-LEVEL
// fields (decoded via encoding/json into map[string]any) plus the reserved
// `event` key holding the resolved event string. `event` wins if the payload
// itself carries a same-named field.
//
// Sandbox hard rules (spec: comparisons, boolean logic, and field access
// only — no function calls, no I/O): the compiled AST is walked and any
// function call, method call, or builtin call is rejected at compile time.
// This AST walk is the load-bearing gate: without a declared env, expr's
// checker leaves identifiers unchecked, so DisableAllBuiltins alone cannot
// stop `len(payload)` from compiling. Comparisons, boolean logic, field and
// index access, and arithmetic over payload values remain allowed.
//
// Missing-field semantics are strict: expr's map fetch silently yields nil
// for absent keys, so Compile also records every statically-known member
// chain the expression references and Eval verifies each against the
// environment before running — a missing field is an eval error, never a
// silent false. Chains the analysis cannot prove (dynamic keys, indexes,
// `?.` optional access) stay on expr's runtime semantics, which either
// errors naturally or degrades to nil — both acceptable outcomes.
//
// Eval is non-blocking: the sandboxed expression language has no loops and
// no calls, so a run is bounded by the compiled AST size. The context
// parameter is checked up front; expr v1.17.8's Run accepts no context.
package webhookfilter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/vm"
)

// Compiled is a validated, sandboxed filter expression. Create one with
// Compile; it is immutable and safe for concurrent Eval.
type Compiled struct {
	program *vm.Program
	// paths holds every statically-provable member chain the expression
	// references (e.g. project.path_with_namespace), for strict
	// missing-field detection at eval time.
	paths [][]string
}

// Compile parses and validates a filter expression for saving on an
// endpoint. Compilation validates syntax, the sandbox rules, and that the
// result is boolean-typed — it never checks payload field existence, which
// is unknown at save time. Parse and type errors from expr are wrapped
// verbatim so the API can surface the parser's own message.
func Compile(expression string) (Compiled, error) {
	tree, err := parser.Parse(expression)
	if err != nil {
		return Compiled{}, fmt.Errorf("invalid filter expression: %w", err)
	}
	if err := rejectCalls(tree.Node); err != nil {
		return Compiled{}, err
	}
	if !resultIsBoolean(tree.Node) {
		return Compiled{}, fmt.Errorf("invalid filter expression: must evaluate to a boolean: %s", ast.Dump(tree.Node))
	}
	// No expr.Env: the payload shape is dynamic JSON, so identifiers must
	// stay unchecked (an env of map[string]any{} would reject every name).
	// DisableAllBuiltins is defense in depth behind the AST walk.
	program, err := expr.Compile(expression, expr.DisableAllBuiltins())
	if err != nil {
		return Compiled{}, fmt.Errorf("invalid filter expression: %w", err)
	}
	return Compiled{program: program, paths: collectPaths(tree.Node, nil)}, nil
}

// Eval evaluates the compiled expression against the delivery environment
// (payload top-level fields plus the reserved `event` key). It returns
// (false, err) when the expression fails to evaluate (missing field, type
// mismatch), (false, nil) when it evaluates to false, and (true, nil) when
// it evaluates to true.
func (c Compiled) Eval(ctx context.Context, env map[string]any) (bool, error) {
	if c.program == nil {
		return false, fmt.Errorf("filter expression is not compiled")
	}
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("filter evaluation cancelled: %w", err)
	}
	for _, path := range c.paths {
		if err := checkPath(path, env); err != nil {
			return false, err
		}
	}
	out, err := expr.Run(c.program, env)
	if err != nil {
		return false, err
	}
	b, ok := out.(bool)
	if !ok {
		return false, fmt.Errorf("filter expression evaluated to %T, want bool", out)
	}
	return b, nil
}

// VerdictOutcome classifies a dry-run result.
type VerdictOutcome int

const (
	// VerdictMatch: the sample payload satisfies the expression.
	VerdictMatch VerdictOutcome = iota
	// VerdictIgnored: the expression evaluated to false; the delivery would
	// be acknowledged and dropped.
	VerdictIgnored
	// VerdictError: the sample payload did not parse or the expression
	// failed to evaluate.
	VerdictError
)

// Verdict is the outcome of a dry-run evaluation.
type Verdict struct {
	Outcome VerdictOutcome
	// Message carries the parse or eval error text for VerdictError.
	Message string
}

// DryRun evaluates an already-compiled expression against a sample payload
// and event, reporting match, ignored, or error. It has no side effects: no
// store writes, no enqueueing. An unparseable expression is a save-time
// concern (Compile); an unparseable sample is a dry-run error verdict.
func DryRun(c Compiled, samplePayloadJSON []byte, event string) Verdict {
	var payload map[string]any
	if err := json.Unmarshal(samplePayloadJSON, &payload); err != nil {
		return Verdict{Outcome: VerdictError, Message: fmt.Sprintf("sample payload is not a JSON object: %v", err)}
	}
	env := make(map[string]any, len(payload)+1)
	for k, v := range payload {
		env[k] = v
	}
	env["event"] = event
	match, err := c.Eval(context.Background(), env)
	switch {
	case err != nil:
		return Verdict{Outcome: VerdictError, Message: err.Error()}
	case match:
		return Verdict{Outcome: VerdictMatch}
	default:
		return Verdict{Outcome: VerdictIgnored}
	}
}

// rejectCalls enforces the sandbox: function calls, method calls, and
// builtins are denied. It walks every node type the expr v1.17.8 AST defines
// (mirroring ast.Walk's dispatch) so no call site can hide, including inside
// predicate and declarator subtrees.
func rejectCalls(node ast.Node) error {
	switch n := node.(type) {
	case *ast.CallNode:
		return fmt.Errorf("invalid filter expression: function calls are not allowed: %s", ast.Dump(n))
	case *ast.BuiltinNode:
		return fmt.Errorf("invalid filter expression: function calls are not allowed: %s", ast.Dump(n))
	case *ast.UnaryNode:
		return rejectCalls(n.Node)
	case *ast.BinaryNode:
		if err := rejectCalls(n.Left); err != nil {
			return err
		}
		return rejectCalls(n.Right)
	case *ast.ChainNode:
		return rejectCalls(n.Node)
	case *ast.MemberNode:
		if err := rejectCalls(n.Node); err != nil {
			return err
		}
		return rejectCalls(n.Property)
	case *ast.SliceNode:
		if err := rejectCalls(n.Node); err != nil {
			return err
		}
		if n.From != nil {
			if err := rejectCalls(n.From); err != nil {
				return err
			}
		}
		if n.To != nil {
			return rejectCalls(n.To)
		}
	case *ast.PredicateNode:
		return rejectCalls(n.Node)
	case *ast.PointerNode:
	case *ast.VariableDeclaratorNode:
		if err := rejectCalls(n.Value); err != nil {
			return err
		}
		return rejectCalls(n.Expr)
	case *ast.SequenceNode:
		for _, child := range n.Nodes {
			if err := rejectCalls(child); err != nil {
				return err
			}
		}
	case *ast.ConditionalNode:
		if err := rejectCalls(n.Cond); err != nil {
			return err
		}
		if err := rejectCalls(n.Exp1); err != nil {
			return err
		}
		return rejectCalls(n.Exp2)
	case *ast.ArrayNode:
		for _, child := range n.Nodes {
			if err := rejectCalls(child); err != nil {
				return err
			}
		}
	case *ast.MapNode:
		for _, pair := range n.Pairs {
			if err := rejectCalls(pair); err != nil {
				return err
			}
		}
	case *ast.PairNode:
		if err := rejectCalls(n.Key); err != nil {
			return err
		}
		return rejectCalls(n.Value)
	}
	return nil
}

// nonBoolBinaryOps are the BinaryNode operators whose result is not a
// boolean: arithmetic and the range/pipe operators. Everything else in the
// expr grammar (comparisons, in/matches/contains/startsWith/endsWith and
// their negations, and/or) yields a boolean.
var nonBoolBinaryOps = map[string]bool{
	"+": true, "-": true, "*": true, "/": true, "%": true, "**": true,
	"..": true, "|": true,
}

// resultIsBoolean reports whether the expression's result is statically
// guaranteed boolean-typed. The delivery env is dynamic JSON, so most
// sub-expressions are untyped; only the structural shape of the root
// expression decides saveability. A conditional's branches resolve at
// runtime, so ternaries stay saveable and Eval's type assertion backstops
// their result; runtime non-bool results error, they never crash.
func resultIsBoolean(node ast.Node) bool {
	switch n := node.(type) {
	case *ast.BoolNode:
		return true
	case *ast.UnaryNode:
		return n.Operator == "!" || n.Operator == "not"
	case *ast.BinaryNode:
		return !nonBoolBinaryOps[n.Operator]
	case *ast.ConditionalNode:
		return true
	case *ast.VariableDeclaratorNode:
		// A let expression's result is its body's result.
		return resultIsBoolean(n.Expr)
	case *ast.ChainNode:
		return resultIsBoolean(n.Node)
	case *ast.SequenceNode:
		if len(n.Nodes) == 0 {
			return false
		}
		return resultIsBoolean(n.Nodes[len(n.Nodes)-1])
	}
	// Members, identifiers, and non-bool literals are untyped or not bool;
	// calls are already rejected before this check runs.
	return false
}

// collectPaths returns every statically-provable member chain the subtree
// references, for strict missing-field detection at eval time. Chains that
// cannot be proven (dynamic keys, indexes, optional `?.` access) are simply
// omitted — they fall back to expr's runtime semantics.
func collectPaths(node ast.Node, aliases map[string][]string) [][]string {
	switch n := node.(type) {
	case *ast.IdentifierNode, *ast.MemberNode:
		if path := chainPath(n, aliases); path != nil {
			return [][]string{path}
		}
		return nil
	case *ast.VariableDeclaratorNode:
		// let binds a name to the value's chain (or to "unchecked") for the
		// declarator's body only; the value resolves against the outer scope.
		child := make(map[string][]string, len(aliases)+1)
		for k, v := range aliases {
			child[k] = v
		}
		child[n.Name] = chainPath(n.Value, aliases)
		return collectPaths(n.Expr, child)
	case *ast.UnaryNode:
		return collectPaths(n.Node, aliases)
	case *ast.BinaryNode:
		return append(collectPaths(n.Left, aliases), collectPaths(n.Right, aliases)...)
	case *ast.ConditionalNode:
		paths := collectPaths(n.Cond, aliases)
		paths = append(paths, collectPaths(n.Exp1, aliases)...)
		return append(paths, collectPaths(n.Exp2, aliases)...)
	case *ast.SequenceNode:
		var paths [][]string
		for _, child := range n.Nodes {
			paths = append(paths, collectPaths(child, aliases)...)
		}
		return paths
	case *ast.ArrayNode:
		var paths [][]string
		for _, child := range n.Nodes {
			paths = append(paths, collectPaths(child, aliases)...)
		}
		return paths
	case *ast.MapNode:
		var paths [][]string
		for _, pair := range n.Pairs {
			paths = append(paths, collectPaths(pair, aliases)...)
		}
		return paths
	case *ast.PairNode:
		return append(collectPaths(n.Key, aliases), collectPaths(n.Value, aliases)...)
	case *ast.SliceNode:
		paths := collectPaths(n.Node, aliases)
		if n.From != nil {
			paths = append(paths, collectPaths(n.From, aliases)...)
		}
		if n.To != nil {
			paths = append(paths, collectPaths(n.To, aliases)...)
		}
		return paths
	}
	return nil
}

// chainPath resolves one subtree to a single member chain rooted at an env
// field, or nil when the subtree is not a statically-provable chain.
func chainPath(node ast.Node, aliases map[string][]string) []string {
	switch n := node.(type) {
	case *ast.IdentifierNode:
		if aliases != nil {
			if path, ok := aliases[n.Value]; ok {
				return path // nil means the alias is unprovable; stay unchecked
			}
		}
		return []string{n.Value}
	case *ast.MemberNode:
		if n.Optional || n.Method {
			return nil
		}
		base := chainPath(n.Node, aliases)
		if base == nil {
			return nil
		}
		prop, ok := n.Property.(*ast.StringNode)
		if !ok {
			return nil // indexed or dynamic member: not provable
		}
		// Copy-on-extend: aliases share slices across chains, so in-place
		// appends could corrupt sibling chains.
		next := make([]string, len(base)+1)
		copy(next, base)
		next[len(base)] = prop.Value
		return next
	}
	return nil
}

// checkPath verifies one member chain resolves in the environment. Only map
// field segments are provable; anything else (nil deref, index) is left to
// the runtime, which errors on its own.
func checkPath(path []string, env map[string]any) error {
	cur := env
	for i, seg := range path {
		v, ok := cur[seg]
		if !ok {
			return fmt.Errorf("field %q not found in payload", strings.Join(path[:i+1], "."))
		}
		if i == len(path)-1 {
			return nil
		}
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		cur = m
	}
	return nil
}
