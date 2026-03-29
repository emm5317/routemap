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
	Inferred   bool // true when resolved via struct-field propagation
}

// ExtractRoutes performs static route extraction from Go source files.
func ExtractRoutes(_ context.Context, cfg Config) (RouteMap, error) {
	if cfg.ModuleDir == "" {
		cfg.ModuleDir = "."
	}
	if err := cfg.validate(); err != nil {
		return RouteMap{}, err
	}

	result := RouteMap{}

	if cfg.PackagePattern != "" {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Severity: "info",
			Code:     "unused-config",
			Message:  "PackagePattern is reserved and currently ignored",
		})
	}

	files, err := discoverGoFiles(cfg.ModuleDir)
	if err != nil {
		return RouteMap{}, err
	}

	allowed := makeAllowedSet(cfg.Frameworks)
	for _, filename := range files {
		routes, diags, err := extractFromFile(filename, allowed, cfg.IncludeMiddleware)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "warning", Message: err.Error(), File: filename})
			continue
		}
		result.Routes = append(result.Routes, routes...)
		result.Diagnostics = append(result.Diagnostics, diags...)
	}

	stableSortRoutes(result.Routes)

	// Flag duplicate method+path pairs.
	result.Diagnostics = append(result.Diagnostics, detectDuplicateRoutes(result.Routes)...)

	// Only mark partial for warning+ severity diagnostics.
	for _, d := range result.Diagnostics {
		if d.Severity == "warning" || d.Severity == "error" {
			result.Partial = true
			break
		}
	}

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
	sfm := scanStructFieldRouters(file, aliases)
	consts := collectConstants(file)
	var routes []Route
	var diags []Diagnostic

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		env := map[string]routerContext{}

		// Seed method receiver env from struct field tracking.
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			recvName, recvType := parseReceiver(fn.Recv.List[0])
			if recvName != "" && recvType != "" {
				for sfKey, ctx := range sfm {
					parts := strings.SplitN(sfKey, ".", 2)
					if len(parts) == 2 && parts[0] == recvType {
						ctx.Inferred = true
						env[recvName+"."+parts[1]] = ctx
					}
				}
			}
		}

		routes = append(routes, walkBlock(fset, filename, fn.Body, env, aliases, consts, allowed, includeMiddleware, &diags)...)
	}

	// Emit diagnostic when a framework is imported with route-like calls but
	// no routes were extracted. Skip files that only import for types (e.g., fiber.Ctx).
	if len(routes) == 0 && hasRoutelikeCalls(file) {
		for _, fw := range detectedFrameworks(aliases, allowed) {
			diags = append(diags, Diagnostic{
				Severity: "info",
				Code:     "no-routes-found",
				Message:  fmt.Sprintf("framework %q imported but no routes extracted", fw),
				File:     filename,
			})
		}
	}

	return routes, diags, nil
}

func walkBlock(
	fset *token.FileSet,
	filename string,
	block *ast.BlockStmt,
	env map[string]routerContext,
	aliases map[string]string,
	consts map[string]string,
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
			routes = append(routes, handleCall(fset, filename, call, env, aliases, consts, allowed, includeMiddleware, diags)...)
		case *ast.IfStmt:
			routes = append(routes, walkBlock(fset, filename, s.Body, cloneEnv(env), aliases, consts, allowed, includeMiddleware, diags)...)
			if s.Else != nil {
				routes = append(routes, walkElse(fset, filename, s.Else, cloneEnv(env), aliases, consts, allowed, includeMiddleware, diags)...)
			}
		case *ast.ForStmt:
			if s.Body != nil {
				routes = append(routes, walkBlock(fset, filename, s.Body, cloneEnv(env), aliases, consts, allowed, includeMiddleware, diags)...)
			}
		case *ast.RangeStmt:
			if s.Body != nil {
				routes = append(routes, walkBlock(fset, filename, s.Body, cloneEnv(env), aliases, consts, allowed, includeMiddleware, diags)...)
			}
		case *ast.SwitchStmt:
			if s.Body != nil {
				for _, clause := range s.Body.List {
					cc, ok := clause.(*ast.CaseClause)
					if !ok {
						continue
					}
					routes = append(routes, walkBlock(fset, filename, &ast.BlockStmt{List: cc.Body}, cloneEnv(env), aliases, consts, allowed, includeMiddleware, diags)...)
				}
			}
		case *ast.TypeSwitchStmt:
			if s.Body != nil {
				for _, clause := range s.Body.List {
					cc, ok := clause.(*ast.CaseClause)
					if !ok {
						continue
					}
					routes = append(routes, walkBlock(fset, filename, &ast.BlockStmt{List: cc.Body}, cloneEnv(env), aliases, consts, allowed, includeMiddleware, diags)...)
				}
			}
		}
	}
	return routes
}

