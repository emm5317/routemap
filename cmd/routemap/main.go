package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/emm5317/routemap/pkg/routemap"
)

func main() {
	os.Exit(run())
}

func run() int {
	var cfg routemap.Config
	var jsonOut bool
	var frameworks string

	flag.StringVar(&cfg.ModuleDir, "module-dir", ".", "module directory")
	flag.StringVar(&cfg.PackagePattern, "package", "./...", "package pattern (reserved for future loaders)")
	flag.StringVar(&frameworks, "frameworks", "", "comma-separated frameworks: nethttp,chi,gin,echo,fiber")
	flag.BoolVar(&cfg.IncludeMiddleware, "middleware", true, "include resolved middleware chain")
	flag.BoolVar(&cfg.Strict, "strict", false, "fail on parse diagnostics")
	flag.BoolVar(&jsonOut, "json", false, "output JSON")
	flag.Parse()

	if frameworks != "" {
		for _, f := range strings.Split(frameworks, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				cfg.Frameworks = append(cfg.Frameworks, f)
			}
		}
	}

	res, err := routemap.ExtractRoutes(context.Background(), cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if cfg.Strict && len(res.Diagnostics) > 0 {
		fmt.Fprintln(os.Stderr, "strict mode: diagnostics present")
		return 1
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	} else {
		for _, r := range res.Routes {
			fmt.Printf("[%s] %-7s %-25s %s (%s:%d)\n", r.Framework, r.Method, r.Path, r.Handler, r.File, r.Line)
			if len(r.Middleware) > 0 {
				fmt.Print("  middleware:")
				for _, m := range r.Middleware {
					fmt.Printf(" %s", m.Name)
				}
				fmt.Println()
			}
		}
		if len(res.Diagnostics) > 0 {
			fmt.Println("Diagnostics:")
			for _, d := range res.Diagnostics {
				fmt.Printf("- [%s] %s", d.Severity, d.Message)
				if d.File != "" {
					fmt.Printf(" (%s:%d)", d.File, d.Line)
				}
				fmt.Println()
			}
		}
	}

	if res.Partial {
		return 2
	}
	return 0
}
