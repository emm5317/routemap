package routemap

import (
	"fmt"

	"github.com/emm5317/routemap/internal/frameworks"
)

// Confidence indicates how the router identity was resolved.
type Confidence string

const (
	// ConfidenceExact means the route was fully tracked from constructor to route call.
	ConfidenceExact Confidence = "exact"
	// ConfidenceInferred means the router identity was resolved via struct-field propagation or heuristic.
	ConfidenceInferred Confidence = "inferred"
)

// Severity indicates the importance of a diagnostic.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Config controls route extraction behavior.
type Config struct {
	// ModuleDir is the root directory of the Go module to analyze.
	ModuleDir string

	// PackagePattern is the pattern passed to the package loader (e.g., "./..." or "./cmd/...").
	// Defaults to "./..." if empty.
	PackagePattern string

	// Frameworks limits extraction to the listed frameworks.
	// An empty slice means all supported frameworks.
	Frameworks []string

	// IncludeMiddleware controls whether resolved middleware chains are included.
	IncludeMiddleware bool
}

// RouteMap is the top-level response.
type RouteMap struct {
	Routes      []Route      `json:"routes"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Partial     bool         `json:"partial"`
}

// Route is a normalized representation of one HTTP route.
type Route struct {
	Method      string          `json:"method"`
	Path        string          `json:"path"`
	Handler     string          `json:"handler"`
	Framework   string          `json:"framework"`
	File        string          `json:"file"`
	Line        int             `json:"line"`
	Middleware  []MiddlewareRef `json:"middleware,omitempty"`
	GroupPath   string          `json:"group_path,omitempty"`
	Confidence  Confidence      `json:"confidence"`
	InferredBy  string          `json:"inferred_by,omitempty"`
	Conditional bool            `json:"conditional,omitempty"`
}

// MiddlewareRef identifies middleware in a resolved chain.
type MiddlewareRef struct {
	Name string `json:"name"`
}

// Diagnostic describes limitations or parse issues.
type Diagnostic struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code,omitempty"`
	Message  string   `json:"message"`
	File     string   `json:"file,omitempty"`
	Line     int      `json:"line,omitempty"`
}

func (c *Config) validate() error {
	if c.ModuleDir == "" {
		c.ModuleDir = "."
	}
	if c.PackagePattern == "" {
		c.PackagePattern = "./..."
	}
	if len(c.Frameworks) == 0 {
		return nil
	}
	for _, f := range c.Frameworks {
		switch frameworks.NormalizeFramework(f) {
		case "nethttp", "chi", "gin", "echo", "fiber":
		default:
			return fmt.Errorf("unsupported framework %q", f)
		}
	}
	return nil
}
