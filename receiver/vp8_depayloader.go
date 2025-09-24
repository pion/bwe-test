// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package receiver

import (
	"github.com/pion/rtp"
)

// VP8Depayloader handles VP8 RTP payloads.
type VP8Depayloader struct {
	currentFrame        []byte
	currentFrameIsFirst bool
	currentTimestamp    uint32
	frameComplete       bool
}

// NewVP8Depayloader creates a new VP8 depayloader.
func NewVP8Depayloader() *VP8Depayloader {
	return &VP8Depayloader{}
}

// IsKeyframe checks if the VP8 frame is a keyframe.
// VP8 keyframes are identified by the P bit in the frame header.
// See https://tools.ietf.org/html/rfc6386#section-9.1.
func IsKeyframe(payload []byte) bool {
	if len(payload) < 1 {
		return false
	}

	// Calculate VP8 payload descriptor size
	payloadDescriptorSize := calculateVP8DescriptorSize(payload)
	if payloadDescriptorSize < 0 {
		return false // Invalid payload
	}

	// Ensure we have enough data for the VP8 header
	if len(payload) <= payloadDescriptorSize {
		return false
	}

	// Check first byte of VP8 frame
	// The P bit (bit 0, 0x01) being 0 indicates a key frame in VP8
	return (payload[payloadDescriptorSize] & 0x01) == 0
}

// calculateVP8DescriptorSize calculates the size of the VP8 payload descriptor.
func calculateVP8DescriptorSize(payload []byte) int {
	if len(payload) < 1 {
		return -1
	}

	payloadDescriptorSize := 1
	xBit := (payload[0] & 0x80) != 0

	// Extended control bits present
	if !xBit {
		return payloadDescriptorSize
	}

	payloadDescriptorSize++
	if len(payload) < payloadDescriptorSize {
		return -1
	}

	// Check for PictureID, TL0PICIDX, TID/KEYIDX
	if payload[1]&0x80 != 0 { // I bit - PictureID present
		if len(payload) < payloadDescriptorSize+1 {
			return -1
		}
		if payload[payloadDescriptorSize]&0x80 != 0 {
			// Long PictureID (2 bytes)
			payloadDescriptorSize += 2
		} else {
			// Short PictureID (1 byte)
			payloadDescriptorSize += 1
		}
	}

	if payload[1]&0x40 != 0 { // L bit - TL0PICIDX present
		payloadDescriptorSize++
	}

	if payload[1]&0x20 != 0 { // T/K bit - TID/KEYIDX present
		payloadDescriptorSize++
	}

	return payloadDescriptorSize
}

// ProcessPacket processes a VP8 RTP packet.
// Returns complete frame data when a frame is complete.
func (d *VP8Depayloader) ProcessPacket(packet *rtp.Packet) (bool, []byte, uint32) {
	if packet == nil || len(packet.Payload) == 0 {
		return false, nil, 0
	}

	payload := packet.Payload
	timestamp := packet.Timestamp

	// If timestamp changed, we have a new frame
	if d.currentTimestamp != timestamp && d.currentTimestamp != 0 {
		// If we have data from the previous frame, mark it as complete
		if len(d.currentFrame) > 0 {
			d.frameComplete = true
		}
	}

	// If we have a complete frame, return it before processing the new packet
	if d.frameComplete {
		completeFrame := d.currentFrame
		_ = d.currentFrameIsFirst
		ts := d.currentTimestamp

		// Reset for new frame
		d.currentFrame = nil
		d.frameComplete = false
		d.currentFrameIsFirst = false

		// Start processing the new packet
		d.currentTimestamp = timestamp

		// Process the first packet of the new frame
		frame, isFirst := d.processPayload(payload)
		d.currentFrame = frame
		d.currentFrameIsFirst = isFirst

		// Return the completed frame
		return true, completeFrame, ts
	}

	// If this is the first packet we're seeing
	if d.currentTimestamp == 0 {
		d.currentTimestamp = timestamp
	}

	// Process the payload
	frame, isFirst := d.processPayload(payload)

	// If this is a new frame, start with this payload
	if d.currentTimestamp != timestamp {
		d.currentTimestamp = timestamp
		d.currentFrame = frame
		d.currentFrameIsFirst = isFirst
	} else {
		// Append payload to current frame
		d.currentFrame = append(d.currentFrame, frame...)

		// If this is the first packet of a frame, update the first flag
		if isFirst {
			d.currentFrameIsFirst = true
		}
	}

	return false, nil, 0
}

// processPayload extracts the VP8 frame data from an RTP payload.
// Returns the frame data and a boolean indicating if this is potentially the first packet.
func (d *VP8Depayloader) processPayload(payload []byte) ([]byte, bool) {
	if len(payload) < 1 {
		return nil, false
	}

	// Calculate VP8 payload descriptor size using helper function
	payloadDescriptorSize := calculateVP8DescriptorSize(payload)
	if payloadDescriptorSize < 0 {
		return nil, false
	}

	// Make sure we have enough data
	if len(payload) <= payloadDescriptorSize {
		return nil, false
	}

	// Get start bit from payload descriptor
	startBit := (payload[0] & 0x10) != 0

	// Extract actual VP8 frame data
	frameData := payload[payloadDescriptorSize:]

	return frameData, startBit
}

// GetFrame returns the current complete frame and resets the assembler.
func (d *VP8Depayloader) GetFrame() ([]byte, bool, uint32) {
	if len(d.currentFrame) == 0 {
		return nil, false, 0
	}

	frame := d.currentFrame
	isFirst := d.currentFrameIsFirst
	timestamp := d.currentTimestamp

	// Reset for the next frame
	d.currentFrame = nil
	d.currentFrameIsFirst = false
	d.frameComplete = false

	// Check if this is a keyframe
	isKeyFrame := false
	if isFirst && len(frame) > 0 {
		isKeyFrame = IsKeyframe(frame)
	}

	return frame, isKeyFrame, timestamp
}

// FlushFrame forces completion of the current frame.
func (d *VP8Depayloader) FlushFrame() ([]byte, bool, uint32) {
	d.frameComplete = true

	return d.GetFrame()
}
