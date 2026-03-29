# routemap

[![CI](https://github.com/Emm5317/routemap/actions/workflows/ci.yml/badge.svg)](https://github.com/Emm5317/routemap/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/Emm5317/routemap.svg)](https://pkg.go.dev/github.com/Emm5317/routemap)
[![Go Report Card](https://goreportcard.com/badge/github.com/Emm5317/routemap)](https://goreportcard.com/report/github.com/Emm5317/routemap)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

Library-first static HTTP route extraction for `net/http`, `chi`, `gin`, `echo`, and `fiber`.

## Install

```bash
go install github.com/Emm5317/routemap/cmd/routemap@latest
```

## CLI Examples

```bash
routemap \
  -module-dir ./testdata/fixtures/gincase \
  -frameworks gin
```

```bash
routemap \
  -module-dir ./testdata/fixtures/nethttpcase \
  -frameworks nethttp \
  -json
```

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
		fmt.Printf("%s %s -> %s\n", r.Method, r.Path, r.Handler)
	}
}
```

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
