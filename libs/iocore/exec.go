package iocore

import (
	"context"
	"io"
	"os/exec"
)

// RunBinary runs a binary with the given name and arguments.
// It securely resolves the binary using rules from ResolveBinary,
// strictly enforcing that 'ffmpeg-serve' is explicitly requested
// over system fallbacks like 'ffmpeg'.
func RunBinary(ctx context.Context, name string, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	binPath, err := ResolveBinary(name)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
