package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/emm5317/routemap"
)

func printOutput(res routemap.RouteMap, format string, totalBefore int, filters []string) {
	switch format {
	case "json":
		printJSON(res)
	case "table":
		printTable(res)
		printSummary(res, totalBefore, filters)
	default:
		printText(res)
		printSummary(res, totalBefore, filters)
	}
	printDiagnostics(res)
}

func printText(res routemap.RouteMap) {
	for _, r := range res.Routes {
		confidence := ""
		switch r.Confidence {
		case routemap.ConfidenceExact:
			confidence = " [exact]"
		case routemap.ConfidenceHigh:
			confidence = " [high]"
		case routemap.ConfidenceInferred:
			suffix := "inferred"
			if r.InferredBy != "" {
				suffix = "inferred:" + r.InferredBy
			}
			confidence = " [" + suffix + "]"
		}
		fmt.Printf("[%s] %-7s %-25s %s (%s:%d)%s\n",
			r.Framework, r.Method, r.Path, r.Handler, r.File, r.Line, confidence)
		if len(r.Middleware) > 0 {
			fmt.Print("  middleware:")
			for _, m := range r.Middleware {
				fmt.Printf(" %s", m.Name)
			}
			fmt.Println()
		}
	}
}

func printTable(res routemap.RouteMap) {
	fmt.Println("| Method | Path | Handler | Framework | File | Line | Confidence |")
	fmt.Println("|--------|------|---------|-----------|------|------|------------|")
	for _, r := range res.Routes {
		fmt.Printf("| %s | %s | %s | %s | %s | %d | %s |\n",
			r.Method, r.Path, r.Handler, r.Framework, r.File, r.Line, r.Confidence)
	}
}

func printJSON(res routemap.RouteMap) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func printSummary(res routemap.RouteMap, totalBefore int, filters []string) {
	if len(res.Routes) == 0 && totalBefore == 0 {
		fmt.Println("\nNo routes detected. Ensure the module contains supported framework route registrations.")
		return
	}
	if len(res.Routes) == 0 && totalBefore > 0 {
		fmt.Printf("\nAll %d routes were excluded by filters. Try broadening --method or --path-prefix.\n", totalBefore)
		return
	}
	files := map[string]bool{}
	fws := map[string]bool{}
	for _, r := range res.Routes {
		files[r.File] = true
		fws[r.Framework] = true
	}
	fmt.Printf("\nFound %d routes across %d files (%d frameworks)\n",
		len(res.Routes), len(files), len(fws))
	if len(filters) > 0 && totalBefore != len(res.Routes) {
		fmt.Printf("Filters applied [%s]: showing %d of %d routes\n",
			strings.Join(filters, ", "), len(res.Routes), totalBefore)
	}
}

func printDiagnostics(res routemap.RouteMap) {
	if globalQuiet || len(res.Diagnostics) == 0 {
		return
	}
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
