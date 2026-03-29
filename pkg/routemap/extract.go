package routemap

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

type routerContext struct {
	Framework  string
	Prefix     string
	Middleware []string
}

// ExtractRoutes performs static route extraction from Go source files.
func ExtractRoutes(_ context.Context, cfg Config) (RouteMap, error) {
	if cfg.ModuleDir == "" {
		cfg.ModuleDir = "."
	}
	if err := cfg.validate(); err != nil {
		return RouteMap{}, err
	}

	files, err := discoverGoFiles(cfg.ModuleDir)
	if err != nil {
		return RouteMap{}, err
	}

	allowed := makeAllowedSet(cfg.Frameworks)
	result := RouteMap{}
	for _, filename := range files {
		routes, diags, err := extractFromFile(filename, allowed, cfg.IncludeMiddleware)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "warning", Message: err.Error(), File: filename})
			result.Partial = true
			continue
		}
		result.Routes = append(result.Routes, routes...)
		result.Diagnostics = append(result.Diagnostics, diags...)
	}
	if len(result.Diagnostics) > 0 {
		result.Partial = true
	}
	stableSortRoutes(result.Routes)
	return result, nil
}

func discoverGoFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" || name == "testdata" {
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func extractFromFile(filename string, allowed map[string]bool, includeMiddleware bool) ([]Route, []Diagnostic, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}

	aliases := parseImportAliases(file)
	var routes []Route
	var diags []Diagnostic

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		env := map[string]routerContext{}
		routes = append(routes, walkBlock(fset, filename, fn.Body, env, aliases, allowed, includeMiddleware, &diags)...)
	}
	return routes, diags, nil
}

func walkBlock(
	fset *token.FileSet,
	filename string,
	block *ast.BlockStmt,
	env map[string]routerContext,
	aliases map[string]string,
	allowed map[string]bool,
	includeMiddleware bool,
	diags *[]Diagnostic,
) []Route {
	var routes []Route
	for _, stmt := range block.List {
		switch s := stmt.(type) {
		case *ast.AssignStmt:
			handleAssignment(env, s, aliases)
		case *ast.DeclStmt:
			handleDeclStmt(env, s, aliases)
		case *ast.ExprStmt:
			call, ok := s.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			routes = append(routes, handleCall(fset, filename, call, env, aliases, allowed, includeMiddleware, diags)...)
		}
	}
	return routes
}

func handleAssignment(env map[string]routerContext, s *ast.AssignStmt, aliases map[string]string) {
	if len(s.Lhs) != len(s.Rhs) {
		return
	}
	for i := range s.Lhs {
		id, ok := s.Lhs[i].(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		ctx, ok := deriveContext(s.Rhs[i], env, aliases)
		if ok {
			env[id.Name] = ctx
		}
	}
}

func handleDeclStmt(env map[string]routerContext, s *ast.DeclStmt, aliases map[string]string) {
	gen, ok := s.Decl.(*ast.GenDecl)
	if !ok || gen.Tok != token.VAR {
		return
	}
	for _, spec := range gen.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for i, name := range vs.Names {
			if i >= len(vs.Values) {
				continue
			}
			ctx, ok := deriveContext(vs.Values[i], env, aliases)
			if ok {
				env[name.Name] = ctx
			}
		}
	}
}

func deriveContext(expr ast.Expr, env map[string]routerContext, aliases map[string]string) (routerContext, bool) {
	switch e := expr.(type) {
	case *ast.Ident:
		ctx, ok := env[e.Name]
		return ctx, ok
	case *ast.CallExpr:
		if framework, ok := constructorFramework(e, aliases); ok {
			return routerContext{Framework: framework, Prefix: ""}, true
		}
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			base, ok := sel.X.(*ast.Ident)
			if !ok {
				return routerContext{}, false
			}
			parent, ok := env[base.Name]
			if !ok {
				return routerContext{}, false
			}
			switch strings.ToLower(sel.Sel.Name) {
			case "group":
				if len(e.Args) == 0 {
					return parent, true
				}
				p := readStringArg(e.Args[0])
				ctx := parent
				ctx.Prefix = joinPath(parent.Prefix, p)
				ctx.Middleware = append([]string{}, parent.Middleware...)
				if parent.Framework == "fiber" || parent.Framework == "gin" {
					for _, arg := range e.Args[1:] {
						ctx.Middleware = append(ctx.Middleware, exprString(arg))
					}
				}
				return ctx, true
			case "with":
				ctx := parent
				ctx.Middleware = append([]string{}, parent.Middleware...)
				for _, arg := range e.Args {
					ctx.Middleware = append(ctx.Middleware, exprString(arg))
				}
				return ctx, true
			}
		}
	}
	return routerContext{}, false
}

