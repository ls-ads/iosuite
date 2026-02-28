package iocore

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ExtractFrames explodes a video into a directory of images.
func ExtractFrames(ctx context.Context, config *FFmpegConfig, videoPath, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}
	// ffmpeg -i %s %s/frame_%05d.png
	pattern := filepath.Join(outputDir, "frame_%05d.png")
	return RunFFmpegAction(ctx, config, videoPath, pattern, "", nil)
}

// Combine takes a directory of images and creates a video.
func Combine(ctx context.Context, config *FFmpegConfig, inputPattern, videoPath string, fps int) error {
	// ffmpeg -framerate %d -i %s/frame_%05d.png -c:v libx264 -pix_fmt yuv420p %s
	extraArgs := []string{
		"-framerate", fmt.Sprintf("%d", fps),
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
	}
	return RunFFmpegAction(ctx, config, inputPattern, videoPath, "", extraArgs)
}

// ExtractAudio extracts the audio stream from a video.
func ExtractAudio(ctx context.Context, config *FFmpegConfig, videoPath, audioPath string) error {
	// ffmpeg -i %s -vn -acodec copy %s
	extraArgs := []string{"-vn", "-acodec", "copy"}
	return RunFFmpegAction(ctx, config, videoPath, audioPath, "", extraArgs)
}

// Stack combines two inputs (image or video) into a side-by-side comparison.
func Stack(ctx context.Context, config *FFmpegConfig, input1, input2, output string, axis string) error {
	// Local only for now as it takes two inputs
	if config != nil && config.Provider != ProviderLocalCPU && config.Provider != ProviderLocalGPU && config.Provider != "" {
		return fmt.Errorf("stack is currently only supported locally")
	}

	var filter string
	if axis == "v" {
		filter = "vstack=inputs=2"
	} else {
		filter = "hstack=inputs=2"
	}

	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-i", input1,
		"-i", input2,
		"-filter_complex", fmt.Sprintf("[0:v][1:v]%s[v]", filter),
		"-map", "[v]",
		"-y", output,
	}

	binPath, err := ResolveBinary("ffmpeg-serve")
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg stack failed: %v", err)
	}
	return nil
}
