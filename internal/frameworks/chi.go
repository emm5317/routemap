package frameworks

import (
	"go/ast"
	"strings"

)

type chiAdapter struct{}

func (a *chiAdapter) Name() string { return "chi" }

func (a *chiAdapter) MatchConstructor(sel *ast.SelectorExpr, importPath string) bool {
	return strings.Contains(importPath, "go-chi/chi") &&
		strings.ToLower(sel.Sel.Name) == "newrouter"
}

func (a *chiAdapter) RouteMethod(methodName string, args []ast.Expr) string {
	m := strings.ToUpper(methodName)
	switch m {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "TRACE", "CONNECT":
		return m
	case "METHOD":
		if len(args) > 0 {
			return strings.ToUpper(readStringLit(args[0]))
		}
	}
	return ""
}

func (a *chiAdapter) GroupCopiesMiddleware() bool { return false }

func (a *chiAdapter) ParseRouteArgs(args []ast.Expr, consts map[string]string) (string, ast.Expr, []string) {
	if len(args) < 2 {
		return "", nil, nil
	}
	path := readStringArgWithConsts(args[0], consts)
	if path == "" {
		return "", nil, nil
	}
	rest := args[1:]
	if len(rest) >= 1 {
		return path, rest[len(rest)-1], nil
	}
	return "", nil, nil
}
