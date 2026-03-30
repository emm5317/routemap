package routemap

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/emm5317/routemap/internal/analysis"
	"github.com/emm5317/routemap/internal/frameworks"
)

// ExtractRoutes performs static route extraction from Go source files.
// It uses go/packages to load packages with proper build-tag and file-selection
// awareness, then walks the ASTs to discover HTTP route registrations.
func ExtractRoutes(ctx context.Context, cfg Config) (RouteMap, error) {
	if cfg.ModuleDir == "" {
		cfg.ModuleDir = "."
	}
	if err := cfg.validate(); err != nil {
		return RouteMap{}, err
	}

	absDir, err := filepath.Abs(cfg.ModuleDir)
	if err != nil {
		return RouteMap{}, fmt.Errorf("resolving module dir: %w", err)
	}

	loadCfg := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedCompiledGoFiles,
		Dir:     absDir,
		Context: ctx,
	}
	pkgs, err := packages.Load(loadCfg, cfg.PackagePattern)
	if err != nil {
		return RouteMap{}, fmt.Errorf("loading packages: %w", err)
	}

	result := RouteMap{}
	allowed := frameworks.MakeAllowedSet(cfg.Frameworks)

	for _, pkg := range pkgs {
		// Report package-level errors as diagnostics.
		for _, e := range pkg.Errors {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{
				Severity: SeverityInfo,
				Code:     "package-load-error",
				Message:  e.Msg,
			})
		}

		// Build package-scope function map for cross-file following.
		pkgFuncMap := map[string]*ast.FuncDecl{}
		for _, file := range pkg.Syntax {
			fname := pkg.Fset.Position(file.Package).Filename
			if strings.HasSuffix(fname, "_test.go") {
				continue
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil || fn.Recv != nil {
					continue
				}
				pkgFuncMap[fn.Name.Name] = fn
			}
		}

		for _, file := range pkg.Syntax {
			absFilename := pkg.Fset.Position(file.Package).Filename
			// Skip test files that may leak in.
			if strings.HasSuffix(absFilename, "_test.go") {
				continue
			}
			relName := relativePath(absDir, absFilename)
			routes, diags := extractFromAST(pkg.Fset, file, relName, absFilename, allowed, cfg.IncludeMiddleware, pkgFuncMap)
			result.Routes = append(result.Routes, routes...)
			result.Diagnostics = append(result.Diagnostics, diags...)
		}
	}

	stableSortRoutes(result.Routes)

	// Flag duplicate method+path pairs.
	result.Diagnostics = append(result.Diagnostics, detectDuplicateRoutes(result.Routes)...)

	// Only mark partial for warning+ severity diagnostics.
	for _, d := range result.Diagnostics {
		if d.Severity == SeverityWarning || d.Severity == SeverityError {
			result.Partial = true
			break
		}
	}

	return result, nil
}

