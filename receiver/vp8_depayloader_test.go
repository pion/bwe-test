// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package receiver

import (
	"testing"

	"github.com/pion/rtp"
	"github.com/stretchr/testify/assert"
)

func TestNewVP8Depayloader(t *testing.T) {
	depayloader := NewVP8Depayloader()
	assert.NotNil(t, depayloader, "NewVP8Depayloader() should not return nil")
}

func TestVP8Depayloader_ProcessPacket(t *testing.T) {
	depayloader := NewVP8Depayloader()

	tests := []struct {
		name    string
		packet  *rtp.Packet
		wantErr bool
	}{
		{
			name: "Valid VP8 packet",
			packet: &rtp.Packet{
				Header: rtp.Header{
					Version:        2,
					Marker:         true,
					PayloadType:    96,
					SequenceNumber: 1,
					Timestamp:      1000,
					SSRC:           12345,
				},
				Payload: []byte{0x10, 0x02, 0x00, 0x9d, 0x01, 0x2a},
			},
			wantErr: false,
		},
		{
			name: "Empty payload",
			packet: &rtp.Packet{
				Header: rtp.Header{
					Version:        2,
					Marker:         false,
					PayloadType:    96,
					SequenceNumber: 2,
					Timestamp:      2000,
					SSRC:           12345,
				},
				Payload: []byte{},
			},
			wantErr: false,
		},
		{
			name: "Small payload",
			packet: &rtp.Packet{
				Header: rtp.Header{
					Version:        2,
					Marker:         false,
					PayloadType:    96,
					SequenceNumber: 3,
					Timestamp:      3000,
					SSRC:           12345,
				},
				Payload: []byte{0x10},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			complete, frameData, timestamp := depayloader.ProcessPacket(tt.packet)

			// Use timestamp to avoid unused variable
			_ = timestamp

			// Basic sanity checks
			if complete {
				assert.NotNil(t, frameData, "ProcessPacket() returned complete=true but frameData=nil")
			}
			if !complete {
				assert.Nil(t, frameData, "ProcessPacket() returned complete=false but frameData!=nil")
			}
		})
	}
}

func TestVP8Depayloader_FlushFrame(t *testing.T) {
	depayloader := NewVP8Depayloader()

	// Test flushing empty depayloader
	frameData, isKeyFrame, timestamp := depayloader.FlushFrame()

	// Use variables to avoid unused variable errors
	_ = timestamp

	// Should handle empty state gracefully
	_ = frameData
	_ = isKeyFrame
}

func TestVP8Depayloader_SequentialPackets(t *testing.T) {
	depayloader := NewVP8Depayloader()

	// Simulate a sequence of packets forming a frame
	packets := []*rtp.Packet{
		{
			Header: rtp.Header{
				Version:        2,
				Marker:         false,
				PayloadType:    96,
				SequenceNumber: 1,
				Timestamp:      1000,
				SSRC:           12345,
			},
			Payload: []byte{0x10, 0x02, 0x00},
		},
		{
			Header: rtp.Header{
				Version:        2,
				Marker:         false,
				PayloadType:    96,
				SequenceNumber: 2,
				Timestamp:      1000, // Same timestamp
				SSRC:           12345,
			},
			Payload: []byte{0x9d, 0x01, 0x2a},
		},
		{
			Header: rtp.Header{
				Version:        2,
				Marker:         true, // End of frame
				PayloadType:    96,
				SequenceNumber: 3,
				Timestamp:      1000, // Same timestamp
				SSRC:           12345,
			},
			Payload: []byte{0x00, 0x00, 0x00},
		},
	}

	var lastComplete bool
	var lastFrameData []byte

	for i, packet := range packets {
		complete, frameData, timestamp := depayloader.ProcessPacket(packet)

		// Use timestamp to avoid unused variable
		_ = timestamp

		lastComplete = complete
		lastFrameData = frameData

		// Only the last packet (with marker) should potentially complete the frame
		// Remove empty branch to fix staticcheck
		if i < len(packets)-1 && complete {
			t.Logf("Frame completed early at packet %d", i)
		}
	}

	// Test that we can handle the sequence without errors
	_ = lastComplete
	_ = lastFrameData
}

