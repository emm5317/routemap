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

var diffCmd = &cobra.Command{
	Use:   "diff <baseline.json>",
	Short: "Diff routes against a baseline JSON file",
	Long: `Compare current route extraction against a previously saved baseline.
Reads the baseline JSON file, runs a fresh extraction, and reports added/removed/changed routes.
Exit 0 = no changes, 1 = changes detected.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runDiff(args[0])
	},
}

type routeKey struct {
	Method    string
	Path      string
	Framework string
}

func makeRouteKey(r routemap.Route) routeKey {
	return routeKey{Method: r.Method, Path: r.Path, Framework: r.Framework}
}

func runDiff(baselineFile string) {
	// Read baseline JSON
	f, err := os.Open(baselineFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening baseline file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	var baseline routemap.RouteMap
	if err := json.NewDecoder(f).Decode(&baseline); err != nil {
		fmt.Fprintf(os.Stderr, "error decoding baseline JSON: %v\n", err)
		os.Exit(1)
	}

	// Run fresh extraction
	cfg := routemap.Config{
		ModuleDir:         globalModuleDir,
		PackagePattern:    globalPackage,
		IncludeMiddleware: scanMiddleware,
	}

	if globalFrameworks != "" {
		for _, fw := range strings.Split(globalFrameworks, ",") {
			fw = strings.TrimSpace(fw)
			if fw != "" {
				cfg.Frameworks = append(cfg.Frameworks, fw)
			}
		}
	}

	current, err := routemap.ExtractRoutes(context.Background(), cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Build lookup maps
	baselineMap := make(map[routeKey]routemap.Route)
	for _, r := range baseline.Routes {
		baselineMap[makeRouteKey(r)] = r
	}
	currentMap := make(map[routeKey]routemap.Route)
	for _, r := range current.Routes {
		currentMap[makeRouteKey(r)] = r
	}

	// Compare
	var added, removed, changed []string

	for key, cur := range currentMap {
		if base, ok := baselineMap[key]; !ok {
			added = append(added, fmt.Sprintf("  + [%s] %s %s → %s", key.Framework, key.Method, key.Path, cur.Handler))
		} else if base.Handler != cur.Handler {
			changed = append(changed, fmt.Sprintf("  ~ [%s] %s %s: %s → %s", key.Framework, key.Method, key.Path, base.Handler, cur.Handler))
		}
	}

	for key, base := range baselineMap {
		if _, ok := currentMap[key]; !ok {
			removed = append(removed, fmt.Sprintf("  - [%s] %s %s (was: %s)", key.Framework, key.Method, key.Path, base.Handler))
		}
	}

	hasChanges := len(added)+len(removed)+len(changed) > 0

	switch globalFormat {
	case "json":
		type diffOutput struct {
			Added   []string `json:"added"`
			Removed []string `json:"removed"`
			Changed []string `json:"changed"`
		}
		out := diffOutput{Added: added, Removed: removed, Changed: changed}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	default:
		if !hasChanges {
			fmt.Printf("No route changes detected between baseline (%s) and current module (%s).\n", baselineFile, globalModuleDir)
			fmt.Printf("All %d routes match the baseline.\n", len(current.Routes))
		} else {
			fmt.Printf("Route changes detected between baseline (%s) and current module (%s):\n\n", baselineFile, globalModuleDir)
			if len(added) > 0 {
				fmt.Printf("Added (%d new routes):\n", len(added))
				for _, s := range added {
					fmt.Println(s)
				}
				fmt.Println()
			}
			if len(removed) > 0 {
				fmt.Printf("Removed (%d routes no longer present):\n", len(removed))
				for _, s := range removed {
					fmt.Println(s)
				}
				fmt.Println()
			}
			if len(changed) > 0 {
				fmt.Printf("Changed (%d routes modified):\n", len(changed))
				for _, s := range changed {
					fmt.Println(s)
				}
				fmt.Println()
			}
			fmt.Printf("Summary: %d added, %d removed, %d changed. Review the changes above before merging.\n",
				len(added), len(removed), len(changed))
		}
	}

	if hasChanges {
		os.Exit(1)
	}
}