// relativePath returns path relative to base, or the original path if that fails.
func relativePath(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

// extractFromAST extracts routes from a pre-parsed AST file.
func extractFromAST(fset *token.FileSet, file *ast.File, filename, absFilename string, allowed map[string]bool, includeMiddleware bool, pkgFuncMap map[string]*ast.FuncDecl) ([]Route, []Diagnostic) {
	aliases := parseImportAliases(file)
	sfm := analysis.ScanStructFieldRouters(file, aliases)
	consts := analysis.CollectConstants(file)
	var routes []Route
	var diags []Diagnostic

	// Build a map of all free function declarations in this file for intra-file following.
	funcMap := map[string]*ast.FuncDecl{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Recv != nil {
			continue
		}
		funcMap[fn.Name.Name] = fn
	}

	// Merge package-scope functions (cross-file); per-file takes precedence.
	for name, fn := range pkgFuncMap {
		if _, exists := funcMap[name]; !exists {
			funcMap[name] = fn
		}
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		env := map[string]analysis.RouterContext{}

		// Seed method receiver env from struct field tracking.
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			recvName, recvType := analysis.ParseReceiver(fn.Recv.List[0])
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

		routes = append(routes, walkBlock(fset, filename, absFilename, fn.Body, env, aliases, consts, allowed, includeMiddleware, &diags, funcMap, 0)...)
	}

	// Emit diagnostic when a framework is imported with route-like calls but
	// no routes were extracted. Skip files that only import for types (e.g., fiber.Ctx).
	if len(routes) == 0 && frameworks.HasRoutelikeCalls(file) {
		for _, fw := range frameworks.DetectedFrameworks(aliases, allowed) {
			diags = append(diags, Diagnostic{
				Severity: SeverityInfo,
				Code:     "no-routes-found",
				Message:  fmt.Sprintf("framework %q imported but no routes extracted", fw),
				File:     filename,
			})
		}
	}

	return routes, diags
}

func walkBlock(
	fset *token.FileSet,
	filename string,
	absFilename string,
	block *ast.BlockStmt,
	env map[string]analysis.RouterContext,
	aliases map[string]string,
	consts map[string]string,
	allowed map[string]bool,
	includeMiddleware bool,
	diags *[]Diagnostic,
	funcMap map[string]*ast.FuncDecl,
	depth int,
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
			routes = append(routes, handleCall(fset, filename, absFilename, call, env, aliases, consts, allowed, includeMiddleware, diags, funcMap, depth)...)
		case *ast.IfStmt:
			routes = append(routes, markConditional(walkBlock(fset, filename, absFilename, s.Body, cloneEnv(env), aliases, consts, allowed, includeMiddleware, diags, funcMap, depth))...)
			if s.Else != nil {
				routes = append(routes, markConditional(walkElse(fset, filename, absFilename, s.Else, cloneEnv(env), aliases, consts, allowed, includeMiddleware, diags, funcMap, depth))...)
			}
		case *ast.ForStmt:
			if s.Body != nil {
				routes = append(routes, walkBlock(fset, filename, absFilename, s.Body, cloneEnv(env), aliases, consts, allowed, includeMiddleware, diags, funcMap, depth)...)
			}
		case *ast.RangeStmt:
			if s.Body != nil {
				routes = append(routes, walkBlock(fset, filename, absFilename, s.Body, cloneEnv(env), aliases, consts, allowed, includeMiddleware, diags, funcMap, depth)...)
			}
		case *ast.SwitchStmt:
			if s.Body != nil {
				for _, clause := range s.Body.List {
					cc, ok := clause.(*ast.CaseClause)
					if !ok {
						continue
					}
					routes = append(routes, markConditional(walkBlock(fset, filename, absFilename, &ast.BlockStmt{List: cc.Body}, cloneEnv(env), aliases, consts, allowed, includeMiddleware, diags, funcMap, depth))...)
				}
			}
		case *ast.TypeSwitchStmt:
			if s.Body != nil {
				for _, clause := range s.Body.List {
					cc, ok := clause.(*ast.CaseClause)
					if !ok {
						continue
					}
					routes = append(routes, markConditional(walkBlock(fset, filename, absFilename, &ast.BlockStmt{List: cc.Body}, cloneEnv(env), aliases, consts, allowed, includeMiddleware, diags, funcMap, depth))...)
				}
			}
		case *ast.GoStmt:
			if s.Call != nil {
				routes = append(routes, handleCall(fset, filename, absFilename, s.Call, env, aliases, consts, allowed, includeMiddleware, diags, funcMap, depth)...)
			}
		case *ast.DeferStmt:
			if s.Call != nil {
				routes = append(routes, handleCall(fset, filename, absFilename, s.Call, env, aliases, consts, allowed, includeMiddleware, diags, funcMap, depth)...)
			}
		case *ast.SelectStmt:
			if s.Body != nil {
				for _, clause := range s.Body.List {
					cc, ok := clause.(*ast.CommClause)
					if !ok {
						continue
					}
					routes = append(routes, walkBlock(fset, filename, absFilename, &ast.BlockStmt{List: cc.Body}, cloneEnv(env), aliases, consts, allowed, includeMiddleware, diags, funcMap, depth)...)
				}
			}
		case *ast.LabeledStmt:
			// Unwrap the labeled statement and process the inner statement.
			routes = append(routes, walkBlock(fset, filename, absFilename, &ast.BlockStmt{List: []ast.Stmt{s.Stmt}}, env, aliases, consts, allowed, includeMiddleware, diags, funcMap, depth)...)
		}
	}
	return routes
}

func walkElse(fset *token.FileSet, filename string, absFilename string, elseStmt ast.Stmt, env map[string]analysis.RouterContext, aliases map[string]string, consts map[string]string, allowed map[string]bool, includeMiddleware bool, diags *[]Diagnostic, funcMap map[string]*ast.FuncDecl, depth int) []Route {
	switch s := elseStmt.(type) {
	case *ast.BlockStmt:
		return walkBlock(fset, filename, absFilename, s, env, aliases, consts, allowed, includeMiddleware, diags, funcMap, depth)
	case *ast.IfStmt:
		routes := walkBlock(fset, filename, absFilename, s.Body, cloneEnv(env), aliases, consts, allowed, includeMiddleware, diags, funcMap, depth)
		if s.Else != nil {
			routes = append(routes, walkElse(fset, filename, absFilename, s.Else, cloneEnv(env), aliases, consts, allowed, includeMiddleware, diags, funcMap, depth)...)
		}
		return routes
	}
	return nil
}

