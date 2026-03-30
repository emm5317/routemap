package main

import "github.com/spf13/cobra"

var version = "1.0.0"

// Global flag variables
var (
	globalModuleDir  string
	globalPackage    string
	globalFrameworks string
	globalFormat     string
	globalQuiet      bool
)

var rootCmd = &cobra.Command{
	Use:   "routemap",
	Short: "Static HTTP route extraction for Go web applications",
	Long: `Routemap uses go/packages to load Go modules with full build-tag awareness,
then performs AST-based static analysis to discover HTTP routes across
net/http, chi, gin, echo, and fiber without executing code.`,
	Version: version,
	Run: func(cmd *cobra.Command, args []string) {
		runScan(cmd)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&globalModuleDir, "module-dir", "d", ".", "module directory")
	rootCmd.PersistentFlags().StringVarP(&globalPackage, "package", "p", "./...", "package pattern")
	rootCmd.PersistentFlags().StringVarP(&globalFrameworks, "frameworks", "f", "", "comma-separated frameworks: nethttp,chi,gin,echo,fiber")
	rootCmd.PersistentFlags().StringVarP(&globalFormat, "format", "o", "text", "output format: text, json, table")
	rootCmd.PersistentFlags().BoolVarP(&globalQuiet, "quiet", "q", false, "suppress diagnostics output")

	// Register scan flags on root for backward compatibility
	addScanFlags(rootCmd)

	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(frameworksCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(diffCmd)
}
