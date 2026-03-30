package frameworks

import (
	"go/ast"
	"strings"

)

type fiberAdapter struct{}

func (a *fiberAdapter) Name() string { return "fiber" }

func (a *fiberAdapter) MatchConstructor(sel *ast.SelectorExpr, importPath string) bool {
	return strings.Contains(importPath, "gofiber/fiber") &&
		strings.ToLower(sel.Sel.Name) == "new"
}

func (a *fiberAdapter) RouteMethod(methodName string, args []ast.Expr) string {
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

func (a *fiberAdapter) GroupCopiesMiddleware() bool { return true }

func (a *fiberAdapter) ParseRouteArgs(args []ast.Expr, consts map[string]string) (string, ast.Expr, []string) {
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
