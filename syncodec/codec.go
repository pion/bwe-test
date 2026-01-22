// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package syncodec provides synthetic codec implementations for bandwidth estimation tests.
// It simulates media codecs with configurable bitrate and other parameters.
package syncodec

import (
	"fmt"
	"time"
)

// Frame represents a media frame with content and duration.
type Frame struct {
	Content  []byte        // Raw frame data
	Duration time.Duration // Duration of the frame
}

func (f Frame) String() string {
	return fmt.Sprintf("FRAME: \n\tDURATION: %v\n\tSIZE: %v\n", f.Duration, len(f.Content))
}

// Codec defines the interface for synthetic codecs.
type Codec interface {
	// GetTargetBitrate returns the current target bitrate in bits per second.
	GetTargetBitrate() int

	// SetTargetBitrate sets the target bitrate in bits per second.
	SetTargetBitrate(int)

	// Start begins the codec operation.
	Start()

	// Close stops the codec and cleans up resources.
	Close() error
}

// FrameWriter defines the interface for writing frames.
type FrameWriter interface {
	// WriteFrame writes a frame to the underlying media sink.
	WriteFrame(Frame)
}