func walkElse(fset *token.FileSet, filename string, elseStmt ast.Stmt, env map[string]routerContext, aliases map[string]string, consts map[string]string, allowed map[string]bool, includeMiddleware bool, diags *[]Diagnostic) []Route {
	switch s := elseStmt.(type) {
	case *ast.BlockStmt:
		return walkBlock(fset, filename, s, env, aliases, consts, allowed, includeMiddleware, diags)
	case *ast.IfStmt:
		routes := walkBlock(fset, filename, s.Body, cloneEnv(env), aliases, consts, allowed, includeMiddleware, diags)
		if s.Else != nil {
			routes = append(routes, walkElse(fset, filename, s.Else, cloneEnv(env), aliases, consts, allowed, includeMiddleware, diags)...)
		}
		return routes
	}
	return nil
}

// resolveRouterKey extracts a string key from an expression for env lookups.
// Returns "r" for Ident("r"), "a.app" for SelectorExpr{X: Ident("a"), Sel: "app"}.
func resolveRouterKey(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.Ident:
		if e.Name == "_" {
			return "", false
		}
		return e.Name, true
	case *ast.SelectorExpr:
		if base, ok := e.X.(*ast.Ident); ok {
			return base.Name + "." + e.Sel.Name, true
		}
	}
	return "", false
}

