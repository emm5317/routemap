package frameworks

import (
	"bytes"
	"go/ast"
	"go/printer"
	"go/token"
	"strconv"
	"strings"
)

// registry holds all known framework adapters.
var registry = []Adapter{
	&chiAdapter{},
	&ginAdapter{},
	&echoAdapter{},
	&fiberAdapter{},
	&nethttpAdapter{},
}

// GetAdapter returns the Adapter for the given framework name, or (nil, false)
// if the framework is not registered.
func GetAdapter(name string) (Adapter, bool) {
	for _, a := range registry {
		if a.Name() == name {
			return a, true
		}
	}
	return nil, false
}

// ConstructorFramework returns the framework name if the call expression is
// a router constructor (e.g. gin.New(), chi.NewRouter(), http.NewServeMux()).
func ConstructorFramework(call *ast.CallExpr, aliases map[string]string) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	path, ok := aliases[pkg.Name]
	if !ok {
		return "", false
	}
	for _, a := range registry {
		if a.MatchConstructor(sel, path) {
			return a.Name(), true
		}
	}
	return "", false
}

// RouteMethodForFramework returns the normalised HTTP method string for a
// framework-specific route registration call, or "" if the method name is
// not a route-registration method for that framework.
func RouteMethodForFramework(framework, method string, args []ast.Expr) string {
	a, ok := GetAdapter(framework)
	if !ok {
		return ""
	}
	return a.RouteMethod(method, args)
}

// IsNetHTTPGlobalHandle returns true when the call is http.Handle or
// http.HandleFunc on the default mux.
func IsNetHTTPGlobalHandle(call *ast.CallExpr, aliases map[string]string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "http" {
		return false
	}
	if aliases[pkg.Name] != "net/http" {
		return false
	}
	name := strings.ToLower(sel.Sel.Name)
	return name == "handle" || name == "handlefunc"
}

// ParseNetHTTPPattern splits a Go 1.22-style "METHOD /path" pattern into its
// constituent parts. Falls back to ("ANY", pattern) for unrecognised patterns.
func ParseNetHTTPPattern(pattern string) (method, path string) {
	parts := strings.SplitN(strings.TrimSpace(pattern), " ", 2)
	if len(parts) == 2 {
		m := strings.ToUpper(strings.TrimSpace(parts[0]))
		switch m {
		case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "TRACE", "CONNECT":
			return m, strings.TrimSpace(parts[1])
		}
	}
	return "ANY", pattern
}

// NormalizeFramework canonicalises user-supplied framework names.
func NormalizeFramework(f string) string {
	f = strings.ToLower(strings.TrimSpace(f))
	switch f {
	case "net/http", "http", "stdlib", "nethttp":
		return "nethttp"
	default:
		return f
	}
}

// FrameworkFromImportPath returns the framework name for a well-known import
// path, or "" if the import is not a supported framework.
func FrameworkFromImportPath(path string) string {
	switch {
	case strings.Contains(path, "gofiber/fiber"):
		return "fiber"
	case strings.Contains(path, "gin-gonic/gin"):
		return "gin"
	case strings.Contains(path, "labstack/echo"):
		return "echo"
	case strings.Contains(path, "go-chi/chi"):
		return "chi"
	case path == "net/http":
		return "nethttp"
	}
	return ""
}

// DetectedFrameworks returns the framework names that are imported in the
// file (via aliases) and present in the allowed set.
func DetectedFrameworks(aliases map[string]string, allowed map[string]bool) []string {
	var found []string
	seen := map[string]bool{}
	for _, path := range aliases {
		fw := FrameworkFromImportPath(path)
		if fw == "" || seen[fw] {
			continue
		}
		if AllowedFramework(fw, allowed) {
			found = append(found, fw)
			seen[fw] = true
		}
	}
	return found
}

// MakeAllowedSet builds the allowed-frameworks lookup map from a slice of
// user-supplied framework names. An empty slice means "allow all".
func MakeAllowedSet(frameworks []string) map[string]bool {
	if len(frameworks) == 0 {
		return map[string]bool{}
	}
	m := make(map[string]bool, len(frameworks))
	for _, f := range frameworks {
		m[NormalizeFramework(f)] = true
	}
	return m
}

// AllowedFramework reports whether the given framework is permitted. An empty
// allowed map means all frameworks are permitted.
func AllowedFramework(framework string, allowed map[string]bool) bool {
	if len(allowed) == 0 {
		return true
	}
	return allowed[framework]
}

// HasRoutelikeCalls returns true if the file contains method calls that look
// like route registrations or router constructors. Used to suppress noisy
// diagnostics on files that only import a framework for types.
func HasRoutelikeCalls(file *ast.File) bool {
	routeMethods := map[string]bool{
		"Get": true, "Post": true, "Put": true, "Delete": true, "Patch": true,
		"Head": true, "Options": true, "Any": true, "All": true,
		"Handle": true, "HandleFunc": true, "Group": true, "Route": true,
		"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true,
		"HEAD": true, "OPTIONS": true,
		"New": true, "Default": true, "NewRouter": true, "NewServeMux": true,
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if routeMethods[sel.Sel.Name] {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// readStringLit is a local helper to read a string literal from an AST
// expression without importing analysis (avoids potential import cycles).
func readStringLit(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	v, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return v
}

// readStringArgWithConsts resolves a string argument from a literal, const identifier,
// or binary concatenation expression. Local copy avoids importing analysis.
func readStringArgWithConsts(expr ast.Expr, consts map[string]string) string {
	if s := readStringLit(expr); s != "" {
		return s
	}
	if id, ok := expr.(*ast.Ident); ok {
		return consts[id.Name]
	}
	if bin, ok := expr.(*ast.BinaryExpr); ok && bin.Op == token.ADD {
		left := readStringArgWithConsts(bin.X, consts)
		right := readStringArgWithConsts(bin.Y, consts)
		if left != "" && right != "" {
			return left + right
		}
	}
	return ""
}

// exprToString renders an AST expression to source. Local copy avoids importing analysis.
func exprToString(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	var b bytes.Buffer
	_ = printer.Fprint(&b, token.NewFileSet(), expr)
	return b.String()
}
