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
	volume    bool
	overwrite bool

	// Shared RunPod flags
	activeWorkers bool
	gpuType       string
	dataCenterIds []string
	volumeSize    int
	keepFailed    bool
)

var rootCmd = &cobra.Command{
	Use:           "ioimg",
	Short:         "iosuite image processing tool",
	SilenceErrors: true,
}

func Execute() error {
	return rootCmd.Execute()
}

func resolveDefaults() {
	if provider == "" {
		provider = "local_gpu"
	}
	if model == "" {
		model = "ffmpeg"
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&input, "input", "i", "", "Input path")
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", "", "Output path")
	rootCmd.PersistentFlags().StringVarP(&provider, "provider", "p", "", "Execution provider (local_cpu, local_gpu, runpod)")
	rootCmd.PersistentFlags().StringVarP(&apiKey, "api-key", "k", "", "API key for remote provider")
	rootCmd.PersistentFlags().StringVarP(&model, "model", "m", "", "Model name (for upscale/ffmpeg)")
	rootCmd.PersistentFlags().BoolVar(&overwrite, "overwrite", false, "Reprocess all files even if output already exists")
	rootCmd.PersistentFlags().BoolVar(&volume, "volume", false, "Use RunPod network volume for processing")
}