func handleAssignment(env map[string]routerContext, s *ast.AssignStmt, aliases map[string]string) {
	if len(s.Lhs) != len(s.Rhs) {
		return
	}
	for i := range s.Lhs {
		key, ok := resolveRouterKey(s.Lhs[i])
		if !ok {
			continue
		}
		ctx, ok := deriveContext(s.Rhs[i], env, aliases)
		if ok {
			env[key] = ctx
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
			key, ok := resolveRouterKey(sel.X)
			if !ok {
				return routerContext{}, false
			}
			parent, ok := env[key]
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
	consts map[string]string,
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

	if key, ok := resolveRouterKey(sel.X); ok {
		ctx, ok := env[key]
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
			env[key] = ctx
			return nil
		}

		if routeMethod := routeMethodForFramework(ctx.Framework, method, call.Args); routeMethod != "" {
			r, ok := buildRoute(fset, filename, call, ctx, routeMethod, consts, includeMiddleware)
			if ok {
				return []Route{r}
			}
			*diags = append(*diags, Diagnostic{Severity: "warning", Code: "unparseable-route", Message: "unable to parse route call", File: filename, Line: fset.Position(call.Pos()).Line})
			return nil
		}

		if ctx.Framework == "chi" && lowerMethod == "route" && len(call.Args) >= 2 {
			prefix := readStringArgWithConsts(call.Args[0], consts)
			next := ctx
			next.Prefix = joinPath(ctx.Prefix, prefix)
			if fn, ok := call.Args[1].(*ast.FuncLit); ok && len(fn.Type.Params.List) > 0 && len(fn.Type.Params.List[0].Names) > 0 {
				childName := fn.Type.Params.List[0].Names[0].Name
				childEnv := cloneEnv(env)
				childEnv[childName] = next
				return walkBlock(fset, filename, fn.Body, childEnv, aliases, consts, allowed, includeMiddleware, diags)
			}
		}
	}

	if isNetHTTPGlobalHandle(call, aliases) && allowedFramework("nethttp", allowed) {
		r, ok := buildGlobalNetHTTPRoute(fset, filename, call, consts, includeMiddleware)
		if ok {
			return []Route{r}
		}
	}

	return nil
}

func buildRoute(fset *token.FileSet, filename string, call *ast.CallExpr, ctx routerContext, method string, consts map[string]string, includeMiddleware bool) (Route, bool) {
	pathArg, handlerArg, routeMW := parseRouteArgs(ctx.Framework, call, consts)
	if pathArg == "" || handlerArg == nil {
		return Route{}, false
	}
	if ctx.Framework == "nethttp" {
		parsedMethod, parsedPath := parseNetHTTPPattern(pathArg)
		method = parsedMethod
		pathArg = parsedPath
	}
	pos := fset.Position(call.Pos())
	confidence := ConfidenceExact
	inferredBy := ""
	if ctx.Inferred {
		confidence = ConfidenceInferred
		inferredBy = "struct-field"
	}
	r := Route{
		Method:     method,
		Path:       joinPath(ctx.Prefix, pathArg),
		Handler:    exprString(handlerArg),
		Framework:  ctx.Framework,
		File:       filename,
		Line:       pos.Line,
		GroupPath:  ctx.Prefix,
		Confidence: confidence,
		InferredBy: inferredBy,
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

func parseRouteArgs(framework string, call *ast.CallExpr, consts map[string]string) (string, ast.Expr, []string) {
	if len(call.Args) < 2 {
		return "", nil, nil
	}
	path := readStringArgWithConsts(call.Args[0], consts)
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
			name = defaultPackageName(path)
		}
		m[name] = path
	}
	return m
}

// defaultPackageName returns the Go package name for an import path.
// For versioned modules (e.g. "github.com/gofiber/fiber/v3"), the version
// suffix is skipped and the second-to-last component is used ("fiber").
func defaultPackageName(importPath string) string {
	parts := strings.Split(importPath, "/")
	last := parts[len(parts)-1]
	if isVersionSuffix(last) && len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	return last
}

func isVersionSuffix(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
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

func buildGlobalNetHTTPRoute(fset *token.FileSet, filename string, call *ast.CallExpr, consts map[string]string, includeMiddleware bool) (Route, bool) {
	if len(call.Args) < 2 {
		return Route{}, false
	}
	pattern := readStringArgWithConsts(call.Args[0], consts)
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

// readStringArgWithConsts extends readStringArg by also resolving identifiers
// against a map of file-level string constants.
func readStringArgWithConsts(expr ast.Expr, consts map[string]string) string {
	if s := readStringArg(expr); s != "" {
		return s
	}
	if id, ok := expr.(*ast.Ident); ok {
		return consts[id.Name]
	}
	return ""
}

// collectConstants gathers package-level const string declarations.
func collectConstants(file *ast.File) map[string]string {
	consts := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i < len(vs.Values) {
					if s := readStringArg(vs.Values[i]); s != "" {
						consts[name.Name] = s
					}
				}
			}
		}
	}
	return consts
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

// scanStructFieldRouters performs a pre-pass over all functions in a file to
// find router constructors assigned to struct fields (e.g., s.app = fiber.New()
// or &App{app: fiber.New()}). Returns a map keyed by "TypeName.FieldName".
func scanStructFieldRouters(file *ast.File, aliases map[string]string) map[string]routerContext {
	sfm := map[string]routerContext{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		// Track local variables that may hold routers.
		localEnv := map[string]routerContext{}
		// Track the type name if this function returns a struct pointer (heuristic for constructors).
		returnType := ""
		if fn.Type.Results != nil {
			for _, r := range fn.Type.Results.List {
				if star, ok := r.Type.(*ast.StarExpr); ok {
					if id, ok := star.X.(*ast.Ident); ok {
						returnType = id.Name
					}
				}
			}
		}
		for _, stmt := range fn.Body.List {
			scanStmtForFieldRouters(stmt, localEnv, aliases, sfm, returnType)
		}
	}
	return sfm
}

func scanStmtForFieldRouters(stmt ast.Stmt, localEnv map[string]routerContext, aliases map[string]string, sfm map[string]routerContext, returnType string) {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		if len(s.Lhs) != len(s.Rhs) {
			return
		}
		for i := range s.Lhs {
			rhs := s.Rhs[i]
			// Track local router variables.
			if id, ok := s.Lhs[i].(*ast.Ident); ok && id.Name != "_" {
				if ctx, ok := deriveConstructorOrAlias(rhs, localEnv, aliases); ok {
					localEnv[id.Name] = ctx
				}
				// Check composite literal assignments: instance := &App{app: localRouter}
				scanCompositeLiteral(rhs, localEnv, aliases, sfm)
			}
			// Track field assignments: s.field = fiber.New() or s.field = localRouter
			if sel, ok := s.Lhs[i].(*ast.SelectorExpr); ok {
				if base, ok := sel.X.(*ast.Ident); ok {
					if ctx, ok := deriveConstructorOrAlias(rhs, localEnv, aliases); ok {
						// Try to determine type name from return type or receiver.
						typeName := returnType
						if typeName == "" {
							typeName = base.Name // fallback
						}
						sfm[typeName+"."+sel.Sel.Name] = ctx
					}
				}
			}
		}
	case *ast.DeclStmt:
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
				if ctx, ok := deriveConstructorOrAlias(vs.Values[i], localEnv, aliases); ok {
					localEnv[name.Name] = ctx
				}
				scanCompositeLiteral(vs.Values[i], localEnv, aliases, sfm)
			}
		}
	}
}