func handleCall(
	fset *token.FileSet,
	filename string,
	call *ast.CallExpr,
	env map[string]routerContext,
	aliases map[string]string,
	allowed map[string]bool,
	includeMiddleware bool,
	diags *[]Diagnostic,
) []Route {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}

	method := sel.Sel.Name
	lowerMethod := strings.ToLower(method)

	if baseID, ok := sel.X.(*ast.Ident); ok {
		ctx, ok := env[baseID.Name]
		if !ok {
			return nil
		}
		if !allowedFramework(ctx.Framework, allowed) {
			return nil
		}

		if lowerMethod == "use" {
			for _, arg := range call.Args {
				ctx.Middleware = append(ctx.Middleware, exprString(arg))
			}
			env[baseID.Name] = ctx
			return nil
		}

		if routeMethod := routeMethodForFramework(ctx.Framework, method, call.Args); routeMethod != "" {
			r, ok := buildRoute(fset, filename, call, ctx, routeMethod, includeMiddleware)
			if ok {
				return []Route{r}
			}
			*diags = append(*diags, Diagnostic{Severity: "warning", Message: "unable to parse route call", File: filename, Line: fset.Position(call.Pos()).Line})
			return nil
		}

		if ctx.Framework == "chi" && lowerMethod == "route" && len(call.Args) >= 2 {
			prefix := readStringArg(call.Args[0])
			next := ctx
			next.Prefix = joinPath(ctx.Prefix, prefix)
			if fn, ok := call.Args[1].(*ast.FuncLit); ok && len(fn.Type.Params.List) > 0 && len(fn.Type.Params.List[0].Names) > 0 {
				childName := fn.Type.Params.List[0].Names[0].Name
				childEnv := cloneEnv(env)
				childEnv[childName] = next
				return walkBlock(fset, filename, fn.Body, childEnv, aliases, allowed, includeMiddleware, diags)
			}
		}
	}

	if isNetHTTPGlobalHandle(call, aliases) && allowedFramework("nethttp", allowed) {
		r, ok := buildGlobalNetHTTPRoute(fset, filename, call, includeMiddleware)
		if ok {
			return []Route{r}
		}
	}

	return nil
}

func buildRoute(fset *token.FileSet, filename string, call *ast.CallExpr, ctx routerContext, method string, includeMiddleware bool) (Route, bool) {
	pathArg, handlerArg, routeMW := parseRouteArgs(ctx.Framework, call)
	if pathArg == "" || handlerArg == nil {
		return Route{}, false
	}
	if ctx.Framework == "nethttp" {
		parsedMethod, parsedPath := parseNetHTTPPattern(pathArg)
		method = parsedMethod
		pathArg = parsedPath
	}
	pos := fset.Position(call.Pos())
	r := Route{
		Method:     method,
		Path:       joinPath(ctx.Prefix, pathArg),
		Handler:    exprString(handlerArg),
		Framework:  ctx.Framework,
		File:       filename,
		Line:       pos.Line,
		GroupPath:  ctx.Prefix,
		Confidence: "exact",
	}
	if includeMiddleware {
		chain := append([]string{}, ctx.Middleware...)
		chain = append(chain, routeMW...)
		for _, m := range chain {
			r.Middleware = append(r.Middleware, MiddlewareRef{Name: m})
		}
	}
	return r, true
}

func parseRouteArgs(framework string, call *ast.CallExpr) (string, ast.Expr, []string) {
	if len(call.Args) < 2 {
		return "", nil, nil
	}
	path := readStringArg(call.Args[0])
	if path == "" {
		return "", nil, nil
	}
	args := call.Args[1:]
	switch framework {
	case "gin", "echo", "fiber":
		if len(args) == 0 {
			return "", nil, nil
		}
		handler := args[len(args)-1]
		mw := make([]string, 0, len(args)-1)
		for _, arg := range args[:len(args)-1] {
			mw = append(mw, exprString(arg))
		}
		return path, handler, mw
	case "chi":
		if len(args) >= 1 {
			return path, args[len(args)-1], nil
		}
	case "nethttp":
		return path, args[len(args)-1], nil
	}
	return "", nil, nil
}

