package iocore

import (
	"context"
	"io"
	"os/exec"
	"strings"
)

// RunBinary runs a binary with the given name and arguments.
// It automatically handles the ffmpeg -> ffmpeg-serve translation.
func RunBinary(ctx context.Context, name string, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	binPath, err := ResolveBinary(name)
	if err != nil {
		return err
	}

	isServe := (name == "ffmpeg" && strings.Contains(binPath, "ffmpeg-serve"))

	var cmd *exec.Cmd
	if isServe {
		// ffmpeg-serve convention: -i <in> -o <out> -- <rest>
		// We need to parse incoming args to find -i and -o if they exist,
		// or translate them.

		serveArgs := []string{}
		extraArgs := []string{}

		// Simple heuristic to extract -i and -o and move rest behind --
		for i := 0; i < len(args); i++ {
			arg := args[i]
			if (arg == "-i" || arg == "-o") && i+1 < len(args) {
				serveArgs = append(serveArgs, arg, args[i+1])
				i++
			} else if arg == "-y" {
				// skip -y as ffmpeg-serve adds it
				continue
			} else {
				extraArgs = append(extraArgs, arg)
			}
		}

		finalArgs := append(serveArgs, "--")
		finalArgs = append(finalArgs, extraArgs...)
		cmd = exec.CommandContext(ctx, binPath, finalArgs...)
	} else {
		cmd = exec.CommandContext(ctx, binPath, args...)
	}

	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	return cmd.Run()
}
