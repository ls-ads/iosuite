package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"iosuite.io/libs/iocore"
)

var (
	width       int
	height      int
	cropW       int
	cropH       int
	cropX       int
	cropY       int
	degrees     int
	axis        string
	aspect      string
	level       float64
	preset      string
	amount      float64
	start       string
	end         string
	fpsRate     int
	multiplier  float64
	chunks      int
	chunkLength float64
	vcodec      string
	acodec      string
	vbitrate    string
	abitrate    string
	crf         string
)

func makeFFmpegConfig() *iocore.FFmpegConfig {
	return &iocore.FFmpegConfig{
		Provider:      iocore.UpscaleProvider(provider),
		APIKey:        apiKey,
		Model:         model,
		UseVolume:     volume,
		GPUID:         gpuType,
		DataCenterIDs: dataCenterIds,
		KeepFailed:    keepFailed,
	}
}

func init() {
	// Scale
	scaleCmd := &cobra.Command{
		Use:   "scale",
		Short: "Scale video",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveDefaults()
			if !iocore.IsVideo(input) {
				return fmt.Errorf("input must be a video (.mp4, .mkv, .mov, etc.): %s", input)
			}
			ctx := context.Background()
			cfg := makeFFmpegConfig()
			return iocore.Scale(ctx, cfg, input, output, width, height)
		},
	}
	scaleCmd.Flags().IntVar(&width, "width", 1280, "target width")
	scaleCmd.Flags().IntVar(&height, "height", 720, "target height")
	rootCmd.AddCommand(scaleCmd)

	// Crop
	cropCmd := &cobra.Command{
		Use:   "crop",
		Short: "Crop video",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveDefaults()
			if !iocore.IsVideo(input) {
				return fmt.Errorf("input must be a video (.mp4, .mkv, .mov, etc.): %s", input)
			}
			ctx := context.Background()
			cfg := makeFFmpegConfig()
			return iocore.Crop(ctx, cfg, input, output, cropW, cropH, cropX, cropY)
		},
	}
	cropCmd.Flags().IntVar(&cropW, "w", 0, "crop width")
	cropCmd.Flags().IntVar(&cropH, "h", 0, "crop height")
	cropCmd.Flags().IntVar(&cropX, "x", 0, "crop x")
	cropCmd.Flags().IntVar(&cropY, "y", 0, "crop y")
	rootCmd.AddCommand(cropCmd)

	// Rotate
	rotateCmd := &cobra.Command{
		Use:   "rotate",
		Short: "Rotate video",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveDefaults()
			if !iocore.IsVideo(input) {
				return fmt.Errorf("input must be a video (.mp4, .mkv, .mov, etc.): %s", input)
			}
			ctx := context.Background()
			cfg := makeFFmpegConfig()
			return iocore.Rotate(ctx, cfg, input, output, degrees)
		},
	}
	rotateCmd.Flags().IntVar(&degrees, "degrees", 0, "degrees (90, 180, 270 or arbitrary)")
	rootCmd.AddCommand(rotateCmd)

	// Flip
	flipCmd := &cobra.Command{
		Use:   "flip",
		Short: "Flip video",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveDefaults()
			if !iocore.IsVideo(input) {
				return fmt.Errorf("input must be a video (.mp4, .mkv, .mov, etc.): %s", input)
			}
			ctx := context.Background()
			cfg := makeFFmpegConfig()
			return iocore.Flip(ctx, cfg, input, output, axis)
		},
	}
	flipCmd.Flags().StringVar(&axis, "axis", "h", "axis (h or v)")
	rootCmd.AddCommand(flipCmd)

	// Pad
	padCmd := &cobra.Command{
		Use:   "pad",
		Short: "Pad video to aspect ratio",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveDefaults()
			if !iocore.IsVideo(input) {
				return fmt.Errorf("input must be a video (.mp4, .mkv, .mov, etc.): %s", input)
			}
			ctx := context.Background()
			cfg := makeFFmpegConfig()
			return iocore.Pad(ctx, cfg, input, output, aspect)
		},
	}
	padCmd.Flags().StringVar(&aspect, "aspect", "16:9", "target aspect ratio")
	rootCmd.AddCommand(padCmd)

	// Brighten
	brightenCmd := &cobra.Command{
		Use:   "brighten",
		Short: "Adjust brightness",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveDefaults()
			if !iocore.IsVideo(input) {
				return fmt.Errorf("input must be a video (.mp4, .mkv, .mov, etc.): %s", input)
			}
			ctx := context.Background()
			cfg := makeFFmpegConfig()
			return iocore.Brighten(ctx, cfg, input, output, level)
		},
	}
	brightenCmd.Flags().Float64Var(&level, "level", 0.0, "brightness level (-1.0 to 1.0)")
	rootCmd.AddCommand(brightenCmd)

	// Contrast
	contrastCmd := &cobra.Command{
		Use:   "contrast",
		Short: "Adjust contrast",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveDefaults()
			if !iocore.IsVideo(input) {
				return fmt.Errorf("input must be a video (.mp4, .mkv, .mov, etc.): %s", input)
			}
			ctx := context.Background()
			cfg := makeFFmpegConfig()
			return iocore.Contrast(ctx, cfg, input, output, level)
		},
	}
	contrastCmd.Flags().Float64Var(&level, "level", 0.0, "contrast level (-100 to 100)")
	rootCmd.AddCommand(contrastCmd)

	// Saturate
	saturateCmd := &cobra.Command{
		Use:   "saturate",
		Short: "Adjust saturation",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveDefaults()
			if !iocore.IsVideo(input) {
				return fmt.Errorf("input must be a video (.mp4, .mkv, .mov, etc.): %s", input)
			}
			ctx := context.Background()
			cfg := makeFFmpegConfig()
			return iocore.Saturate(ctx, cfg, input, output, level)
		},
	}
	saturateCmd.Flags().Float64Var(&level, "level", 1.0, "saturation level (0 to 3)")
	rootCmd.AddCommand(saturateCmd)

	// Denoise
	denoiseCmd := &cobra.Command{
		Use:   "denoise",
		Short: "Denoise video",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveDefaults()
			if !iocore.IsVideo(input) {
				return fmt.Errorf("input must be a video (.mp4, .mkv, .mov, etc.): %s", input)
			}
			ctx := context.Background()
			cfg := makeFFmpegConfig()
			return iocore.Denoise(ctx, cfg, input, output, preset)
		},
	}
	denoiseCmd.Flags().StringVar(&preset, "preset", "med", "preset (weak, med, strong)")
	rootCmd.AddCommand(denoiseCmd)

	// Sharpen
	sharpenCmd := &cobra.Command{
		Use:   "sharpen",
		Short: "Sharpen video",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveDefaults()
			if !iocore.IsVideo(input) {
				return fmt.Errorf("input must be a video (.mp4, .mkv, .mov, etc.): %s", input)
			}
			ctx := context.Background()
			cfg := makeFFmpegConfig()
			return iocore.Sharpen(ctx, cfg, input, output, amount)
		},
	}
	sharpenCmd.Flags().Float64Var(&amount, "amount", 1.0, "sharpen amount (0 to 5)")
	rootCmd.AddCommand(sharpenCmd)

	// Trim
	trimCmd := &cobra.Command{
		Use:   "trim",
		Short: "Trim video",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveDefaults()
			if !iocore.IsVideo(input) {
				return fmt.Errorf("input must be a video (.mp4, .mkv, .mov, etc.): %s", input)
			}
			ctx := context.Background()
			cfg := makeFFmpegConfig()
			return iocore.Trim(ctx, cfg, input, output, start, end)
		},
	}
	trimCmd.Flags().StringVar(&start, "start", "00:00:00", "start time")
	trimCmd.Flags().StringVar(&end, "end", "", "end time")
	rootCmd.AddCommand(trimCmd)

	// FPS
	fpsCmd := &cobra.Command{
		Use:   "fps",
		Short: "Change frame rate",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveDefaults()
			if !iocore.IsVideo(input) {
				return fmt.Errorf("input must be a video (.mp4, .mkv, .mov, etc.): %s", input)
			}
			ctx := context.Background()
			cfg := makeFFmpegConfig()
			return iocore.FPS(ctx, cfg, input, output, fpsRate)
		},
	}
	fpsCmd.Flags().IntVar(&fpsRate, "rate", 30, "frames per second")
	rootCmd.AddCommand(fpsCmd)

	// Mute
	muteCmd := &cobra.Command{
		Use:   "mute",
		Short: "Remove audio",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveDefaults()
			if !iocore.IsVideo(input) {
				return fmt.Errorf("input must be a video (.mp4, .mkv, .mov, etc.): %s", input)
			}
			ctx := context.Background()
			cfg := makeFFmpegConfig()
			return iocore.Mute(ctx, cfg, input, output)
		},
	}
	rootCmd.AddCommand(muteCmd)

	// Speed
	speedCmd := &cobra.Command{
		Use:   "speed",
		Short: "Change video speed",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveDefaults()
			if !iocore.IsVideo(input) {
				return fmt.Errorf("input must be a video (.mp4, .mkv, .mov, etc.): %s", input)
			}
			ctx := context.Background()
			cfg := makeFFmpegConfig()
			return iocore.Speed(ctx, cfg, input, output, multiplier)
		},
	}
	speedCmd.Flags().Float64Var(&multiplier, "multiplier", 1.0, "speed multiplier (e.g. 0.5, 2.0)")
	rootCmd.AddCommand(speedCmd)

	// Chunk
	chunkCmd := &cobra.Command{
		Use:   "chunk",
		Short: "Chunk video into multiple segments",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveDefaults()
			if !iocore.IsVideo(input) {
				return fmt.Errorf("input must be a video (.mp4, .mkv, .mov, etc.): %s", input)
			}
			if chunks <= 0 && chunkLength <= 0 {
				return fmt.Errorf("must specify either --chunks or --length")
			}
			if chunks > 0 && chunkLength > 0 {
				return fmt.Errorf("cannot specify both --chunks and --length")
			}
			ctx := context.Background()

			outputPattern := output
			if !strings.Contains(outputPattern, "%") {
				ext := filepath.Ext(outputPattern)
				base := strings.TrimSuffix(outputPattern, ext)
				outputPattern = fmt.Sprintf("%s_%%03d%s", base, ext)
			}

			return iocore.Chunk(ctx, input, outputPattern, chunks, chunkLength)
		},
	}
	chunkCmd.Flags().IntVar(&chunks, "chunks", 0, "number of chunks to split the video into")
	chunkCmd.Flags().Float64Var(&chunkLength, "length", 0, "length of each chunk in seconds")
	rootCmd.AddCommand(chunkCmd)

	// Transcode
	transcodeCmd := &cobra.Command{
		Use:   "transcode",
		Short: "Transcode video and audio streams",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveDefaults()
			if !iocore.IsVideo(input) {
				return fmt.Errorf("input must be a video (.mp4, .mkv, .mov, etc.): %s", input)
			}
			ctx := context.Background()
			cfg := makeFFmpegConfig()
			return iocore.Transcode(ctx, cfg, input, output, vcodec, acodec, vbitrate, abitrate, crf)
		},
	}
	transcodeCmd.Flags().StringVar(&vcodec, "vcodec", "", "video codec (e.g. h264, hevc, av1, vp9)")
	transcodeCmd.Flags().StringVar(&acodec, "acodec", "", "audio codec (e.g. aac, mp3, opus)")
	transcodeCmd.Flags().StringVar(&vbitrate, "vbitrate", "", "video bitrate (e.g. 5M, 1000k)")
	transcodeCmd.Flags().StringVar(&abitrate, "abitrate", "", "audio bitrate (e.g. 128k, 192k)")
	transcodeCmd.Flags().StringVar(&crf, "crf", "", "constant rate factor (e.g. 23, 28, 35)")
	rootCmd.AddCommand(transcodeCmd)

	// Concat
	concatCmd := &cobra.Command{
		Use:   "concat [input1] [input2]...",
		Short: "Seamlessly combine multiple video clips losslessly",
		Args:  cobra.MinimumNArgs(2), // Require at least 2 file arguments
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveDefaults()
			if output == "" {
				return fmt.Errorf("must specify an output file using -o or --output")
			}
			for _, f := range args {
				if !iocore.IsVideo(f) {
					return fmt.Errorf("input must be a video (.mp4, .mkv, .mov, etc.): %s", f)
				}
			}
			ctx := context.Background()
			cfg := makeFFmpegConfig()
			return iocore.Concat(ctx, cfg, args, output)
		},
	}
	rootCmd.AddCommand(concatCmd)
}