// scanCompositeLiteral checks for &Type{field: router} patterns.
func scanCompositeLiteral(expr ast.Expr, localEnv map[string]routerContext, aliases map[string]string, sfm map[string]routerContext) {
	unary, ok := expr.(*ast.UnaryExpr)
	if !ok || unary.Op != token.AND {
		return
	}
	comp, ok := unary.X.(*ast.CompositeLit)
	if !ok {
		return
	}
	typeName := typeNameFromExpr(comp.Type)
	if typeName == "" {
		return
	}
	for _, elt := range comp.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		fieldID, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		if ctx, ok := deriveConstructorOrAlias(kv.Value, localEnv, aliases); ok {
			sfm[typeName+"."+fieldID.Name] = ctx
		}
	}
}

// deriveConstructorOrAlias returns a routerContext if the expression is a
// framework constructor call or a reference to a known local router variable.
func deriveConstructorOrAlias(expr ast.Expr, localEnv map[string]routerContext, aliases map[string]string) (routerContext, bool) {
	switch e := expr.(type) {
	case *ast.Ident:
		ctx, ok := localEnv[e.Name]
		return ctx, ok
	case *ast.CallExpr:
		if fw, ok := constructorFramework(e, aliases); ok {
			return routerContext{Framework: fw}, true
		}
	}
	return routerContext{}, false
}

func typeNameFromExpr(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		if id, ok := e.X.(*ast.Ident); ok {
			return id.Name + "." + e.Sel.Name
		}
	}
	return ""
}

// parseReceiver extracts the receiver variable name and type name.
// For "func (a *App) method()" returns ("a", "App").
func parseReceiver(field *ast.Field) (name string, typeName string) {
	if len(field.Names) > 0 {
		name = field.Names[0].Name
	}
	typeName = receiverTypeName(field.Type)
	return
}

func receiverTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		if id, ok := e.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
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

// hasRoutelikeCalls returns true if the file contains method calls that look
// like route registrations (Get, Post, Handle, etc.) or router constructors.
// Used to avoid noisy diagnostics on files that only import a framework for types.
func hasRoutelikeCalls(file *ast.File) bool {
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

// frameworkFromImportPath returns the framework name if the import path
// belongs to a supported web framework, or "" otherwise.
func frameworkFromImportPath(path string) string {
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

// detectedFrameworks returns the list of supported frameworks imported in this
// file that are also in the allowed set.
func detectedFrameworks(aliases map[string]string, allowed map[string]bool) []string {
	var found []string
	seen := map[string]bool{}
	for _, path := range aliases {
		fw := frameworkFromImportPath(path)
		if fw == "" || seen[fw] {
			continue
		}
		if allowedFramework(fw, allowed) {
			found = append(found, fw)
			seen[fw] = true
		}
	}
	return found
}

// detectDuplicateRoutes flags duplicate method+path pairs as warnings.
func detectDuplicateRoutes(routes []Route) []Diagnostic {
	type routeKey struct {
		method string
		path   string
	}
	type routeLoc struct {
		file string
		line int
	}
	seen := map[routeKey]routeLoc{}
	var diags []Diagnostic
	for _, r := range routes {
		key := routeKey{method: r.Method, path: r.Path}
		if prev, exists := seen[key]; exists {
			diags = append(diags, Diagnostic{
				Severity: "warning",
				Code:     "duplicate-route",
				Message:  fmt.Sprintf("%s %s registered at %s:%d and %s:%d", r.Method, r.Path, prev.file, prev.line, r.File, r.Line),
				File:     r.File,
				Line:     r.Line,
			})
		} else {
			seen[key] = routeLoc{file: r.File, line: r.Line}
		}
	}
	return diags
}
