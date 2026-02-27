package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"iosuite.io/libs/iocore"
)

var (
	input2 string
	fps    int
)

func init() {
	// Extract Frames
	extractFramesCmd := &cobra.Command{
		Use:   "extract-frames",
		Short: "Extract frames from video into images",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !iocore.IsVideo(input) {
				return fmt.Errorf("input must be a video (.mp4, .mkv, .mov, etc.): %s", input)
			}
			ctx := context.Background()
			cfg := &iocore.FFmpegConfig{Provider: iocore.UpscaleProvider(provider), APIKey: apiKey, Model: model}
			return iocore.ExtractFrames(ctx, cfg, input, output)
		},
	}
	rootCmd.AddCommand(extractFramesCmd)

	// Extract Audio
	extractAudioCmd := &cobra.Command{
		Use:   "extract-audio",
		Short: "Extract audio stream from video",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !iocore.IsVideo(input) {
				return fmt.Errorf("input must be a video (.mp4, .mkv, .mov, etc.): %s", input)
			}
			if !iocore.IsAudio(output) {
				return fmt.Errorf("output must be an audio file (.mp3, .wav, .m4a, etc.): %s", output)
			}
			ctx := context.Background()
			cfg := &iocore.FFmpegConfig{Provider: iocore.UpscaleProvider(provider), APIKey: apiKey, Model: model}
			return iocore.ExtractAudio(ctx, cfg, input, output)
		},
	}
	rootCmd.AddCommand(extractAudioCmd)

	// Stack
	stackCmd := &cobra.Command{
		Use:   "stack",
		Short: "Combine two inputs side-by-side",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !iocore.IsVideo(input) || !iocore.IsVideo(input2) {
				return fmt.Errorf("both inputs must be videos: %s, %s", input, input2)
			}
			ctx := context.Background()
			cfg := &iocore.FFmpegConfig{Provider: iocore.UpscaleProvider(provider), APIKey: apiKey, Model: model}
			return iocore.Stack(ctx, cfg, input, input2, output, axis)
		},
	}
	stackCmd.Flags().StringVar(&input2, "i2", "", "second input file")
	stackCmd.Flags().StringVar(&axis, "axis", "h", "axis (h or v)")
	rootCmd.AddCommand(stackCmd)
}