// markConditional sets Conditional=true on all routes in the slice.
func markConditional(routes []Route) []Route {
	for i := range routes {
		routes[i].Conditional = true
	}
	return routes
}

func handleAssignment(env map[string]analysis.RouterContext, s *ast.AssignStmt, aliases map[string]string) {
	if len(s.Lhs) != len(s.Rhs) {
		return
	}
	for i := range s.Lhs {
		key, ok := analysis.ResolveRouterKey(s.Lhs[i])
		if !ok {
			continue
		}
		ctx, ok := deriveContext(s.Rhs[i], env, aliases)
		if ok {
			env[key] = ctx
		}
	}
}

func handleDeclStmt(env map[string]analysis.RouterContext, s *ast.DeclStmt, aliases map[string]string) {
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

func deriveContext(expr ast.Expr, env map[string]analysis.RouterContext, aliases map[string]string) (analysis.RouterContext, bool) {
	switch e := expr.(type) {
	case *ast.Ident:
		ctx, ok := env[e.Name]
		return ctx, ok
	case *ast.CallExpr:
		if fw, ok := frameworks.ConstructorFramework(e, aliases); ok {
			return analysis.RouterContext{Framework: fw, Prefix: ""}, true
		}
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			key, ok := analysis.ResolveRouterKey(sel.X)
			if !ok {
				return analysis.RouterContext{}, false
			}
			parent, ok := env[key]
			if !ok {
				return analysis.RouterContext{}, false
			}
			switch strings.ToLower(sel.Sel.Name) {
			case "group":
				if len(e.Args) == 0 {
					return parent, true
				}
				p := analysis.ReadStringArg(e.Args[0])
				ctx := parent
				ctx.Prefix = analysis.JoinPath(parent.Prefix, p)
				ctx.Middleware = append([]string{}, parent.Middleware...)
				if parent.Framework == "fiber" || parent.Framework == "gin" || parent.Framework == "echo" {
					for _, arg := range e.Args[1:] {
						ctx.Middleware = append(ctx.Middleware, analysis.ExprString(arg))
					}
				}
				return ctx, true
			case "with":
				ctx := parent
				ctx.Middleware = append([]string{}, parent.Middleware...)
				for _, arg := range e.Args {
					ctx.Middleware = append(ctx.Middleware, analysis.ExprString(arg))
				}
				return ctx, true
			}
		}
	}
	return analysis.RouterContext{}, false
}

