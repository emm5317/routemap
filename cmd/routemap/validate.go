package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/emm5317/routemap"
	"github.com/spf13/cobra"
)

var (
	validateNoDuplicates  bool
	validateRequireRoutes bool
	validateMinRoutes     int
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "CI validation of route extraction",
	Long:  "Extract routes and validate against configurable rules. Exit 0 = pass, 1 = fail.",
	Run: func(cmd *cobra.Command, args []string) {
		runValidate()
	},
}

func init() {
	validateCmd.Flags().BoolVar(&validateNoDuplicates, "no-duplicates", true, "fail if duplicate Method+Path exist")
	validateCmd.Flags().BoolVar(&validateRequireRoutes, "require-routes", false, "fail if no routes found")
	validateCmd.Flags().IntVar(&validateMinRoutes, "min-routes", 0, "fail if fewer than N routes found")
}

type validateResult struct {
	Pass       bool     `json:"pass"`
	RouteCount int      `json:"route_count"`
	Failures   []string `json:"failures,omitempty"`
}

func runValidate() {
	cfg := routemap.Config{
		ModuleDir:         globalModuleDir,
		PackagePattern:    globalPackage,
		IncludeMiddleware: scanMiddleware,
	}

	if globalFrameworks != "" {
		for _, f := range strings.Split(globalFrameworks, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				cfg.Frameworks = append(cfg.Frameworks, f)
			}
		}
	}

	res, err := routemap.ExtractRoutes(context.Background(), cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	vr := validateResult{
		Pass:       true,
		RouteCount: len(res.Routes),
	}

	// Check require-routes
	if validateRequireRoutes && len(res.Routes) == 0 {
		vr.Failures = append(vr.Failures, "no routes found (--require-routes)")
		vr.Pass = false
	}

	// Check min-routes
	if validateMinRoutes > 0 && len(res.Routes) < validateMinRoutes {
		vr.Failures = append(vr.Failures,
			fmt.Sprintf("found %d routes, minimum required is %d (--min-routes)", len(res.Routes), validateMinRoutes))
		vr.Pass = false
	}

	// Check no-duplicates
	if validateNoDuplicates {
		seen := map[string]int{}
		for _, r := range res.Routes {
			key := r.Method + " " + r.Path
			seen[key]++
		}
		for key, count := range seen {
			if count > 1 {
				vr.Failures = append(vr.Failures,
					fmt.Sprintf("duplicate route: %s (appears %d times)", key, count))
				vr.Pass = false
			}
		}
	}

	switch globalFormat {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(vr); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	default:
		if vr.Pass {
			fmt.Printf("Validation passed: %d routes found, all checks satisfied.\n", vr.RouteCount)
		} else {
			fmt.Printf("Validation failed: %d route(s) found, %d check(s) did not pass:\n\n", vr.RouteCount, len(vr.Failures))
			for _, f := range vr.Failures {
				fmt.Printf("  - %s\n", f)
			}
			fmt.Println("\nFix the issues above or adjust the validation flags to match your expectations.")
		}
	}

	if !vr.Pass {
		os.Exit(1)
	}
}
