package iocore

import (
	"context"
	"io"
	"os"
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
		// Improved heuristic to extract -i and -o and move rest behind --
		var input, output string
		var extraArgs []string

		for i := 0; i < len(args); i++ {
			arg := args[i]
			if arg == "-i" && i+1 < len(args) {
				input = args[i+1]
				i++
			} else if arg == "-o" && i+1 < len(args) {
				output = args[i+1]
				i++
			} else if arg == "-y" {
				// skip -y as ffmpeg-serve adds it
				continue
			} else {
				extraArgs = append(extraArgs, arg)
			}
		}

		// If no explicit -o was found, check if the last extra arg is a positional output
		if output == "" && len(extraArgs) > 0 {
			last := extraArgs[len(extraArgs)-1]
			// Positional args in ffmpeg don't start with - (except for "-" meaning stdout)
			if !strings.HasPrefix(last, "-") || last == "-" {
				isFlagValue := false
				if len(extraArgs) >= 2 {
					prev := extraArgs[len(extraArgs)-2]
					if strings.HasPrefix(prev, "-") {
						isFlagValue = true
					}
				}
				if !isFlagValue {
					output = last
					extraArgs = extraArgs[:len(extraArgs)-1]
				}
			}
		}

		// Workaround for ffmpeg-serve's poor support for stdin/stdout pipes
		var tmpIn, tmpOut string
		if input == "-" && stdin != nil {
			f, err := os.CreateTemp("", "iocore-in-*.tmp")
			if err == nil {
				io.Copy(f, stdin)
				f.Close()
				tmpIn = f.Name()
				input = tmpIn
				stdin = nil // consumed
			}
		}
		if output == "-" && stdout != nil {
			// Change to a temp file for processing, then copy back
			f, err := os.CreateTemp("", "iocore-out-*.tmp")
			if err == nil {
				f.Close()
				tmpOut = f.Name()
				output = tmpOut
			}
		}

		if tmpIn != "" {
			defer os.Remove(tmpIn)
		}
		if tmpOut != "" {
			defer os.Remove(tmpOut)
		}

		serveArgs := []string{}
		if input != "" {
			serveArgs = append(serveArgs, "-i", input)
		}
		if output != "" {
			serveArgs = append(serveArgs, "-o", output)
		}

		finalArgs := append(serveArgs, "--")
		finalArgs = append(finalArgs, extraArgs...)
		cmd = exec.CommandContext(ctx, binPath, finalArgs...)

		cmd.Stdin = stdin
		cmd.Stdout = stdout
		cmd.Stderr = stderr

		if err := cmd.Run(); err != nil {
			return err
		}

		if tmpOut != "" {
			// Copy result back to the original stdout writer
			data, err := os.ReadFile(tmpOut)
			if err != nil {
				return err
			}
			if _, err := stdout.Write(data); err != nil {
				return err
			}
		}
		return nil
	} else {
		cmd = exec.CommandContext(ctx, binPath, args...)
		cmd.Stdin = stdin
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		return cmd.Run()
	}
}
