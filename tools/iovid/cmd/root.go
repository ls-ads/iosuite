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
)

var rootCmd = &cobra.Command{
	Use:   "iovid",
	Short: "iovid is a CLI tool for video transformations",
	Long:  `iovid provides atomic verbs for video geometric and visual transformations using FFmpeg.`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&input, "input", "i", "", "input file")
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", "", "output file")
	rootCmd.PersistentFlags().StringVarP(&provider, "provider", "p", "local_gpu", "Execution provider (local_cpu, local_gpu, runpod)")
	rootCmd.PersistentFlags().StringVarP(&apiKey, "api-key", "k", "", "API key for remote provider")
	rootCmd.PersistentFlags().StringVarP(&model, "model", "m", "ffmpeg", "Model name")
}
