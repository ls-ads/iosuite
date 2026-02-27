package iocore

import (
	"context"
	"fmt"
	"strings"
)

// Pipeline allows chaining multiple FFmpeg transformations into a single execution.
type Pipeline struct {
	ctx       context.Context
	config    *FFmpegConfig
	input     string
	output    string
	filters   []string
	extraArgs []string
}

// NewPipeline creates a new FFmpeg transformation pipeline.
func NewPipeline(ctx context.Context, config *FFmpegConfig, input, output string) *Pipeline {
	return &Pipeline{
		ctx:    ctx,
		config: config,
		input:  input,
		output: output,
	}
}

// Scale adds a scaling filter to the pipeline.
func (p *Pipeline) Scale(width, height int) *Pipeline {
	p.filters = append(p.filters, fmt.Sprintf("scale=%d:%d", width, height))
	return p
}

// Crop adds a cropping filter to the pipeline.
func (p *Pipeline) Crop(w, h, x, y int) *Pipeline {
	p.filters = append(p.filters, fmt.Sprintf("crop=%d:%d:%d:%d", w, h, x, y))
	return p
}

// Rotate adds a rotation filter to the pipeline.
func (p *Pipeline) Rotate(degrees int) *Pipeline {
	// FFmpeg rotate uses radians
	rad := float64(degrees) * 3.14159 / 180.0
	p.filters = append(p.filters, fmt.Sprintf("rotate=%f", rad))
	return p
}

// Flip adds a flipping filter to the pipeline.
func (p *Pipeline) Flip(axis string) *Pipeline {
	if axis == "v" {
		p.filters = append(p.filters, "vflip")
	} else {
		p.filters = append(p.filters, "hflip")
	}
	return p
}

// Brighten adds a brightness adjustment filter.
func (p *Pipeline) Brighten(level float64) *Pipeline {
	p.filters = append(p.filters, fmt.Sprintf("eq=brightness=%f", level))
	return p
}

// Contrast adds a contrast adjustment filter.
func (p *Pipeline) Contrast(level float64) *Pipeline {
	// eq filter contrast is 1.0 based
	c := 1.0 + (level / 100.0)
	p.filters = append(p.filters, fmt.Sprintf("eq=contrast=%f", c))
	return p
}

// Saturate adds a saturation filter.
func (p *Pipeline) Saturate(level float64) *Pipeline {
	p.filters = append(p.filters, fmt.Sprintf("hue=s=%f", level))
	return p
}

// Denoise adds a denoising filter.
func (p *Pipeline) Denoise(preset string) *Pipeline {
	p.filters = append(p.filters, "hqdn3d")
	return p
}

// Sharpen adds a sharpening filter.
func (p *Pipeline) Sharpen(amount float64) *Pipeline {
	p.filters = append(p.filters, fmt.Sprintf("unsharp=luma_msize_x=3:luma_msize_y=3:luma_amount=%f", amount))
	return p
}

// Extra adds arbitrary extra arguments to the final FFmpeg command.
func (p *Pipeline) Extra(args ...string) *Pipeline {
	p.extraArgs = append(p.extraArgs, args...)
	return p
}

// Run executes the pipeline.
func (p *Pipeline) Run() error {
	if len(p.filters) == 0 && len(p.extraArgs) == 0 {
		return fmt.Errorf("pipeline has no operations")
	}

	filterChain := ""
	if len(p.filters) > 0 {
		filterChain = strings.Join(p.filters, ",")
	}

	return RunFFmpegAction(p.ctx, p.config, p.input, p.output, filterChain, p.extraArgs)
}
