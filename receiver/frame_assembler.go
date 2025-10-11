// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package receiver

import (
	"time"

	"github.com/pion/logging"
	"github.com/pion/rtp"
)

// VP8FrameAssembler handles assembly of VP8 frames from RTP packets.
type VP8FrameAssembler struct {
	depayloader     *VP8Depayloader
	hasKeyFrame     bool
	frameCount      uint64
	lastFrameTime   time.Time
	frameTimeout    time.Duration
	bufferKeyFrames [][]byte // Buffer to store keyframes if needed
	log             logging.LeveledLogger
}

// NewVP8FrameAssembler creates a new VP8 frame assembler.
func NewVP8FrameAssembler(logger logging.LeveledLogger) *VP8FrameAssembler {
	return &VP8FrameAssembler{
		depayloader:     NewVP8Depayloader(),
		hasKeyFrame:     false,
		frameCount:      0,
		lastFrameTime:   time.Now(),
		frameTimeout:    500 * time.Millisecond, // Timeout to consider a frame abandoned
		bufferKeyFrames: make([][]byte, 0),
		log:             logger,
	}
}

// ProcessPacket processes an RTP packet and returns a complete frame if available.
// Returns:
// - bool: true if a complete frame is available
// - []byte: the complete frame data, or nil if no frame is available
// - bool: true if the frame is a keyframe
// - uint64: the timestamp of the frame.
func (a *VP8FrameAssembler) ProcessPacket(packet *rtp.Packet) (bool, []byte, bool, uint64) {
	now := time.Now()

	// Check for timeout on current frame
	if now.Sub(a.lastFrameTime) > a.frameTimeout {
		a.log.Debugf("Frame timeout, flushing current frame")
		a.depayloader.FlushFrame()
	}
	a.lastFrameTime = now

	// Process the packet through the depayloader
	complete, frameData, _ := a.depayloader.ProcessPacket(packet)

	if complete && len(frameData) > 0 {
		// Check if this is a keyframe
		isKeyFrame := IsKeyframe(frameData)

		// If we haven't seen a keyframe yet, and this is one, mark that we have one
		if isKeyFrame && !a.hasKeyFrame {
			a.hasKeyFrame = true
			a.log.Infof("First keyframe received")
		}

		// Only return frames if we've seen a keyframe
		if a.hasKeyFrame {
			a.frameCount++

			return true, frameData, isKeyFrame, a.frameCount
		} else {
			a.log.Debugf("Dropping frame: no keyframe seen yet")

			return false, nil, false, 0
		}
	}

	return false, nil, false, 0
}

// FlushFrame forces completion of any in-progress frame.
func (a *VP8FrameAssembler) FlushFrame() (bool, []byte, bool, uint64) {
	frameData, isKeyFrame, _ := a.depayloader.FlushFrame()

	if len(frameData) > 0 && a.hasKeyFrame {
		a.frameCount++

		return true, frameData, isKeyFrame, a.frameCount
	}

	return false, nil, false, 0
}
