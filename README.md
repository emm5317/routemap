# routemap

[![CI](https://github.com/Emm5317/routemap/actions/workflows/ci.yml/badge.svg)](https://github.com/Emm5317/routemap/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/Emm5317/routemap.svg)](https://pkg.go.dev/github.com/Emm5317/routemap)
[![Go Report Card](https://goreportcard.com/badge/github.com/Emm5317/routemap)](https://goreportcard.com/report/github.com/Emm5317/routemap)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

Library-first static HTTP route extraction for `net/http`, `chi`, `gin`, `echo`, and `fiber`.

Routemap performs AST-based static analysis on Go source files to discover and catalog HTTP routes without executing code. It handles direct constructor patterns, struct field receivers (`a.app.Get(...)`), nested groups, middleware chains, conditional registration, and constant path strings.

## Install

```bash
go install github.com/Emm5317/routemap/cmd/routemap@latest
```

## CLI Usage

```
routemap [flags]

Flags:
  -module-dir string   module directory (default ".")
  -frameworks string   comma-separated frameworks: nethttp,chi,gin,echo,fiber
  -middleware          include resolved middleware chain (default true)
  -format string       output format: text, json, table (default "text")
  -json               output JSON (shorthand for -format json)
  -strict             fail on parse diagnostics (exit code 1)
```

## CLI Examples

```bash
# Text output (default)
routemap -module-dir ./myapp -frameworks gin

# JSON output
routemap -module-dir ./myapp -frameworks fiber -json

# Markdown table for documentation
routemap -module-dir ./myapp -frameworks chi -format table
```

### Output Formats

**Text** (default):
```
[fiber] GET     /                         a.handleHome (server.go:159)
  middleware: requestid.New() recover.New()
[fiber] POST    /bets/:id/settle          a.handleSettle (server.go:186)
```

**Table** (`-format table`):
```
| Method | Path | Handler | Framework | File | Line | Confidence |
|--------|------|---------|-----------|------|------|------------|
| GET    | /    | a.handleHome | fiber | server.go | 159 | inferred |
| POST   | /bets/:id/settle | a.handleSettle | fiber | server.go | 186 | inferred |
```

**JSON** (`-json` or `-format json`):
```json
{
  "routes": [
    {
      "method": "GET",
      "path": "/",
      "handler": "a.handleHome",
      "framework": "fiber",
      "file": "server.go",
      "line": 159,
      "confidence": "inferred",
      "inferred_by": "struct-field"
    }
  ],
  "diagnostics": [],
  "partial": false
}
```

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Error, or strict mode with diagnostics present |
| 2 | Partial results (warning-level diagnostics present) |

## API Example

```go
package main

import (
	"context"
	"fmt"

	"github.com/Emm5317/routemap/pkg/routemap"
)

func main() {
	res, err := routemap.ExtractRoutes(context.Background(), routemap.Config{
		ModuleDir:         ".",
		Frameworks:        []string{"gin", "chi", "nethttp"},
		IncludeMiddleware: true,
	})
	if err != nil {
		panic(err)
	}

	for _, r := range res.Routes {
		fmt.Printf("[%s] %s %s -> %s (%s)\n", r.Confidence, r.Method, r.Path, r.Handler, r.Framework)
	}
}
```

## What It Detects

| Pattern | Example | Confidence |
|---------|---------|------------|
| Direct constructor + route calls | `r := gin.New(); r.GET("/users", h)` | `exact` |
| Struct field receivers | `a.app.Get("/users", h)` where `app *fiber.App` | `inferred` |
| Router groups | `api := r.Group("/api"); api.GET("/users", h)` | `exact` |
| Middleware chains | `r.Use(Auth, Log)` | -- |
| Chi nested routes | `r.Route("/api", func(sub chi.Router) { ... })` | `exact` |
| Chi `.With()` middleware | `auth := r.With(AuthMW); auth.Get("/admin", h)` | `exact` |
| Conditional registration | `if flag { r.GET("/admin", h) }` | `exact` |
| Constant path strings | `const P = "/health"; app.Get(P, h)` | `exact` |
| `net/http` pattern methods | `mux.HandleFunc("GET /users/{id}", h)` | `exact` |
| Versioned module imports | `github.com/gofiber/fiber/v3` | -- |

## Confidence Levels

Each extracted route includes a `confidence` field:

- **`exact`**: Fully tracked from constructor to route call within the same function scope.
- **`inferred`**: Router identity resolved via struct-field propagation across functions. The `inferred_by` field explains how (e.g., `"struct-field"`).

## Diagnostics

Routemap emits structured diagnostics with machine-readable codes:

| Code | Severity | Meaning |
|------|----------|---------|
| `no-routes-found` | info | Framework imported with route-like calls but no routes extracted |
| `duplicate-route` | warning | Same method+path registered at multiple locations |
| `unparseable-route` | warning | Route call detected but could not be parsed |
| `unused-config` | info | `PackagePattern` config field is set but currently ignored |

Only `warning` and `error` severity diagnostics set `Partial: true` and trigger exit code 2. Info-level diagnostics are informational and do not affect the exit code.

## Frameworks

| Framework | Constructor | Import Path |
|-----------|-------------|-------------|
| net/http | `http.NewServeMux()` | `net/http` |
| chi | `chi.NewRouter()` | `github.com/go-chi/chi` |
| gin | `gin.New()`, `gin.Default()` | `github.com/gin-gonic/gin` |
| echo | `echo.New()` | `github.com/labstack/echo` |
| fiber | `fiber.New()` | `github.com/gofiber/fiber` |

Versioned module paths (`/v2`, `/v3`, etc.) are handled automatically.

## Limitations

- **Static analysis only**: Cannot detect routes created via reflection or dynamic code.
- **String arguments**: Path arguments must be string literals or file-level constants (not computed expressions or variables).
- **Cross-file tracking**: Struct field propagation works within a single file. Router constructors and route registrations must be in the same `.go` file.
- **Helper wrappers**: Routes registered through custom helper functions (e.g., `registerRoutes(r, "/api")`) are not followed.

## Update Control

The repo is configured so contributor PRs must be approved by `@Emm5317`.

- `CODEOWNERS` assigns ownership to `@Emm5317`.
- `pr-approval-gate` requires `Emm5317` approval on contributor PRs.
- Enable branch protection on `main` in GitHub Settings:
  - Require pull request before merge.
  - Require Code Owner reviews.
  - Require status checks: `ci` and `pr-approval-gate / approval`.
  - Restrict push access to `Emm5317`.

## License

MIT. See [LICENSE](LICENSE).
