package cmd

import (
	"github.com/spf13/cobra"
)

var (
	input     string
	output    string
	provider  string
	apiKey    string
	model     string
	overwrite bool
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
	rootCmd.PersistentFlags().StringVarP(&input, "input", "i", "", "Input path")
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", "", "Output path")
	rootCmd.PersistentFlags().StringVarP(&provider, "provider", "p", "local_gpu", "Execution provider (local_cpu, local_gpu, runpod)")
	rootCmd.PersistentFlags().StringVarP(&apiKey, "api-key", "k", "", "API key for remote provider")
	rootCmd.PersistentFlags().StringVarP(&model, "model", "m", "ffmpeg", "Model name (for upscale/ffmpeg)")
	rootCmd.PersistentFlags().BoolVar(&overwrite, "overwrite", false, "Reprocess all files even if output already exists")
}
