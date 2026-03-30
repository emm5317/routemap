package frameworks

import "go/ast"

// Adapter defines framework-specific behavior for route extraction.
type Adapter interface {
	// Name returns the canonical framework name (e.g., "gin", "chi").
	Name() string
	// MatchConstructor returns true if the call is a constructor for this framework's router.
	MatchConstructor(sel *ast.SelectorExpr, importPath string) bool
	// RouteMethod returns the HTTP method for a route-registration call, or "" if not a route method.
	RouteMethod(methodName string, args []ast.Expr) string
	// GroupCopiesMiddleware returns true if the framework's Group() method accepts inline middleware args.
	GroupCopiesMiddleware() bool
	// ParseRouteArgs extracts (path, handler, middleware) from a route call's arguments.
	// Returns empty path if the call can't be parsed.
	ParseRouteArgs(args []ast.Expr, consts map[string]string) (path string, handler ast.Expr, mw []string)
}
