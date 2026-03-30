package routemap_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/emm5317/routemap"
)

type expectedRoute struct {
	Method      string
	Path        string
	Framework   string
	Confidence  routemap.Confidence
	Conditional bool
}

type expectedDiag struct {
	Code     string
	Severity routemap.Severity
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
				{Method: "GET", Path: "/api/users", Framework: "gin", Confidence: routemap.ConfidenceExact},
			},
		},
		{
			name:       "net/http patterns",
			fixture:    "nethttpcase",
			frameworks: []string{"nethttp"},
			wantCount:  2,
			wantRoutes: []expectedRoute{
				{Method: "GET", Path: "/users/{id}", Framework: "nethttp", Confidence: routemap.ConfidenceExact},
				{Method: "ANY", Path: "/healthz", Framework: "nethttp", Confidence: routemap.ConfidenceExact},
			},
		},
		{
			name:       "fiber basic flat routes",
			fixture:    "fibercase",
			frameworks: []string{"fiber"},
			wantCount:  3,
			wantRoutes: []expectedRoute{
				{Method: "GET", Path: "/", Framework: "fiber", Confidence: routemap.ConfidenceExact},
				{Method: "POST", Path: "/users", Framework: "fiber", Confidence: routemap.ConfidenceExact},
				{Method: "GET", Path: "/users/:id", Framework: "fiber", Confidence: routemap.ConfidenceExact},
			},
		},
		{
			name:       "fiber struct field receiver (betbot pattern)",
			fixture:    "fiberrecv",
			frameworks: []string{"fiber"},
			wantCount:  2,
			wantRoutes: []expectedRoute{
				{Method: "GET", Path: "/", Framework: "fiber", Confidence: routemap.ConfidenceHigh},
				{Method: "POST", Path: "/bets/:id/settle", Framework: "fiber", Confidence: routemap.ConfidenceHigh},
			},
		},
		{
			name:       "chi groups and middleware",
			fixture:    "chicase",
			frameworks: []string{"chi"},
			wantCount:  4,
			wantRoutes: []expectedRoute{
				{Method: "GET", Path: "/", Framework: "chi", Confidence: routemap.ConfidenceExact},
				{Method: "GET", Path: "/api/users", Framework: "chi", Confidence: routemap.ConfidenceExact},
				{Method: "POST", Path: "/api/users", Framework: "chi", Confidence: routemap.ConfidenceExact},
				{Method: "DELETE", Path: "/admin", Framework: "chi", Confidence: routemap.ConfidenceExact},
			},
		},
		{
			name:       "echo groups and middleware",
			fixture:    "echocase",
			frameworks: []string{"echo"},
			wantCount:  3,
			wantRoutes: []expectedRoute{
				{Method: "GET", Path: "/", Framework: "echo", Confidence: routemap.ConfidenceExact},
				{Method: "GET", Path: "/api/items", Framework: "echo", Confidence: routemap.ConfidenceExact},
				{Method: "POST", Path: "/api/items", Framework: "echo", Confidence: routemap.ConfidenceExact},
			},
		},
		{
			name:       "conditional route registration",
			fixture:    "conditional",
			frameworks: []string{"gin"},
			wantCount:  5,
			wantRoutes: []expectedRoute{
				{Method: "GET", Path: "/", Framework: "gin", Confidence: routemap.ConfidenceExact, Conditional: false},
				{Method: "GET", Path: "/admin", Framework: "gin", Confidence: routemap.ConfidenceExact, Conditional: true},
				{Method: "GET", Path: "/guest", Framework: "gin", Confidence: routemap.ConfidenceExact, Conditional: true},
				{Method: "GET", Path: "/metrics", Framework: "gin", Confidence: routemap.ConfidenceExact, Conditional: true},
				{Method: "GET", Path: "/debug", Framework: "gin", Confidence: routemap.ConfidenceExact, Conditional: true},
			},
		},
		{
			name:       "const path strings",
			fixture:    "constpaths",
			frameworks: []string{"fiber"},
			wantCount:  2,
			wantRoutes: []expectedRoute{
				{Method: "GET", Path: "/health", Framework: "fiber", Confidence: routemap.ConfidenceExact},
				{Method: "GET", Path: "/users", Framework: "fiber", Confidence: routemap.ConfidenceExact},
			},
		},
		{
			name:       "string concatenation paths",
			fixture:    "concatpaths",
			frameworks: []string{"fiber"},
			wantCount:  2,
			wantRoutes: []expectedRoute{
				{Method: "GET", Path: "/api/users", Framework: "fiber", Confidence: routemap.ConfidenceExact},
				{Method: "GET", Path: "/v1/health", Framework: "fiber", Confidence: routemap.ConfidenceExact},
			},
		},
		{
			name:       "no routes found diagnostic",
			fixture:    "noresults",
			frameworks: []string{"fiber"},
			wantCount:  0,
			wantDiags:  []expectedDiag{{Code: "no-routes-found", Severity: routemap.SeverityInfo}},
			wantPartial: false, // info diagnostics do not trigger partial
		},
		{
			name:       "duplicate route detection",
			fixture:    "duperoutes",
			frameworks: []string{"gin"},
			wantCount:  3,
			wantDiags:  []expectedDiag{{Code: "duplicate-route", Severity: routemap.SeverityWarning}},
			wantPartial: true,
		},
		{
			name:       "intra-file helper function following",
			fixture:    "helperfunc",
			frameworks: []string{"gin"},
			wantCount:  3,
			wantRoutes: []expectedRoute{
				{Method: "GET", Path: "/", Framework: "gin", Confidence: routemap.ConfidenceExact},
				{Method: "GET", Path: "/api/users", Framework: "gin", Confidence: routemap.ConfidenceExact},
				{Method: "POST", Path: "/api/users", Framework: "gin", Confidence: routemap.ConfidenceExact},
			},
		},
		{
			name:       "cross-file router tracking",
			fixture:    "crossfile",
			frameworks: []string{"gin"},
			wantCount:  3,
			wantRoutes: []expectedRoute{
				{Method: "GET", Path: "/", Framework: "gin", Confidence: routemap.ConfidenceExact},
				{Method: "GET", Path: "/api/users", Framework: "gin", Confidence: routemap.ConfidenceInferred},
				{Method: "POST", Path: "/api/users", Framework: "gin", Confidence: routemap.ConfidenceInferred},
			},
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
				assertRoute(t, i, res.Routes[i], want)
			}

			for _, wantDiag := range tt.wantDiags {
				assertDiagnostic(t, res.Diagnostics, wantDiag)
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

func TestExtractRoutes_EchoMiddlewareOrder(t *testing.T) {
	res, err := routemap.ExtractRoutes(context.Background(), routemap.Config{
		ModuleDir:         fixtureDir(t, "echocase"),
		IncludeMiddleware: true,
		Frameworks:        []string{"echo"},
	})
	if err != nil {
		t.Fatalf("ExtractRoutes() error = %v", err)
	}
	// Check the /api/items route has both LogMW (global) and APIMW (group)
	var apiRoute *routemap.Route
	for i := range res.Routes {
		if res.Routes[i].Path == "/api/items" && res.Routes[i].Method == "GET" {
			apiRoute = &res.Routes[i]
			break
		}
	}
	if apiRoute == nil {
		t.Fatal("expected GET /api/items route")
	}
	want := []string{"LogMW", "APIMW"}
	if len(apiRoute.Middleware) != len(want) {
		t.Fatalf("middleware count: got %d want %d: %v", len(apiRoute.Middleware), len(want), apiRoute.Middleware)
	}
	for i := range want {
		if apiRoute.Middleware[i].Name != want[i] {
			t.Errorf("middleware[%d]: got %q want %q", i, apiRoute.Middleware[i].Name, want[i])
		}
	}
}

func assertRoute(t *testing.T, i int, got routemap.Route, want expectedRoute) {
	t.Helper()
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
	if got.Conditional != want.Conditional {
		t.Errorf("route[%d] conditional: got %v want %v", i, got.Conditional, want.Conditional)
	}
}

func assertDiagnostic(t *testing.T, diags []routemap.Diagnostic, want expectedDiag) {
	t.Helper()
	for _, d := range diags {
		if d.Code == want.Code && d.Severity == want.Severity {
			return
		}
	}
	t.Errorf("expected diagnostic code=%q severity=%q, got %v", want.Code, want.Severity, diags)
}

func fixtureDir(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", "fixtures", name))
	if err != nil {
		t.Fatalf("fixture path: %v", err)
	}
	return p
}

func routesSummary(routes []routemap.Route) string {
	s := ""
	for _, r := range routes {
		s += r.Method + " " + r.Path + " (" + string(r.Confidence) + ")\n"
	}
	return s
}