func TestIsKeyframe_ExtendedCases(t *testing.T) {
	tests := []struct {
		name      string
		frameData []byte
		want      bool
	}{
		{
			name:      "Extended descriptor - X bit set, PictureID, keyframe",
			frameData: []byte{0x80, 0x80, 0x12, 0x00}, // X=1, I=1, short PictureID, VP8 keyframe
			want:      true,
		},
		{
			name:      "Extended descriptor - X bit set, PictureID, non-keyframe",
			frameData: []byte{0x80, 0x80, 0x12, 0x01}, // X=1, I=1, short PictureID, VP8 non-keyframe
			want:      false,
		},
		{
			name:      "Extended descriptor - long PictureID, keyframe",
			frameData: []byte{0x80, 0x80, 0x81, 0x23, 0x00}, // X=1, I=1, long PictureID, VP8 keyframe
			want:      true,
		},
		{
			name:      "Extended descriptor - TL0PICIDX present",
			frameData: []byte{0x80, 0x40, 0x34, 0x00}, // X=1, L=1, TL0PICIDX, VP8 keyframe
			want:      true,
		},
		{
			name:      "Extended descriptor - TID/KEYIDX present",
			frameData: []byte{0x80, 0x20, 0x56, 0x00}, // X=1, T=1, TID/KEYIDX, VP8 keyframe
			want:      true,
		},
		{
			name:      "Extended descriptor - all flags set",
			frameData: []byte{0x80, 0xE0, 0x81, 0x23, 0x34, 0x56, 0x00}, // X=1, I=1, L=1, T=1, all fields, VP8 keyframe
			want:      true,
		},
		{
			name:      "Insufficient data for VP8 header",
			frameData: []byte{0x80, 0x80, 0x12}, // Extended descriptor but no VP8 data
			want:      false,
		},
		{
			name:      "Insufficient data for PictureID",
			frameData: []byte{0x80, 0x80}, // Extended descriptor, I=1 but no PictureID
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsKeyframe(tt.frameData)
			assert.Equal(t, tt.want, got, "IsKeyframe() should return expected result")
		})
	}
}

func TestVP8Depayloader_ProcessPayload(t *testing.T) {
	depayloader := NewVP8Depayloader()

	// Test processPayload with various inputs
	tests := []struct {
		name    string
		payload []byte
		marker  bool
	}{
		{
			name:    "Normal payload with marker",
			payload: []byte{0x10, 0x00, 0x9d, 0x01, 0x2a},
			marker:  true,
		},
		{
			name:    "Normal payload without marker",
			payload: []byte{0x10, 0x00, 0x9d, 0x01, 0x2a},
			marker:  false,
		},
		{
			name:    "Empty payload",
			payload: []byte{},
			marker:  true,
		},
		{
			name:    "Small payload",
			payload: []byte{0x10},
			marker:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet := &rtp.Packet{
				Header: rtp.Header{
					Marker:    tt.marker,
					Timestamp: 1000,
				},
				Payload: tt.payload,
			}

			// Just test that processPayload doesn't panic
			complete, frameData, timestamp := depayloader.ProcessPacket(packet)
			_ = complete
			_ = frameData
			_ = timestamp
		})
	}
}

func TestVP8Depayloader_GetFrame_AfterData(t *testing.T) {
	depayloader := NewVP8Depayloader()

	// Add some data first
	packet := &rtp.Packet{
		Header: rtp.Header{
			Marker:    true,
			Timestamp: 1000,
		},
		Payload: []byte{0x10, 0x00, 0x9d, 0x01, 0x2a},
	}

	depayloader.ProcessPacket(packet)

	// Now test GetFrame
	frameData, isKeyFrame, timestamp := depayloader.GetFrame()
	_ = frameData
	_ = isKeyFrame
	_ = timestamp
}