func handleCall(
	fset *token.FileSet,
	filename string,
	absFilename string,
	call *ast.CallExpr,
	env map[string]analysis.RouterContext,
	aliases map[string]string,
	consts map[string]string,
	allowed map[string]bool,
	includeMiddleware bool,
	diags *[]Diagnostic,
	funcMap map[string]*ast.FuncDecl,
	depth int,
) []Route {
	// Handle immediately-invoked func literals: func() { r.GET(...) }()
	if fn, ok := call.Fun.(*ast.FuncLit); ok {
		if fn.Body != nil {
			return walkBlock(fset, filename, absFilename, fn.Body, cloneEnv(env), aliases, consts, allowed, includeMiddleware, diags, funcMap, depth)
		}
		return nil
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		// Try to follow plain function calls that pass a known router.
		if depth < 3 {
			if ident, ok := call.Fun.(*ast.Ident); ok {
				if callee, ok := funcMap[ident.Name]; ok {
					for i, arg := range call.Args {
						if key, ok := analysis.ResolveRouterKey(arg); ok {
							if ctx, ok := env[key]; ok {
								if i < len(callee.Type.Params.List) {
									paramNames := callee.Type.Params.List[i].Names
									if len(paramNames) > 0 {
										childEnv := cloneEnv(env)
										childEnv[paramNames[0].Name] = ctx
										calleeAbsFile := fset.Position(callee.Pos()).Filename
										crossFile := calleeAbsFile != absFilename
										walkFilename := filename
										walkAbsFilename := absFilename
										if crossFile {
											pkgDir := filepath.Dir(absFilename)
											walkAbsFilename = calleeAbsFile
											walkFilename = relativePath(pkgDir, calleeAbsFile)
										}
										inner := walkBlock(fset, walkFilename, walkAbsFilename, callee.Body, childEnv, aliases, consts, allowed, includeMiddleware, diags, funcMap, depth+1)
										if crossFile {
											for j := range inner {
												if inner[j].Confidence == ConfidenceExact {
													inner[j].Confidence = ConfidenceInferred
													inner[j].InferredBy = "cross-file"
												}
											}
										}
										return inner
									}
								}
							}
						}
					}
				}
			}
		}
		return nil
	}

	method := sel.Sel.Name
	lowerMethod := strings.ToLower(method)

	if key, ok := analysis.ResolveRouterKey(sel.X); ok {
		ctx, ok := env[key]
		if !ok {
			return nil
		}
		if !frameworks.AllowedFramework(ctx.Framework, allowed) {
			return nil
		}

		if lowerMethod == "use" {
			for _, arg := range call.Args {
				ctx.Middleware = append(ctx.Middleware, analysis.ExprString(arg))
			}
			env[key] = ctx
			return nil
		}

		if routeMethod := frameworks.RouteMethodForFramework(ctx.Framework, method, call.Args); routeMethod != "" {
			r, ok := buildRoute(fset, filename, call, ctx, routeMethod, consts, includeMiddleware)
			if ok {
				return []Route{r}
			}
			*diags = append(*diags, Diagnostic{Severity: SeverityWarning, Code: "unparseable-route", Message: "unable to parse route call", File: filename, Line: fset.Position(call.Pos()).Line})
			return nil
		}

		if ctx.Framework == "chi" && lowerMethod == "route" && len(call.Args) >= 2 {
			prefix := analysis.ReadStringArgWithConsts(call.Args[0], consts)
			next := ctx
			next.Prefix = analysis.JoinPath(ctx.Prefix, prefix)
			if fn, ok := call.Args[1].(*ast.FuncLit); ok && len(fn.Type.Params.List) > 0 && len(fn.Type.Params.List[0].Names) > 0 {
				childName := fn.Type.Params.List[0].Names[0].Name
				childEnv := cloneEnv(env)
				childEnv[childName] = next
				return walkBlock(fset, filename, absFilename, fn.Body, childEnv, aliases, consts, allowed, includeMiddleware, diags, funcMap, depth)
			}
		}
	}

	if frameworks.IsNetHTTPGlobalHandle(call, aliases) && frameworks.AllowedFramework("nethttp", allowed) {
		r, ok := buildGlobalNetHTTPRoute(fset, filename, call, consts, includeMiddleware)
		if ok {
			return []Route{r}
		}
	}

	return nil
}

func buildRoute(fset *token.FileSet, filename string, call *ast.CallExpr, ctx analysis.RouterContext, method string, consts map[string]string, includeMiddleware bool) (Route, bool) {
	pathArg, handlerArg, routeMW := parseRouteArgs(ctx.Framework, call, consts)
	if pathArg == "" || handlerArg == nil {
		return Route{}, false
	}
	if ctx.Framework == "nethttp" {
		parsedMethod, parsedPath := frameworks.ParseNetHTTPPattern(pathArg)
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
		Path:       analysis.JoinPath(ctx.Prefix, pathArg),
		Handler:    analysis.ExprString(handlerArg),
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
	a, ok := frameworks.GetAdapter(framework)
	if !ok {
		return "", nil, nil
	}
	return a.ParseRouteArgs(call.Args, consts)
}

func buildGlobalNetHTTPRoute(fset *token.FileSet, filename string, call *ast.CallExpr, consts map[string]string, includeMiddleware bool) (Route, bool) {
	if len(call.Args) < 2 {
		return Route{}, false
	}
	pattern := analysis.ReadStringArgWithConsts(call.Args[0], consts)
	if pattern == "" {
		return Route{}, false
	}
	method, path := frameworks.ParseNetHTTPPattern(pattern)
	pos := fset.Position(call.Pos())
	r := Route{
		Method:     method,
		Path:       path,
		Handler:    analysis.ExprString(call.Args[1]),
		Framework:  "nethttp",
		File:       filename,
		Line:       pos.Line,
		Confidence: ConfidenceExact,
	}
	if includeMiddleware {
		r.Middleware = nil
	}
	return r, true
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

func cloneEnv(in map[string]analysis.RouterContext) map[string]analysis.RouterContext {
	out := make(map[string]analysis.RouterContext, len(in))
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
				Severity: SeverityWarning,
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
