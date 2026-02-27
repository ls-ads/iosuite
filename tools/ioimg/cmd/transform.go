package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"iosuite.io/libs/iocore"
)

var (
	imgWidth   int
	imgHeight  int
	imgCropW   int
	imgCropH   int
	imgCropX   int
	imgCropY   int
	imgDegrees int
	imgAxis    string
	imgAspect  string
	imgLevel   float64
	imgPreset  string
	imgAmount  float64
)

func init() {
	// Scale
	scaleCmd := &cobra.Command{
		Use:   "scale",
		Short: "Scale image",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !iocore.IsImage(input) {
				return fmt.Errorf("input must be an image (.jpg, .jpeg, .png): %s", input)
			}
			ctx := context.Background()
			cfg := &iocore.FFmpegConfig{
				Provider: iocore.UpscaleProvider(provider),
				APIKey:   apiKey,
				Model:    model,
			}
			return iocore.Scale(ctx, cfg, input, output, imgWidth, imgHeight)
		},
	}
	scaleCmd.Flags().IntVar(&imgWidth, "width", 1280, "target width")
	scaleCmd.Flags().IntVar(&imgHeight, "height", 720, "target height")
	rootCmd.AddCommand(scaleCmd)

	// Crop
	cropCmd := &cobra.Command{
		Use:   "crop",
		Short: "Crop image",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !iocore.IsImage(input) {
				return fmt.Errorf("input must be an image (.jpg, .jpeg, .png): %s", input)
			}
			ctx := context.Background()
			cfg := &iocore.FFmpegConfig{Provider: iocore.UpscaleProvider(provider), APIKey: apiKey, Model: model}
			return iocore.Crop(ctx, cfg, input, output, imgCropW, imgCropH, imgCropX, imgCropY)
		},
	}
	cropCmd.Flags().IntVarP(&imgCropW, "width", "w", 0, "crop width")
	cropCmd.Flags().IntVarP(&imgCropH, "height", "h", 0, "crop height")
	cropCmd.Flags().IntVarP(&imgCropX, "x", "x", 0, "crop x")
	cropCmd.Flags().IntVarP(&imgCropY, "y", "y", 0, "crop y")
	cropCmd.Flags().BoolP("help", "H", false, "help for crop")
	rootCmd.AddCommand(cropCmd)

	// Rotate
	rotateCmd := &cobra.Command{
		Use:   "rotate",
		Short: "Rotate image",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !iocore.IsImage(input) {
				return fmt.Errorf("input must be an image (.jpg, .jpeg, .png): %s", input)
			}
			ctx := context.Background()
			cfg := &iocore.FFmpegConfig{Provider: iocore.UpscaleProvider(provider), APIKey: apiKey, Model: model}
			return iocore.Rotate(ctx, cfg, input, output, imgDegrees)
		},
	}
	rotateCmd.Flags().IntVar(&imgDegrees, "degrees", 0, "degrees (90, 180, 270 or arbitrary)")
	rootCmd.AddCommand(rotateCmd)

	// Flip
	flipCmd := &cobra.Command{
		Use:   "flip",
		Short: "Flip image",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !iocore.IsImage(input) {
				return fmt.Errorf("input must be an image (.jpg, .jpeg, .png): %s", input)
			}
			ctx := context.Background()
			cfg := &iocore.FFmpegConfig{Provider: iocore.UpscaleProvider(provider), APIKey: apiKey, Model: model}
			return iocore.Flip(ctx, cfg, input, output, imgAxis)
		},
	}
	flipCmd.Flags().StringVar(&imgAxis, "axis", "h", "axis (h or v)")
	rootCmd.AddCommand(flipCmd)

	// Pad
	padCmd := &cobra.Command{
		Use:   "pad",
		Short: "Pad image to aspect ratio",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !iocore.IsImage(input) {
				return fmt.Errorf("input must be an image (.jpg, .jpeg, .png): %s", input)
			}
			ctx := context.Background()
			cfg := &iocore.FFmpegConfig{Provider: iocore.UpscaleProvider(provider), APIKey: apiKey, Model: model}
			return iocore.Pad(ctx, cfg, input, output, imgAspect)
		},
	}
	padCmd.Flags().StringVar(&imgAspect, "aspect", "16:9", "target aspect ratio")
	rootCmd.AddCommand(padCmd)

	// Brighten
	brightenCmd := &cobra.Command{
		Use:   "brighten",
		Short: "Adjust brightness",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !iocore.IsImage(input) {
				return fmt.Errorf("input must be an image (.jpg, .jpeg, .png): %s", input)
			}
			ctx := context.Background()
			cfg := &iocore.FFmpegConfig{Provider: iocore.UpscaleProvider(provider), APIKey: apiKey, Model: model}
			return iocore.Brighten(ctx, cfg, input, output, imgLevel)
		},
	}
	brightenCmd.Flags().Float64Var(&imgLevel, "level", 0.0, "brightness level (-1.0 to 1.0)")
	rootCmd.AddCommand(brightenCmd)

	// Contrast
	contrastCmd := &cobra.Command{
		Use:   "contrast",
		Short: "Adjust contrast",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !iocore.IsImage(input) {
				return fmt.Errorf("input must be an image (.jpg, .jpeg, .png): %s", input)
			}
			ctx := context.Background()
			cfg := &iocore.FFmpegConfig{Provider: iocore.UpscaleProvider(provider), APIKey: apiKey, Model: model}
			return iocore.Contrast(ctx, cfg, input, output, imgLevel)
		},
	}
	contrastCmd.Flags().Float64Var(&imgLevel, "level", 0.0, "contrast level (-100 to 100)")
	rootCmd.AddCommand(contrastCmd)

	// Saturate
	saturateCmd := &cobra.Command{
		Use:   "saturate",
		Short: "Adjust saturation",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !iocore.IsImage(input) {
				return fmt.Errorf("input must be an image (.jpg, .jpeg, .png): %s", input)
			}
			ctx := context.Background()
			cfg := &iocore.FFmpegConfig{Provider: iocore.UpscaleProvider(provider), APIKey: apiKey, Model: model}
			return iocore.Saturate(ctx, cfg, input, output, imgLevel)
		},
	}
	saturateCmd.Flags().Float64Var(&imgLevel, "level", 1.0, "saturation level (0 to 3)")
	rootCmd.AddCommand(saturateCmd)

	// Denoise
	denoiseCmd := &cobra.Command{
		Use:   "denoise",
		Short: "Denoise image",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !iocore.IsImage(input) {
				return fmt.Errorf("input must be an image (.jpg, .jpeg, .png): %s", input)
			}
			ctx := context.Background()
			cfg := &iocore.FFmpegConfig{Provider: iocore.UpscaleProvider(provider), APIKey: apiKey, Model: model}
			return iocore.Denoise(ctx, cfg, input, output, imgPreset)
		},
	}
	denoiseCmd.Flags().StringVar(&imgPreset, "preset", "med", "preset (weak, med, strong)")
	rootCmd.AddCommand(denoiseCmd)

	// Sharpen
	sharpenCmd := &cobra.Command{
		Use:   "sharpen",
		Short: "Sharpen image",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !iocore.IsImage(input) {
				return fmt.Errorf("input must be an image (.jpg, .jpeg, .png): %s", input)
			}
			ctx := context.Background()
			cfg := &iocore.FFmpegConfig{Provider: iocore.UpscaleProvider(provider), APIKey: apiKey, Model: model}
			return iocore.Sharpen(ctx, cfg, input, output, imgAmount)
		},
	}
	sharpenCmd.Flags().Float64Var(&imgAmount, "amount", 1.0, "sharpen amount (0 to 5)")
	rootCmd.AddCommand(sharpenCmd)
}
