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
	var format string
	var frameworks string

	flag.StringVar(&cfg.ModuleDir, "module-dir", ".", "module directory")
	flag.StringVar(&frameworks, "frameworks", "", "comma-separated frameworks: nethttp,chi,gin,echo,fiber")
	flag.BoolVar(&cfg.IncludeMiddleware, "middleware", true, "include resolved middleware chain")
	flag.BoolVar(&cfg.Strict, "strict", false, "fail on parse diagnostics")
	flag.BoolVar(&jsonOut, "json", false, "output JSON (shorthand for -format json)")
	flag.StringVar(&format, "format", "text", "output format: text, json, table")
	flag.Parse()

	if jsonOut {
		format = "json"
	}

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

	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	case "table":
		printTable(res)
	default:
		printText(res)
	}

	if res.Partial {
		return 2
	}
	return 0
}

func printText(res routemap.RouteMap) {
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
	printDiagnostics(res)
}

func printTable(res routemap.RouteMap) {
	fmt.Println("| Method | Path | Handler | Framework | File | Line | Confidence |")
	fmt.Println("|--------|------|---------|-----------|------|------|------------|")
	for _, r := range res.Routes {
		fmt.Printf("| %s | %s | %s | %s | %s | %d | %s |\n",
			r.Method, r.Path, r.Handler, r.Framework, r.File, r.Line, r.Confidence)
	}
	printDiagnostics(res)
}

func printDiagnostics(res routemap.RouteMap) {
	if len(res.Diagnostics) > 0 {
		fmt.Println()
		fmt.Println("Diagnostics:")
		for _, d := range res.Diagnostics {
			fmt.Printf("- [%s]", d.Severity)
			if d.Code != "" {
				fmt.Printf(" %s:", d.Code)
			}
			fmt.Printf(" %s", d.Message)
			if d.File != "" {
				fmt.Printf(" (%s:%d)", d.File, d.Line)
			}
			fmt.Println()
		}
	}
}
