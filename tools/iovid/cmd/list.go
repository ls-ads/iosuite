package cmd

import (
	"os"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func init() {
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all commands and their supported providers",
		Run: func(cmd *cobra.Command, args []string) {
			table := tablewriter.NewTable(os.Stdout,
				tablewriter.WithHeader([]string{"Command", "Local CPU", "Local GPU", "RunPod"}),
			)

			table.Append("scale", "Yes", "Yes", "Yes")
			table.Append("crop", "Yes", "Yes", "Yes")
			table.Append("rotate", "Yes", "Yes", "Yes")
			table.Append("flip", "Yes", "Yes", "Yes")
			table.Append("pad", "Yes", "Yes", "Yes")
			table.Append("brighten", "Yes", "Yes", "Yes")
			table.Append("contrast", "Yes", "Yes", "Yes")
			table.Append("saturate", "Yes", "Yes", "Yes")
			table.Append("denoise", "Yes", "Yes", "Yes")
			table.Append("sharpen", "Yes", "Yes", "Yes")
			table.Append("trim", "Yes", "Yes", "Yes")
			table.Append("fps", "Yes", "Yes", "Yes")
			table.Append("mute", "Yes", "Yes", "Yes")
			table.Append("speed", "Yes", "Yes", "Yes")
			table.Append("extract-frames", "Yes", "Yes", "Yes")
			table.Append("extract-audio", "Yes", "Yes", "Yes")
			table.Append("stack", "Yes", "Yes", "No")
			table.Append("pipeline", "Yes", "Yes", "Yes")

			table.Render()
		},
	}

	rootCmd.AddCommand(listCmd)
}
