package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/emm5317/routemap"
	"github.com/spf13/cobra"
)

var (
	scanMiddleware  bool
	scanMethod      string
	scanPathPrefix  string
	scanStrict      bool
	scanJSON        bool
	scanFailOnEmpty bool
	scanSortBy      string
)

func addScanFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&scanMiddleware, "middleware", true, "include resolved middleware chain")
	cmd.Flags().StringVar(&scanMethod, "method", "", "filter routes by HTTP method")
	cmd.Flags().StringVar(&scanPathPrefix, "path-prefix", "", "filter routes by path prefix")
	cmd.Flags().BoolVar(&scanStrict, "strict", false, "fail on parse diagnostics")
	cmd.Flags().BoolVar(&scanJSON, "json", false, "output JSON (shorthand for --format json)")
	cmd.Flags().BoolVar(&scanFailOnEmpty, "fail-on-empty", false, "exit 1 when no routes found")
	cmd.Flags().StringVar(&scanSortBy, "sort-by", "file", "sort routes by: file, method, path, framework")
}

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Extract and display HTTP routes",
	Long:  "Scan the module for HTTP route registrations and display them.",
	Run: func(cmd *cobra.Command, args []string) {
		runScan(cmd)
	},
}

func init() {
	addScanFlags(scanCmd)
}

func runScan(cmd *cobra.Command) {
	cfg, format := buildScanConfig()

	res, err := routemap.ExtractRoutes(context.Background(), cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if scanStrict && len(res.Diagnostics) > 0 {
		printOutput(res, format, 0, nil)
		fmt.Fprintf(os.Stderr, "\nStrict mode: %d diagnostic(s) found. Resolve them or remove --strict.\n", len(res.Diagnostics))
		os.Exit(1)
	}

	totalBefore := len(res.Routes)
	filtered, activeFilters := applyRouteFilters(res.Routes)
	res.Routes = filtered
	sortRoutes(res.Routes, scanSortBy)
	printOutput(res, format, totalBefore, activeFilters)
	handleScanExit(len(res.Routes), res.Partial)
}

func buildScanConfig() (routemap.Config, string) {
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
	format := globalFormat
	if scanJSON {
		format = "json"
	}
	return cfg, format
}

func applyRouteFilters(routes []routemap.Route) ([]routemap.Route, []string) {
	if scanMethod == "" && scanPathPrefix == "" {
		return routes, nil
	}
	var activeFilters []string
	filtered := routes[:0]
	for _, r := range routes {
		if scanMethod != "" && !strings.EqualFold(r.Method, scanMethod) {
			continue
		}
		if scanPathPrefix != "" && !strings.HasPrefix(r.Path, scanPathPrefix) {
			continue
		}
		filtered = append(filtered, r)
	}
	if scanMethod != "" {
		activeFilters = append(activeFilters, "method="+scanMethod)
	}
	if scanPathPrefix != "" {
		activeFilters = append(activeFilters, "path-prefix="+scanPathPrefix)
	}
	return filtered, activeFilters
}

func handleScanExit(routeCount int, partial bool) {
	if routeCount == 0 {
		if scanFailOnEmpty {
			fmt.Fprintln(os.Stderr, "\nNo routes found. Failing because --fail-on-empty is set.")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "\nNo routes found. Check that the module directory and framework filters are correct.")
		os.Exit(3)
	}
	if partial {
		fmt.Fprintln(os.Stderr, "\nCompleted with warnings. Some routes may be incomplete — review diagnostics above.")
		os.Exit(2)
	}
}

func sortRoutes(routes []routemap.Route, sortBy string) {
	sort.SliceStable(routes, func(i, j int) bool {
		a, b := routes[i], routes[j]
		switch sortBy {
		case "method":
			if a.Method != b.Method {
				return a.Method < b.Method
			}
			return a.Path < b.Path
		case "path":
			if a.Path != b.Path {
				return a.Path < b.Path
			}
			return a.Method < b.Method
		case "framework":
			if a.Framework != b.Framework {
				return a.Framework < b.Framework
			}
			return a.Path < b.Path
		default: // "file"
			if a.File != b.File {
				return a.File < b.File
			}
			return a.Line < b.Line
		}
	})
}