func parseImportAliases(file *ast.File) map[string]string {
	m := map[string]string{}
	for _, im := range file.Imports {
		path := strings.Trim(im.Path.Value, "\"")
		name := ""
		if im.Name != nil {
			name = im.Name.Name
		} else {
			parts := strings.Split(path, "/")
			name = parts[len(parts)-1]
		}
		m[name] = path
	}
	return m
}

func constructorFramework(call *ast.CallExpr, aliases map[string]string) (string, bool) {
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
	name := strings.ToLower(sel.Sel.Name)
	switch {
	case strings.Contains(path, "go-chi/chi") && name == "newrouter":
		return "chi", true
	case strings.Contains(path, "gin-gonic/gin") && (name == "new" || name == "default"):
		return "gin", true
	case strings.Contains(path, "labstack/echo") && name == "new":
		return "echo", true
	case strings.Contains(path, "gofiber/fiber") && name == "new":
		return "fiber", true
	case path == "net/http" && name == "newservemux":
		return "nethttp", true
	}
	return "", false
}

func routeMethodForFramework(framework, method string, args []ast.Expr) string {
	m := strings.ToUpper(method)
	switch framework {
	case "chi":
		switch m {
		case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "TRACE", "CONNECT":
			return m
		case "METHOD":
			if len(args) > 0 {
				return strings.ToUpper(readStringArg(args[0]))
			}
		}
	case "gin", "echo", "fiber":
		switch m {
		case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS":
			return m
		case "ANY", "ALL":
			return "ANY"
		case "MATCH", "ADD":
			return "MULTI"
		}
	case "nethttp":
		switch m {
		case "HANDLE", "HANDLEFUNC":
			return "ANY"
		}
	}
	return ""
}

func isNetHTTPGlobalHandle(call *ast.CallExpr, aliases map[string]string) bool {
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

func buildGlobalNetHTTPRoute(fset *token.FileSet, filename string, call *ast.CallExpr, includeMiddleware bool) (Route, bool) {
	if len(call.Args) < 2 {
		return Route{}, false
	}
	pattern := readStringArg(call.Args[0])
	if pattern == "" {
		return Route{}, false
	}
	method, path := parseNetHTTPPattern(pattern)
	pos := fset.Position(call.Pos())
	r := Route{
		Method:     method,
		Path:       path,
		Handler:    exprString(call.Args[1]),
		Framework:  "nethttp",
		File:       filename,
		Line:       pos.Line,
		Confidence: "exact",
	}
	if includeMiddleware {
		r.Middleware = nil
	}
	return r, true
}

func parseNetHTTPPattern(pattern string) (method, path string) {
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

func joinPath(prefix, path string) string {
	if path == "" {
		if prefix == "" {
			return "/"
		}
		return prefix
	}
	if prefix == "" || prefix == "/" {
		if strings.HasPrefix(path, "/") {
			return path
		}
		return "/" + path
	}
	p := strings.TrimSuffix(prefix, "/")
	s := path
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return p + s
}

func readStringArg(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	v, err := strconvUnquote(lit.Value)
	if err != nil {
		return ""
	}
	return v
}

func strconvUnquote(v string) (string, error) {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '`' && v[len(v)-1] == '`') {
			return v[1 : len(v)-1], nil
		}
	}
	return "", fmt.Errorf("invalid string literal")
}

func exprString(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	var b bytes.Buffer
	_ = printer.Fprint(&b, token.NewFileSet(), expr)
	return b.String()
}

func cloneEnv(in map[string]routerContext) map[string]routerContext {
	out := make(map[string]routerContext, len(in))
	for k, v := range in {
		cp := v
		cp.Middleware = append([]string{}, v.Middleware...)
		out[k] = cp
	}
	return out
}

func stableSortRoutes(routes []Route) {
	sort.Slice(routes, func(i, j int) bool {
		a, b := routes[i], routes[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Framework != b.Framework {
			return a.Framework < b.Framework
		}
		if a.Method != b.Method {
			return a.Method < b.Method
		}
		return a.Path < b.Path
	})
}

func makeAllowedSet(frameworks []string) map[string]bool {
	if len(frameworks) == 0 {
		return map[string]bool{}
	}
	m := make(map[string]bool, len(frameworks))
	for _, f := range frameworks {
		m[normalizeFramework(f)] = true
	}
	return m
}

func allowedFramework(framework string, allowed map[string]bool) bool {
	if len(allowed) == 0 {
		return true
	}
	return allowed[framework]
}

func normalizeFramework(f string) string {
	f = strings.ToLower(strings.TrimSpace(f))
	switch f {
	case "net/http", "http", "stdlib", "nethttp":
		return "nethttp"
	default:
		return f
	}
}
