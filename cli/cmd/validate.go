package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/inchestnov/marko/cli/validator"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Run structural and semantic validation on marko.yaml and templates/",
	RunE: func(cmd *cobra.Command, args []string) error {
		pr, err := loadConfig()
		if err != nil {
			return err
		}

		findings, err := validateConfig(pr.Config, pr.DuplicateTemplates)
		if err != nil {
			return err
		}

		if jsonOutput {
			data, err := json.MarshalIndent(findings, "", "  ")
			if err != nil {
				return newExitError(1, err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
		} else {
			errOut := cmd.ErrOrStderr()
			for _, f := range findings {
				line := f.String()
				if f.Severity == validator.SeverityWarning {
					fmt.Fprintln(errOut, "warning: "+line)
				} else {
					fmt.Fprintln(errOut, line)
				}
			}
			if !validator.HasErrors(findings) {
				errCount := countErrors(findings)
				warnCount := countWarnings(findings)
				fmt.Fprintf(cmd.OutOrStdout(), "%s is valid (%s, %s)\n",
					filepath.Base(pr.Config.SourcePath), pluralize(errCount, "error"), pluralize(warnCount, "warning"))
			}
		}

		if validator.HasErrors(findings) {
			return newExitError(1, fmt.Errorf("validation failed with %d error(s)", countErrors(findings)))
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
}