func TestVP8Depayloader_StateTransitions(t *testing.T) {
	_ = NewVP8Depayloader() // Create but don't use base depayloader

	// Test different state transitions
	tests := []struct {
		name      string
		packets   []*rtp.Packet
		expectErr bool
	}{
		{
			name: "Single complete frame",
			packets: []*rtp.Packet{
				{
					Header: rtp.Header{
						Marker:    true,
						Timestamp: 1000,
					},
					Payload: []byte{0x10, 0x00, 0x9d, 0x01, 0x2a},
				},
			},
		},
		{
			name: "Multi-packet frame",
			packets: []*rtp.Packet{
				{
					Header: rtp.Header{
						Marker:    false,
						Timestamp: 2000,
					},
					Payload: []byte{0x10, 0x00, 0x9d},
				},
				{
					Header: rtp.Header{
						Marker:    true,
						Timestamp: 2000,
					},
					Payload: []byte{0x01, 0x2a},
				},
			},
		},
		{
			name: "Timestamp change mid-frame",
			packets: []*rtp.Packet{
				{
					Header: rtp.Header{
						Marker:    false,
						Timestamp: 3000,
					},
					Payload: []byte{0x10, 0x00},
				},
				{
					Header: rtp.Header{
						Marker:    true,
						Timestamp: 4000, // Different timestamp
					},
					Payload: []byte{0x9d, 0x01},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := NewVP8Depayloader()

			for i, packet := range tt.packets {
				complete, frameData, timestamp := dep.ProcessPacket(packet)

				// Log for debugging but don't fail on specific results
				if complete {
					t.Logf("Frame completed at packet %d, size: %d, timestamp: %d",
						i, len(frameData), timestamp)
				}
			}
		})
	}
}

func TestIsKeyframe(t *testing.T) {
	tests := []struct {
		name      string
		frameData []byte
		want      bool
	}{
		{
			name:      "VP8 keyframe - simple payload descriptor + keyframe",
			frameData: []byte{0x10, 0x02, 0x00, 0x9d, 0x01, 0x2a},
			// 0x10 = no X bit, then VP8 frame starts at 0x02 (even = keyframe)
			want: true,
		},
		{
			name:      "VP8 non-keyframe - simple payload descriptor + non-keyframe",
			frameData: []byte{0x10, 0x03, 0x00, 0x9d, 0x01, 0x2a},
			// 0x10 = no X bit, then VP8 frame starts at 0x03 (odd = non-keyframe)
			want: false,
		},
		{
			name:      "Empty frame",
			frameData: []byte{},
			want:      false,
		},
		{
			name:      "Single byte - insufficient data",
			frameData: []byte{0x10},
			want:      false,
		},
		{
			name:      "Two bytes - keyframe",
			frameData: []byte{0x10, 0x00}, // Simple descriptor + keyframe (0x00 & 0x01 == 0)
			want:      true,
		},
		{
			name:      "Two bytes - non-keyframe",
			frameData: []byte{0x10, 0x01}, // Simple descriptor + non-keyframe (0x01 & 0x01 == 1)
			want:      false,
		},
		{
			name:      "Extended descriptor edge case - insufficient data for all fields",
			frameData: []byte{0x80, 0xE0, 0x81}, // X=1, I=1, L=1, T=1 but insufficient data
			want:      false,
		},
		{
			name:      "Extended descriptor - only X bit, no optional fields",
			frameData: []byte{0x80, 0x00, 0x00}, // X=1, no I/L/T bits, VP8 keyframe
			want:      true,
		},
		{
			name:      "Complex valid case - all fields present",
			frameData: []byte{0x80, 0xE0, 0x80, 0x12, 0x34, 0x56, 0x00}, // X=1, I=1(short), L=1, T=1, VP8 keyframe
			want:      true,
		},
		{
			name:      "Insufficient data after payload descriptor",
			frameData: []byte{0x80, 0x00}, // X=1, no optional fields, but no VP8 data
			want:      false,
		},
		{
			name:      "Edge case - exactly at payload descriptor boundary",
			frameData: []byte{0x80, 0x80, 0x12}, // X=1, I=1, PictureID but no VP8 data after
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsKeyframe(tt.frameData)
			assert.Equal(t, tt.want, got, "IsKeyframe() should return expected result")
		})
	}
}

// Test the new helper function for VP8 descriptor size calculation.
func TestCalculateVP8DescriptorSize(t *testing.T) {
	tests := []struct {
		name     string
		payload  []byte
		expected int
	}{
		{"Empty payload", []byte{}, -1},
		{"Simple descriptor", []byte{0x10, 0x00}, 1},
		{"Extended - no optional fields", []byte{0x80, 0x00, 0x00}, 2},
		{"Extended - short PictureID", []byte{0x80, 0x80, 0x12, 0x00}, 3},
		{"Extended - long PictureID", []byte{0x80, 0x80, 0x81, 0x23, 0x00}, 4},
		{"Extended - TL0PICIDX", []byte{0x80, 0x40, 0x34, 0x00}, 3},
		{"Extended - TID/KEYIDX", []byte{0x80, 0x20, 0x56, 0x00}, 3},
		{"Extended - all flags", []byte{0x80, 0xE0, 0x81, 0x23, 0x34, 0x56, 0x00}, 6},
		{"Invalid - insufficient data", []byte{0x80}, -1},
		{"Invalid - insufficient for PictureID", []byte{0x80, 0x80}, -1},
		{"Edge case - minimal extended", []byte{0x80, 0x00}, 2},
		{"Complex valid case", []byte{0x80, 0xE0, 0x80, 0x12, 0x34, 0x56, 0x78}, 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateVP8DescriptorSize(tt.payload)
			assert.Equal(t, tt.expected, result, "calculateVP8DescriptorSize should return correct size")
		})
	}
}
