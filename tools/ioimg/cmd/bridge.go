package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"iosuite.io/libs/iocore"
)

var (
	combineFPS int
)

func init() {
	// Combine
	combineCmd := &cobra.Command{
		Use:   "combine",
		Short: "Combine images into a video",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !iocore.IsImage(input) {
				return fmt.Errorf("input must be an image pattern (e.g. frame_%%05d.png): %s", input)
			}
			if !iocore.IsVideo(output) {
				return fmt.Errorf("output must be a video file (.mp4, .mkv, etc.): %s", output)
			}
			ctx := context.Background()
			cfg := &iocore.FFmpegConfig{Provider: iocore.UpscaleProvider(provider), APIKey: apiKey, Model: model}
			return iocore.Combine(ctx, cfg, input, output, combineFPS)
		},
	}
	combineCmd.Flags().IntVar(&combineFPS, "fps", 30, "frames per second")
	rootCmd.AddCommand(combineCmd)
}
