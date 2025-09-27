// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package receiver

import (
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
)

// VP8Depayloader handles VP8 RTP payloads using Pion's VP8Packet.
type VP8Depayloader struct {
	vp8Packet        *codecs.VP8Packet
	frameBuffer      [][]byte // Store frame parts
	currentTimestamp uint32
}

// NewVP8Depayloader creates a new VP8 depayloader.
func NewVP8Depayloader() *VP8Depayloader {
	return &VP8Depayloader{
		vp8Packet:   &codecs.VP8Packet{},
		frameBuffer: make([][]byte, 0),
	}
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

// ProcessPacket processes a VP8 RTP packet using Pion's VP8Packet.
// Returns complete frame data when a frame is complete.
func (d *VP8Depayloader) ProcessPacket(packet *rtp.Packet) (bool, []byte, uint32) {
	if packet == nil || len(packet.Payload) == 0 {
		return false, nil, 0
	}

	// Use Pion's VP8Packet to unmarshal
	frameData, err := d.vp8Packet.Unmarshal(packet.Payload)
	if err != nil {
		return false, nil, 0
	}

	timestamp := packet.Timestamp

	// Check if this is a new frame (timestamp changed)
	if d.currentTimestamp != timestamp && d.currentTimestamp != 0 {
		// Complete current frame and return it
		if len(d.frameBuffer) > 0 {
			completeFrame := d.assembleFrame()
			ts := d.currentTimestamp

			// Reset for new frame
			d.frameBuffer = d.frameBuffer[:0]
			d.currentTimestamp = timestamp
			d.frameBuffer = append(d.frameBuffer, frameData)

			return true, completeFrame, ts
		}
	}

	// Initialize timestamp if first packet
	if d.currentTimestamp == 0 {
		d.currentTimestamp = timestamp
	}

	// Add frame data to buffer
	d.frameBuffer = append(d.frameBuffer, frameData)

	// Check if this is the final packet using marker bit and partition tail
	if packet.Marker || d.vp8Packet.IsPartitionTail(packet.Marker, frameData) {
		completeFrame := d.assembleFrame()
		ts := d.currentTimestamp

		// Reset for next frame
		d.frameBuffer = d.frameBuffer[:0]
		d.currentTimestamp = 0

		return true, completeFrame, ts
	}

	return false, nil, 0
}

// assembleFrame combines frame buffer parts into a complete frame.
func (d *VP8Depayloader) assembleFrame() []byte {
	if len(d.frameBuffer) == 0 {
		return nil
	}

	// Calculate total size
	totalSize := 0
	for _, part := range d.frameBuffer {
		totalSize += len(part)
	}

	// Assemble frame
	completeFrame := make([]byte, 0, totalSize)
	for _, part := range d.frameBuffer {
		completeFrame = append(completeFrame, part...)
	}

	return completeFrame
}

// GetFrame returns the current complete frame and resets the assembler.
func (d *VP8Depayloader) GetFrame() ([]byte, bool, uint32) {
	if len(d.frameBuffer) == 0 {
		return nil, false, 0
	}

	frame := d.assembleFrame()
	timestamp := d.currentTimestamp

	// Reset for the next frame
	d.frameBuffer = d.frameBuffer[:0]
	d.currentTimestamp = 0

	// Check if this is a keyframe
	isKeyFrame := false
	if len(frame) > 0 {
		isKeyFrame = IsKeyframe(frame)
	}

	return frame, isKeyFrame, timestamp
}

// FlushFrame forces completion of the current frame.
func (d *VP8Depayloader) FlushFrame() ([]byte, bool, uint32) {
	return d.GetFrame()
}
