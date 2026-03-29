package routemap_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/emm5317/routemap/pkg/routemap"
)

type expectedRoute struct {
	Method     string
	Path       string
	Framework  string
	Confidence string
}

type expectedDiag struct {
	Code     string
	Severity string
}

func TestExtractRoutes(t *testing.T) {
	tests := []struct {
		name       string
		fixture    string
		frameworks []string
		wantCount  int
		wantRoutes []expectedRoute
		wantDiags  []expectedDiag
		wantPartial bool
	}{
		{
			name:       "gin middleware chain",
			fixture:    "gincase",
			frameworks: []string{"gin"},
			wantCount:  1,
			wantRoutes: []expectedRoute{
				{Method: "GET", Path: "/api/users", Framework: "gin", Confidence: "exact"},
			},
		},
		{
			name:       "net/http patterns",
			fixture:    "nethttpcase",
			frameworks: []string{"nethttp"},
			wantCount:  2,
			wantRoutes: []expectedRoute{
				{Method: "GET", Path: "/users/{id}", Framework: "nethttp", Confidence: "exact"},
				{Method: "ANY", Path: "/healthz", Framework: "nethttp", Confidence: "exact"},
			},
		},
		{
			name:       "fiber basic flat routes",
			fixture:    "fibercase",
			frameworks: []string{"fiber"},
			wantCount:  3,
			wantRoutes: []expectedRoute{
				{Method: "GET", Path: "/", Framework: "fiber", Confidence: "exact"},
				{Method: "POST", Path: "/users", Framework: "fiber", Confidence: "exact"},
				{Method: "GET", Path: "/users/:id", Framework: "fiber", Confidence: "exact"},
			},
		},
		{
			name:       "fiber struct field receiver (betbot pattern)",
			fixture:    "fiberrecv",
			frameworks: []string{"fiber"},
			wantCount:  2,
			wantRoutes: []expectedRoute{
				{Method: "GET", Path: "/", Framework: "fiber", Confidence: "inferred"},
				{Method: "POST", Path: "/bets/:id/settle", Framework: "fiber", Confidence: "inferred"},
			},
		},
		{
			name:       "chi groups and middleware",
			fixture:    "chicase",
			frameworks: []string{"chi"},
			wantCount:  4,
			wantRoutes: []expectedRoute{
				{Method: "GET", Path: "/", Framework: "chi", Confidence: "exact"},
				{Method: "GET", Path: "/api/users", Framework: "chi", Confidence: "exact"},
				{Method: "POST", Path: "/api/users", Framework: "chi", Confidence: "exact"},
				{Method: "DELETE", Path: "/admin", Framework: "chi", Confidence: "exact"},
			},
		},
		{
			name:       "echo groups and middleware",
			fixture:    "echocase",
			frameworks: []string{"echo"},
			wantCount:  3,
			wantRoutes: []expectedRoute{
				{Method: "GET", Path: "/", Framework: "echo", Confidence: "exact"},
				{Method: "GET", Path: "/api/items", Framework: "echo", Confidence: "exact"},
				{Method: "POST", Path: "/api/items", Framework: "echo", Confidence: "exact"},
			},
		},
		{
			name:       "conditional route registration",
			fixture:    "conditional",
			frameworks: []string{"gin"},
			wantCount:  5,
			wantRoutes: []expectedRoute{
				{Method: "GET", Path: "/", Framework: "gin", Confidence: "exact"},
				{Method: "GET", Path: "/admin", Framework: "gin", Confidence: "exact"},
				{Method: "GET", Path: "/guest", Framework: "gin", Confidence: "exact"},
				{Method: "GET", Path: "/metrics", Framework: "gin", Confidence: "exact"},
				{Method: "GET", Path: "/debug", Framework: "gin", Confidence: "exact"},
			},
		},
		{
			name:       "const path strings",
			fixture:    "constpaths",
			frameworks: []string{"fiber"},
			wantCount:  2,
			wantRoutes: []expectedRoute{
				{Method: "GET", Path: "/health", Framework: "fiber", Confidence: "exact"},
				{Method: "GET", Path: "/users", Framework: "fiber", Confidence: "exact"},
			},
		},
		{
			name:       "no routes found diagnostic",
			fixture:    "noresults",
			frameworks: []string{"fiber"},
			wantCount:  0,
			wantDiags:  []expectedDiag{{Code: "no-routes-found", Severity: "info"}},
			wantPartial: false, // info diagnostics do not trigger partial
		},
		{
			name:       "duplicate route detection",
			fixture:    "duperoutes",
			frameworks: []string{"gin"},
			wantCount:  3,
			wantDiags:  []expectedDiag{{Code: "duplicate-route", Severity: "warning"}},
			wantPartial: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := routemap.ExtractRoutes(context.Background(), routemap.Config{
				ModuleDir:         fixtureDir(t, tt.fixture),
				IncludeMiddleware: true,
				Frameworks:        tt.frameworks,
			})
			if err != nil {
				t.Fatalf("ExtractRoutes() error = %v", err)
			}

			if len(res.Routes) != tt.wantCount {
				t.Fatalf("expected %d routes, got %d:\n%v", tt.wantCount, len(res.Routes), routesSummary(res.Routes))
			}

			for i, want := range tt.wantRoutes {
				if i >= len(res.Routes) {
					break
				}
				got := res.Routes[i]
				if got.Method != want.Method {
					t.Errorf("route[%d] method: got %q want %q", i, got.Method, want.Method)
				}
				if got.Path != want.Path {
					t.Errorf("route[%d] path: got %q want %q", i, got.Path, want.Path)
				}
				if got.Framework != want.Framework {
					t.Errorf("route[%d] framework: got %q want %q", i, got.Framework, want.Framework)
				}
				if got.Confidence != want.Confidence {
					t.Errorf("route[%d] confidence: got %q want %q", i, got.Confidence, want.Confidence)
				}
			}

			for _, wantDiag := range tt.wantDiags {
				found := false
				for _, d := range res.Diagnostics {
					if d.Code == wantDiag.Code && d.Severity == wantDiag.Severity {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected diagnostic code=%q severity=%q, got %v", wantDiag.Code, wantDiag.Severity, res.Diagnostics)
				}
			}

			if res.Partial != tt.wantPartial {
				t.Errorf("partial: got %v want %v", res.Partial, tt.wantPartial)
			}
		})
	}
}

func TestExtractRoutes_GinMiddlewareOrder(t *testing.T) {
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

func TestExtractRoutes_PackagePatternDiagnostic(t *testing.T) {
	res, err := routemap.ExtractRoutes(context.Background(), routemap.Config{
		ModuleDir:      fixtureDir(t, "gincase"),
		PackagePattern: "./...",
		Frameworks:     []string{"gin"},
	})
	if err != nil {
		t.Fatalf("ExtractRoutes() error = %v", err)
	}
	found := false
	for _, d := range res.Diagnostics {
		if d.Code == "unused-config" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected unused-config diagnostic when PackagePattern is set")
	}
	if res.Partial {
		t.Error("info-level diagnostic should not set Partial=true")
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

func routesSummary(routes []routemap.Route) string {
	s := ""
	for _, r := range routes {
		s += r.Method + " " + r.Path + " (" + r.Confidence + ")\n"
	}
	return s
}
