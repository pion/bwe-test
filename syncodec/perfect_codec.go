// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package syncodec

import (
	"time"
)

var _ Codec = (*PerfectCodec)(nil)

// PerfectCodec implements a simple codec that produces frames at a constant rate
// with sizes exactly matching the target bitrate.
type PerfectCodec struct {
	writer FrameWriter

	targetBitrateBps int
	fps              int

	done chan struct{}
}

// NewPerfectCodec creates a new PerfectCodec with the specified frame writer and target bitrate.
func NewPerfectCodec(writer FrameWriter, targetBitrateBps int) *PerfectCodec {
	return &PerfectCodec{
		writer:           writer,
		targetBitrateBps: targetBitrateBps,
		fps:              30,
		done:             make(chan struct{}),
	}
}

// GetTargetBitrate returns the current target bitrate in bit per second.
func (c *PerfectCodec) GetTargetBitrate() int {
	return c.targetBitrateBps
}

// SetTargetBitrate sets the target bitrate to r bits per second.
func (c *PerfectCodec) SetTargetBitrate(r int) {
	c.targetBitrateBps = r
}

// Start begins the codec operation, generating frames at the configured frame rate.
func (c *PerfectCodec) Start() {
	msToNextFrame := time.Duration((1.0/float64(c.fps))*1000.0) * time.Millisecond
	ticker := time.NewTicker(msToNextFrame)
	for {
		select {
		case <-ticker.C:
			c.writer.WriteFrame(Frame{
				Content:  make([]byte, c.targetBitrateBps/(8.0*c.fps)),
				Duration: msToNextFrame,
			})
		case <-c.done:
			return
		}
	}
}

// Close stops the codec and cleans up resources.
func (c *PerfectCodec) Close() error {
	close(c.done)

	return nil
}
