package frameworks

import (
	"go/ast"
	"strings"

)

type nethttpAdapter struct{}

func (a *nethttpAdapter) Name() string { return "nethttp" }

func (a *nethttpAdapter) MatchConstructor(sel *ast.SelectorExpr, importPath string) bool {
	return importPath == "net/http" &&
		strings.ToLower(sel.Sel.Name) == "newservemux"
}

func (a *nethttpAdapter) RouteMethod(methodName string, args []ast.Expr) string {
	m := strings.ToUpper(methodName)
	switch m {
	case "HANDLE", "HANDLEFUNC":
		return "ANY"
	}
	return ""
}

func (a *nethttpAdapter) GroupCopiesMiddleware() bool { return false }

func (a *nethttpAdapter) ParseRouteArgs(args []ast.Expr, consts map[string]string) (string, ast.Expr, []string) {
	if len(args) < 2 {
		return "", nil, nil
	}
	path := readStringArgWithConsts(args[0], consts)
	if path == "" {
		return "", nil, nil
	}
	rest := args[1:]
	return path, rest[len(rest)-1], nil
}
