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
				tablewriter.WithHeader([]string{"Command/Model", "Local CPU", "Local GPU", "RunPod", "Replicate"}),
			)

			table.Append("upscale (real-esrgan)", "No", "Yes", "Yes", "Yes")
			table.Append("upscale (ffmpeg)", "Yes", "Yes", "Yes", "No")
			table.Append("scale", "Yes", "Yes", "Yes", "No")
			table.Append("crop", "Yes", "Yes", "Yes", "No")
			table.Append("rotate", "Yes", "Yes", "Yes", "No")
			table.Append("flip", "Yes", "Yes", "Yes", "No")
			table.Append("pad", "Yes", "Yes", "Yes", "No")
			table.Append("brighten", "Yes", "Yes", "Yes", "No")
			table.Append("contrast", "Yes", "Yes", "Yes", "No")
			table.Append("saturate", "Yes", "Yes", "Yes", "No")
			table.Append("denoise", "Yes", "Yes", "Yes", "No")
			table.Append("sharpen", "Yes", "Yes", "Yes", "No")
			table.Append("combine", "Yes", "Yes", "Yes", "No")
			table.Append("pipeline", "Yes", "Yes", "Yes", "No")

			table.Render()
		},
	}

	rootCmd.AddCommand(listCmd)
}
