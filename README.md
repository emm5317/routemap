<p align="center">
  <img src="logo.png" alt="routemap logo" width="300">
</p>

# routemap

[![CI](https://github.com/emm5317/routemap/actions/workflows/ci.yml/badge.svg)](https://github.com/emm5317/routemap/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/emm5317/routemap.svg)](https://pkg.go.dev/github.com/emm5317/routemap)
[![Go Report Card](https://goreportcard.com/badge/github.com/emm5317/routemap)](https://goreportcard.com/report/github.com/emm5317/routemap)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

Library-first static HTTP route extraction for `net/http`, `chi`, `gin`, `echo`, and `fiber`.

Routemap uses `go/packages` to load Go modules with full build-tag and file-selection awareness, then performs AST-based static analysis to discover and catalog HTTP routes without executing code. It handles direct constructor patterns, struct field receivers, nested groups, middleware chains, conditional registration, constant and concatenated path strings, helper function following (intra-file and cross-file), and versioned module imports.

## Install

```bash
go install github.com/emm5317/routemap/cmd/routemap@latest
```

## CLI Usage

```
routemap [flags]

Flags:
  -module-dir string    module directory (default ".")
  -package string       package pattern to load (default "./...")
  -frameworks string    comma-separated frameworks: nethttp,chi,gin,echo,fiber
  -middleware           include resolved middleware chain (default true)
  -format string        output format: text, json, table (default "text")
  -json                output JSON (shorthand for -format json)
  -strict              fail on parse diagnostics (exit code 1)
  -method string        filter routes by HTTP method
  -path-prefix string   filter routes by path prefix
  -fail-on-empty       exit 1 when no routes found
```

## CLI Examples

```bash
# Text output (default)
routemap -module-dir ./myapp -frameworks gin

# JSON output
routemap -module-dir ./myapp -frameworks fiber -json

# Markdown table for documentation
routemap -module-dir ./myapp -frameworks chi -format table

# Filter to only GET routes under /api
routemap -module-dir ./myapp -method GET -path-prefix /api

# CI: fail if no routes found
routemap -module-dir ./myapp -fail-on-empty
```

### Output Formats

**Text** (default):
```
[fiber] GET     /                         a.handleHome (server.go:159)
  middleware: requestid.New() recover.New()
[fiber] POST    /bets/:id/settle          a.handleSettle (server.go:186)

Found 2 routes across 1 files (1 frameworks)
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
| 0 | Success with routes found |
| 1 | Error, strict mode with diagnostics, or `--fail-on-empty` with no routes |
| 2 | Partial results (warning-level diagnostics present) |
| 3 | No routes found |

## API Example

```go
package main

import (
	"context"
	"fmt"

	"github.com/emm5317/routemap"
)

func main() {
	res, err := routemap.ExtractRoutes(context.Background(), routemap.Config{
		ModuleDir:         ".",
		PackagePattern:    "./...",
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
| Conditional registration | `if flag { r.GET("/admin", h) }` | `exact` (marked `conditional`) |
| Constant path strings | `const P = "/health"; app.Get(P, h)` | `exact` |
| String concatenation | `app.Get("/api" + "/users", h)` | `exact` |
| `net/http` pattern methods | `mux.HandleFunc("GET /users/{id}", h)` | `exact` |
| Helper function following | `setupRoutes(r)` followed into callee | `exact` |
| Cross-file router passing | Router passed to function in another file | `inferred` |
| Versioned module imports | `github.com/gofiber/fiber/v3` | -- |

## Confidence Levels

Each extracted route includes a `confidence` field:

- **`exact`**: Fully tracked from constructor to route call within the same function scope.
- **`inferred`**: Router identity resolved via struct-field propagation or cross-file tracking. The `inferred_by` field explains how (e.g., `"struct-field"`, `"cross-file"`).

Routes inside conditional branches (`if`, `switch`) are additionally marked with `"conditional": true`.

## Diagnostics

Routemap emits structured diagnostics with machine-readable codes:

| Code | Severity | Meaning |
|------|----------|---------|
| `no-routes-found` | info | Framework imported with route-like calls but no routes extracted |
| `duplicate-route` | warning | Same method+path registered at multiple locations |
| `unparseable-route` | warning | Route call detected but could not be parsed |
| `package-load-error` | info | Error loading a Go package |

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

Framework support is adapter-driven internally, making it straightforward to add new frameworks.

## Limitations

- **Static analysis only**: Cannot detect routes created via reflection or dynamic code.
- **String arguments**: Path arguments must be string literals, file-level constants, or simple concatenation expressions (not variables or function return values).
- **Helper function depth**: Intra-file and cross-file function following is limited to 3 levels of nesting.
- **Same-package scope**: Cross-file tracking works within a single Go package. Cross-package router passing is not followed.

## Update Control

The repo is configured so contributor PRs must be approved by `@emm5317`.

- `CODEOWNERS` assigns ownership to `@emm5317`.
- `pr-approval-gate` requires `emm5317` approval on contributor PRs.
- Enable branch protection on `main` in GitHub Settings:
  - Require pull request before merge.
  - Require Code Owner reviews.
  - Require status checks: `ci` and `pr-approval-gate / approval`.
  - Restrict push access to `emm5317`.

## License

MIT. See [LICENSE](LICENSE).
