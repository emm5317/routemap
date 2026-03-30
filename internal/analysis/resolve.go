package analysis

import (
	"bytes"
	"go/ast"
	"go/printer"
	"go/token"
	"strconv"
	"strings"
)

// ResolveRouterKey extracts a string key from an expression for env lookups.
// Returns "r" for Ident("r"), "a.app" for SelectorExpr{X: Ident("a"), Sel: "app"}.
func ResolveRouterKey(expr ast.Expr) (string, bool) {
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

// ReadStringArg reads a string literal from an AST expression.
func ReadStringArg(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	v, err := UnquoteLiteral(lit.Value)
	if err != nil {
		return ""
	}
	return v
}

// ReadStringArgWithConsts extends ReadStringArg by also resolving identifiers
// against a map of file-level string constants, and string concatenation
// expressions (e.g., "/api" + "/v1").
func ReadStringArgWithConsts(expr ast.Expr, consts map[string]string) string {
	if s := ReadStringArg(expr); s != "" {
		return s
	}
	if id, ok := expr.(*ast.Ident); ok {
		return consts[id.Name]
	}
	// Handle binary string concatenation: "/api" + "/v1"
	if bin, ok := expr.(*ast.BinaryExpr); ok && bin.Op == token.ADD {
		left := ReadStringArgWithConsts(bin.X, consts)
		right := ReadStringArgWithConsts(bin.Y, consts)
		if left != "" && right != "" {
			return left + right
		}
	}
	return ""
}

// CollectConstants gathers package-level const string declarations from a file.
func CollectConstants(file *ast.File) map[string]string {
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
					if s := ReadStringArg(vs.Values[i]); s != "" {
						consts[name.Name] = s
					}
				}
			}
		}
	}
	return consts
}

// UnquoteLiteral unquotes a Go string literal value.
func UnquoteLiteral(v string) (string, error) {
	return strconv.Unquote(v)
}

// ExprString renders an AST expression back to its source representation.
func ExprString(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	var b bytes.Buffer
	_ = printer.Fprint(&b, token.NewFileSet(), expr)
	return b.String()
}

// JoinPath joins a route prefix and path segment, normalising slashes.
func JoinPath(prefix, path string) string {
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
