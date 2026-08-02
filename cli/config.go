package cli

import (
	"errors"
	"fmt"

	configpkg "github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/auth"
	"github.com/spf13/cobra"
)

var errConfigLintFailed = errors.New("config lint failed")

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Inspect and validate local configuration",
}

var configLintCmd = &cobra.Command{
	Use:           "lint",
	Short:         "Validate config syntax, supported keys, and scalar formats",
	RunE:          runConfigLint,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	configCmd.AddCommand(configLintCmd)
	rootCmd.AddCommand(configCmd)
}

type configLintResult struct {
	OK          bool                       `json:"ok"`
	Config      string                     `json:"config"`
	Diagnostics []configpkg.LintDiagnostic `json:"diagnostics"`
}

func runConfigLint(_ *cobra.Command, _ []string) error {
	diagnostics, err := configpkg.Lint(configFile, configpkg.WithKnownProviders(auth.ListKnownProviders()))
	if err != nil {
		return err
	}

	result := configLintResult{
		OK:          len(diagnostics) == 0,
		Config:      configFile,
		Diagnostics: diagnostics,
	}
	if jsonOutput {
		if err := printJSON(result); err != nil {
			return err
		}
		if !result.OK {
			return errConfigLintFailed
		}
		return nil
	}

	if result.OK {
		fmt.Printf("Config OK: %s\n", configFile)
		return nil
	}

	fmt.Printf("Config invalid: %s\n", configFile)
	for _, d := range diagnostics {
		fmt.Printf("  - %s\n", formatLintDiagnostic(configFile, d))
	}
	return errConfigLintFailed
}

func formatLintDiagnostic(file string, d configpkg.LintDiagnostic) string {
	if d.Line > 0 {
		return fmt.Sprintf("%s:%d:%d %s: %s", file, d.Line, d.Column, d.Path, d.Message)
	}
	return fmt.Sprintf("%s %s: %s", file, d.Path, d.Message)
}
