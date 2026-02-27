package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"iosuite.io/libs/iocore"
)

var (
	pipelineOps string
)

func init() {
	pipelineCmd := &cobra.Command{
		Use:   "pipeline",
		Short: "Run a sequence of transformations in a single pass",
		Long: `Run multiple transformations chained together.
Example: ioimg pipeline -i in.jpg -o out.jpg --ops "scale=1280x720,brighten=0.1,contrast=5"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolveDefaults()
			if !iocore.IsImage(input) && !iocore.IsVideo(input) {
				return fmt.Errorf("unsupported input: %s", input)
			}

			ctx := context.Background()
			cfg := &iocore.FFmpegConfig{
				Provider: iocore.UpscaleProvider(provider),
				APIKey:   apiKey,
				Model:    model,
			}

			pipe := iocore.NewPipeline(ctx, cfg, input, output)

			ops := strings.Split(pipelineOps, ",")
			for _, opStr := range ops {
				parts := strings.Split(opStr, "=")
				op := strings.TrimSpace(parts[0])
				if op == "" {
					continue
				}

				val := ""
				if len(parts) > 1 {
					val = strings.TrimSpace(parts[1])
				}

				switch op {
				case "scale":
					wh := strings.Split(val, "x")
					if len(wh) != 2 {
						return fmt.Errorf("invalid scale format: %s (expected WxH)", val)
					}
					w, _ := strconv.Atoi(wh[0])
					h, _ := strconv.Atoi(wh[1])
					pipe.Scale(w, h)
				case "crop":
					whxy := strings.Split(val, "x")
					if len(whxy) != 4 {
						return fmt.Errorf("invalid crop format: %s (expected WxHxXxY)", val)
					}
					w, _ := strconv.Atoi(whxy[0])
					h, _ := strconv.Atoi(whxy[1])
					x, _ := strconv.Atoi(whxy[2])
					y, _ := strconv.Atoi(whxy[3])
					pipe.Crop(w, h, x, y)
				case "rotate":
					deg, _ := strconv.Atoi(val)
					pipe.Rotate(deg)
				case "flip":
					pipe.Flip(val)
				case "brighten":
					l, _ := strconv.ParseFloat(val, 64)
					pipe.Brighten(l)
				case "contrast":
					l, _ := strconv.ParseFloat(val, 64)
					pipe.Contrast(l)
				case "saturate":
					l, _ := strconv.ParseFloat(val, 64)
					pipe.Saturate(l)
				case "denoise":
					pipe.Denoise(val)
				case "sharpen":
					a, _ := strconv.ParseFloat(val, 64)
					pipe.Sharpen(a)
				default:
					return fmt.Errorf("unknown operation: %s", op)
				}
			}

			return pipe.Run()
		},
	}

	pipelineCmd.Flags().StringVar(&pipelineOps, "ops", "", "Comma-separated operations (e.g. scale=1280x720,brighten=0.1)")
	pipelineCmd.MarkFlagRequired("ops")

	rootCmd.AddCommand(pipelineCmd)
}
