package cmd

import (
	"fmt"

	"github.com/inchestnov/marko/cli/sync"
	"github.com/spf13/cobra"
)

var exportOut string

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Render the desired state and write it to a static JSON file",
	RunE: func(cmd *cobra.Command, args []string) error {
		pr, err := runPipeline()
		if err != nil {
			printFindingsToStderr(cmd.ErrOrStderr(), pr)
			return err
		}

		n, err := sync.WriteExport(exportOut, pr.Tree)
		if err != nil {
			return newExitError(3, err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s (%d nodes)\n", exportOut, n)
		return nil
	},
}

func init() {
	exportCmd.Flags().StringVar(&exportOut, "out", "marko-export.json", "output file path")
	rootCmd.AddCommand(exportCmd)
}
