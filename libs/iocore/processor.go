package iocore

import (
	"context"
	"io"
)

// Processor defines the standard interface for all IO processing units.
// It supports streaming via io.Reader and io.Writer for high performance.
type Processor interface {
	Process(ctx context.Context, r io.Reader, w io.Writer) error
}

// Option defines a functional option for configuring processors.
type Option func(interface{})
