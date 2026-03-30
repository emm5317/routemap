package analysis

import (
	"go/ast"
	"go/token"

	"github.com/emm5317/routemap/internal/frameworks"
)

// RouterContext holds the accumulated routing context for a variable at a
// given point in a function body.
type RouterContext struct {
	Framework   string
	Prefix      string
	Middleware  []string
	Inferred    bool // true when resolved via struct-field propagation
	Conditional bool // true when inside a conditional branch (if/switch)
}

// ScanStructFieldRouters performs a pre-pass over all functions in a file to
// find router constructors assigned to struct fields (e.g. s.app = fiber.New()
// or &App{app: fiber.New()}). Returns a map keyed by "TypeName.FieldName".
func ScanStructFieldRouters(file *ast.File, aliases map[string]string) map[string]RouterContext {
	sfm := map[string]RouterContext{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		// Track local variables that may hold routers.
		localEnv := map[string]RouterContext{}
		// Track the type name if this function returns a struct pointer
		// (heuristic for constructors).
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

func scanStmtForFieldRouters(stmt ast.Stmt, localEnv map[string]RouterContext, aliases map[string]string, sfm map[string]RouterContext, returnType string) {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		scanAssignForFieldRouters(s, localEnv, aliases, sfm, returnType)
	case *ast.DeclStmt:
		scanDeclForFieldRouters(s, localEnv, aliases, sfm)
	}
}

func scanAssignForFieldRouters(s *ast.AssignStmt, localEnv map[string]RouterContext, aliases map[string]string, sfm map[string]RouterContext, returnType string) {
	if len(s.Lhs) != len(s.Rhs) {
		return
	}
	for i := range s.Lhs {
		rhs := s.Rhs[i]
		if id, ok := s.Lhs[i].(*ast.Ident); ok && id.Name != "_" {
			if ctx, ok := deriveConstructorOrAlias(rhs, localEnv, aliases); ok {
				localEnv[id.Name] = ctx
			}
			scanCompositeLiteral(rhs, localEnv, aliases, sfm)
		}
		if sel, ok := s.Lhs[i].(*ast.SelectorExpr); ok {
			if base, ok := sel.X.(*ast.Ident); ok {
				if ctx, ok := deriveConstructorOrAlias(rhs, localEnv, aliases); ok {
					typeName := returnType
					if typeName == "" {
						typeName = base.Name
					}
					sfm[typeName+"."+sel.Sel.Name] = ctx
				}
			}
		}
	}
}

func scanDeclForFieldRouters(s *ast.DeclStmt, localEnv map[string]RouterContext, aliases map[string]string, sfm map[string]RouterContext) {
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

// scanCompositeLiteral checks for &Type{field: router} patterns.
func scanCompositeLiteral(expr ast.Expr, localEnv map[string]RouterContext, aliases map[string]string, sfm map[string]RouterContext) {
	unary, ok := expr.(*ast.UnaryExpr)
	if !ok || unary.Op != token.AND {
		return
	}
	comp, ok := unary.X.(*ast.CompositeLit)
	if !ok {
		return
	}
	typeName := TypeNameFromExpr(comp.Type)
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

// deriveConstructorOrAlias returns a RouterContext if the expression is a
// framework constructor call or a reference to a known local router variable.
func deriveConstructorOrAlias(expr ast.Expr, localEnv map[string]RouterContext, aliases map[string]string) (RouterContext, bool) {
	switch e := expr.(type) {
	case *ast.Ident:
		ctx, ok := localEnv[e.Name]
		return ctx, ok
	case *ast.CallExpr:
		if fw, ok := frameworks.ConstructorFramework(e, aliases); ok {
			return RouterContext{Framework: fw}, true
		}
	}
	return RouterContext{}, false
}

// TypeNameFromExpr extracts a type name string from an AST expression.
func TypeNameFromExpr(expr ast.Expr) string {
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

// ParseReceiver extracts the receiver variable name and type name.
// For "func (a *App) method()" returns ("a", "App").
func ParseReceiver(field *ast.Field) (name string, typeName string) {
	if len(field.Names) > 0 {
		name = field.Names[0].Name
	}
	typeName = ReceiverTypeName(field.Type)
	return
}

// ReceiverTypeName returns the base type name from a receiver type expression,
// stripping any pointer indirection.
func ReceiverTypeName(expr ast.Expr) string {
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
