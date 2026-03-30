package frameworks

import (
	"go/ast"
	"strings"
)

type ginAdapter struct{}

func (a *ginAdapter) Name() string { return "gin" }

func (a *ginAdapter) MatchConstructor(sel *ast.SelectorExpr, importPath string) bool {
	name := strings.ToLower(sel.Sel.Name)
	return strings.Contains(importPath, "gin-gonic/gin") && (name == "new" || name == "default")
}

func (a *ginAdapter) RouteMethod(methodName string, args []ast.Expr) string {
	m := strings.ToUpper(methodName)
	switch m {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS":
		return m
	case "ANY", "ALL":
		return "ANY"
	case "MATCH", "ADD":
		return "MULTI"
	}
	return ""
}

func (a *ginAdapter) GroupCopiesMiddleware() bool { return true }

func (a *ginAdapter) ParseRouteArgs(args []ast.Expr, consts map[string]string) (string, ast.Expr, []string) {
	if len(args) < 2 {
		return "", nil, nil
	}
	path := readStringArgWithConsts(args[0], consts)
	if path == "" {
		return "", nil, nil
	}
	rest := args[1:]
	if len(rest) == 0 {
		return "", nil, nil
	}
	handler := rest[len(rest)-1]
	mw := make([]string, 0, len(rest)-1)
	for _, arg := range rest[:len(rest)-1] {
		mw = append(mw, exprToString(arg))
	}
	return path, handler, mw
}
