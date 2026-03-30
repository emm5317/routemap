package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

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
	baseline := loadBaseline(baselineFile)
	cfg, _ := buildScanConfig()
	current, err := routemap.ExtractRoutes(context.Background(), cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	added, removed, changed := computeDiff(baseline, current)
	hasChanges := len(added)+len(removed)+len(changed) > 0
	printDiffOutput(baselineFile, current, added, removed, changed, hasChanges)

	if hasChanges {
		os.Exit(1)
	}
}

func loadBaseline(path string) routemap.RouteMap {
	f, err := os.Open(path)
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
	return baseline
}

func computeDiff(baseline, current routemap.RouteMap) (added, removed, changed []string) {
	baselineMap := make(map[routeKey]routemap.Route)
	for _, r := range baseline.Routes {
		baselineMap[makeRouteKey(r)] = r
	}
	currentMap := make(map[routeKey]routemap.Route)
	for _, r := range current.Routes {
		currentMap[makeRouteKey(r)] = r
	}

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
	return
}

func printDiffOutput(baselineFile string, current routemap.RouteMap, added, removed, changed []string, hasChanges bool) {
	switch globalFormat {
	case "json":
		type diffOutput struct {
			Added   []string `json:"added"`
			Removed []string `json:"removed"`
			Changed []string `json:"changed"`
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(diffOutput{Added: added, Removed: removed, Changed: changed})
	default:
		if !hasChanges {
			fmt.Printf("No route changes detected between baseline (%s) and current module (%s).\n", baselineFile, globalModuleDir)
			fmt.Printf("All %d routes match the baseline.\n", len(current.Routes))
			return
		}
		fmt.Printf("Route changes detected between baseline (%s) and current module (%s):\n\n", baselineFile, globalModuleDir)
		printDiffSection("Added", "new routes", added)
		printDiffSection("Removed", "routes no longer present", removed)
		printDiffSection("Changed", "routes modified", changed)
		fmt.Printf("Summary: %d added, %d removed, %d changed. Review the changes above before merging.\n",
			len(added), len(removed), len(changed))
	}
}

func printDiffSection(title, desc string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Printf("%s (%d %s):\n", title, len(items), desc)
	for _, s := range items {
		fmt.Println(s)
	}
	fmt.Println()
}
