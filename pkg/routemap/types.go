package routemap

import "fmt"

// Confidence levels for extracted routes.
const (
	ConfidenceExact    = "exact"    // fully tracked from constructor to route call
	ConfidenceInferred = "inferred" // router identity resolved via struct-field propagation or heuristic
)

// Config controls route extraction behavior.
type Config struct {
	ModuleDir         string
	PackagePattern    string // reserved for future package-aware loaders; currently unused
	Frameworks        []string
	IncludeMiddleware bool
	Strict            bool // checked by the CLI to fail on diagnostics (exit code 1); not used by the library
}

// RouteMap is the top-level response.
type RouteMap struct {
	Routes      []Route      `json:"routes"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Partial     bool         `json:"partial"`
}

// Route is a normalized representation of one HTTP route.
type Route struct {
	Method     string          `json:"method"`
	Path       string          `json:"path"`
	Handler    string          `json:"handler"`
	Framework  string          `json:"framework"`
	File       string          `json:"file"`
	Line       int             `json:"line"`
	Middleware []MiddlewareRef `json:"middleware,omitempty"`
	GroupPath  string          `json:"group_path,omitempty"`
	Confidence string          `json:"confidence"`
	InferredBy string          `json:"inferred_by,omitempty"`
}

// MiddlewareRef identifies middleware in a resolved chain.
type MiddlewareRef struct {
	Name string `json:"name"`
}

// Diagnostic describes limitations or parse issues.
type Diagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
}

func (c Config) validate() error {
	if c.ModuleDir == "" {
		c.ModuleDir = "."
	}
	if len(c.Frameworks) == 0 {
		return nil
	}
	for _, f := range c.Frameworks {
		switch normalizeFramework(f) {
		case "nethttp", "chi", "gin", "echo", "fiber":
		default:
			return fmt.Errorf("unsupported framework %q", f)
		}
	}
	return nil
}
