package routemap_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/emm5317/routemap/pkg/routemap"
)

func TestExtractRoutes_GinMiddlewareChain(t *testing.T) {
	res, err := routemap.ExtractRoutes(context.Background(), routemap.Config{
		ModuleDir:         fixtureDir(t, "gincase"),
		IncludeMiddleware: true,
		Frameworks:        []string{"gin"},
	})
	if err != nil {
		t.Fatalf("ExtractRoutes() error = %v", err)
	}
	if len(res.Routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(res.Routes))
	}
	r := res.Routes[0]
	if r.Framework != "gin" || r.Method != "GET" || r.Path != "/api/users" {
		t.Fatalf("unexpected route: %#v", r)
	}
	want := []string{"Auth", "Log", "APIMW", "RouteMW"}
	if len(r.Middleware) != len(want) {
		t.Fatalf("middleware count mismatch: got %v", r.Middleware)
	}
	for i := range want {
		if r.Middleware[i].Name != want[i] {
			t.Fatalf("middleware order mismatch at %d: got %q want %q", i, r.Middleware[i].Name, want[i])
		}
	}
}

func TestExtractRoutes_NetHTTPPatterns(t *testing.T) {
	res, err := routemap.ExtractRoutes(context.Background(), routemap.Config{
		ModuleDir:         fixtureDir(t, "nethttpcase"),
		IncludeMiddleware: true,
		Frameworks:        []string{"nethttp"},
	})
	if err != nil {
		t.Fatalf("ExtractRoutes() error = %v", err)
	}
	if len(res.Routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(res.Routes))
	}
	if res.Routes[0].Method != "GET" || res.Routes[0].Path != "/users/{id}" {
		t.Fatalf("unexpected first route: %#v", res.Routes[0])
	}
	if res.Routes[1].Method != "ANY" || res.Routes[1].Path != "/healthz" {
		t.Fatalf("unexpected second route: %#v", res.Routes[1])
	}
}

func fixtureDir(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", name))
	if err != nil {
		t.Fatalf("fixture path: %v", err)
	}
	return p
}
