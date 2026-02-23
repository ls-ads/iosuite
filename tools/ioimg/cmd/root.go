package cmd

import (
	"github.com/spf13/cobra"
)

var (
	input  string
	output string
)

var rootCmd = &cobra.Command{
	Use:           "ioimg",
	Short:         "iosuite image processing tool",
	SilenceErrors: true,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&input, "input", "i", "", "Input image path")
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", "", "Output image path")
}
