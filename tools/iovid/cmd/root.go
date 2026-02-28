package cmd

import (
	"github.com/spf13/cobra"
)

var (
	input    string
	output   string
	provider string
	apiKey   string
	model    string
	volume   string

	// Shared RunPod flags
	activeWorkers bool
	gpuType       string
	dataCenterIds []string
	volumeSize    int
	keepFailed    bool
)

var rootCmd = &cobra.Command{
	Use:   "iovid",
	Short: "iovid is a CLI tool for video transformations",
	Long:  `iovid provides atomic verbs for video geometric and visual transformations using FFmpeg.`,
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
	rootCmd.PersistentFlags().StringVarP(&input, "input", "i", "", "input file")
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", "", "output file")
	rootCmd.PersistentFlags().StringVarP(&provider, "provider", "p", "", "Execution provider (local_cpu, local_gpu, runpod)")
	rootCmd.PersistentFlags().StringVarP(&apiKey, "api-key", "k", "", "API key for remote provider")
	rootCmd.PersistentFlags().StringVarP(&model, "model", "m", "", "Model name")
	rootCmd.PersistentFlags().StringVar(&volume, "volume", "", "RunPod volume ID or size in GB for the volume workflow")
}
