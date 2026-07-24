package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/inchestnov/marko/internal/bookmarktree"
	"github.com/inchestnov/marko/validator"
	"github.com/spf13/cobra"
)

var renderOut string

var renderCmd = &cobra.Command{
	Use:   "render",
	Short: "Run the full pipeline and print the resulting BookmarkTree",
	RunE: func(cmd *cobra.Command, args []string) error {
		pr, err := runPipeline()
		if err != nil {
			printFindingsToStderr(cmd.ErrOrStderr(), pr)
			return err
		}

		var out io.Writer = cmd.OutOrStdout()
		var f *os.File
		if renderOut != "" {
			file, err := os.Create(renderOut)
			if err != nil {
				return newExitError(3, fmt.Errorf("creating %q: %w", renderOut, err))
			}
			defer file.Close()
			f = file
			out = file
		}

		printTreeView(out, pr.Tree)

		if f != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s\n", renderOut)
		}

		return nil
	},
}

func init() {
	renderCmd.Flags().StringVar(&renderOut, "out", "", "write output to a file instead of stdout")
	rootCmd.AddCommand(renderCmd)
}

func printFindingsToStderr(w io.Writer, pr *pipelineResult) {
	if pr == nil {
		return
	}
	for _, f := range pr.Findings {
		line := f.String()
		if f.Severity == validator.SeverityWarning {
			fmt.Fprintln(w, "warning: "+line)
		} else {
			fmt.Fprintln(w, line)
		}
	}
}

func rootDisplayName(name string) string {
	switch name {
	case "bar":
		return "Bookmarks Bar"
	case "other":
		return "Other Bookmarks"
	default:
		return name
	}
}

// printTreeView renders tree in the box-drawing indented format shown in
// docs/architecture.md §9.3.
func printTreeView(w io.Writer, tree *bookmarktree.BookmarkTree) {
	for _, root := range tree.Roots {
		fmt.Fprintln(w, rootDisplayName(root.Name))
		printChildren(w, root.Children, "")
	}
}

func printChildren(w io.Writer, children []*bookmarktree.Node, prefix string) {
	for i, c := range children {
		last := i == len(children)-1
		connector := "├── "
		childPrefix := prefix + "│   "
		if last {
			connector = "└── "
			childPrefix = prefix + "    "
		}
		fmt.Fprintln(w, prefix+connector+c.Name)
		if len(c.Children) > 0 {
			printChildren(w, c.Children, childPrefix)
		}
	}
}
