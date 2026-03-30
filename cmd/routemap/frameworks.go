package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/emm5317/routemap/internal/frameworks"
	"github.com/spf13/cobra"
)

var frameworksCmd = &cobra.Command{
	Use:   "frameworks",
	Short: "List supported web frameworks",
	Long:  "Display all supported web frameworks and their router constructor signatures.",
	Run: func(cmd *cobra.Command, args []string) {
		runFrameworks()
	},
}

type frameworkInfo struct {
	Name        string `json:"name"`
	Constructor string `json:"constructor"`
	ImportPath  string `json:"import_path"`
}

var constructorInfo = map[string]frameworkInfo{
	"chi": {
		Name:        "chi",
		Constructor: "chi.NewRouter()",
		ImportPath:  "github.com/go-chi/chi/v5",
	},
	"gin": {
		Name:        "gin",
		Constructor: "gin.New() / gin.Default()",
		ImportPath:  "github.com/gin-gonic/gin",
	},
	"echo": {
		Name:        "echo",
		Constructor: "echo.New()",
		ImportPath:  "github.com/labstack/echo/v4",
	},
	"fiber": {
		Name:        "fiber",
		Constructor: "fiber.New()",
		ImportPath:  "github.com/gofiber/fiber/v2",
	},
	"nethttp": {
		Name:        "nethttp",
		Constructor: "http.NewServeMux() / http.Handle()",
		ImportPath:  "net/http",
	},
}

func runFrameworks() {
	adapters := frameworks.AllAdapters()

	switch globalFormat {
	case "json":
		var infos []frameworkInfo
		for _, a := range adapters {
			if info, ok := constructorInfo[a.Name()]; ok {
				infos = append(infos, info)
			} else {
				infos = append(infos, frameworkInfo{Name: a.Name()})
			}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(infos); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	case "table":
		fmt.Println("| Framework | Constructor | Import Path |")
		fmt.Println("|-----------|-------------|-------------|")
		for _, a := range adapters {
			if info, ok := constructorInfo[a.Name()]; ok {
				fmt.Printf("| %s | %s | %s |\n", info.Name, info.Constructor, info.ImportPath)
			} else {
				fmt.Printf("| %s | - | - |\n", a.Name())
			}
		}
	default:
		fmt.Printf("Supported frameworks (%d):\n\n", len(adapters))
		for _, a := range adapters {
			if info, ok := constructorInfo[a.Name()]; ok {
				fmt.Printf("  %-10s  constructor: %-30s  import: %s\n",
					info.Name, info.Constructor, info.ImportPath)
			} else {
				fmt.Printf("  %-10s\n", a.Name())
			}
		}
	}
}
