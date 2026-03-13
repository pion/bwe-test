//go:build !js
// +build !js

// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"image"
	"testing"
)

// newBenchSender creates an RTCSender with numTracks video tracks for benchmarking.
// Tracks are added but no peer connection is set up (no WebRTC needed for these benchmarks).
func newBenchSender(b *testing.B, numTracks int) *RTCSender {
	b.Helper()

	sender, err := NewRTCSender()
	if err != nil {
		b.Fatalf("NewRTCSender: %v", err)
	}

	for i := range numTracks {
		trackID := "cam-" + string(rune('0'+i/10)) + string(rune('0'+i%10))
		err = sender.AddVideoTrack(VideoTrackInfo{
			TrackID:        trackID,
			Width:          1280,
			Height:         720,
			InitialBitrate: 500_000,
		})
		if err != nil {
			b.Fatalf("AddVideoTrack(%s): %v", trackID, err)
		}
	}

	return sender
}

// BenchmarkProcessEncodedFrames_NoFrames measures the hot-path cost of
// processEncodedFrames when no frames are available (the common case between
// frame arrivals). Before: parallel goroutines + WaitGroup per tick.
// After: sequential non-blocking Read loop.
func BenchmarkProcessEncodedFrames_NoFrames(b *testing.B) {
	for _, numTracks := range []int{1, 4, 14} {
		b.Run(trackCountName(numTracks), func(b *testing.B) {
			sender := newBenchSender(b, numTracks)
			defer func() { _ = sender.Close() }()

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				sender.processEncodedFrames()
			}
		})
	}
}

// BenchmarkProcessEncodedFrames_WithFrames measures processEncodedFrames when
// every track has a frame ready. This exercises the encode+write path.
func BenchmarkProcessEncodedFrames_WithFrames(b *testing.B) {
	for _, numTracks := range []int{1, 4, 14} {
		b.Run(trackCountName(numTracks), func(b *testing.B) {
			sender := newBenchSender(b, numTracks)
			defer func() { _ = sender.Close() }()

			// Pre-create test frame
			testImg := image.NewYCbCr(
				image.Rect(0, 0, 1280, 720),
				image.YCbCrSubsampleRatio420,
			)

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				// Feed a frame to every track
				sender.tracksMu.RLock()
				for _, track := range sender.tracks {
					if fb, ok := track.videoSource.(*FrameBuffer); ok {
						_ = fb.SendFrame(testImg)
					}
				}
				sender.tracksMu.RUnlock()

				sender.processEncodedFrames()
			}
		})
	}
}

// BenchmarkUpdateBitrate measures the bitrate update path.
// Before: Infof logging per tick. After: Debugf (no-op at default level).
func BenchmarkUpdateBitrate(b *testing.B) {
	for _, numTracks := range []int{1, 4, 14} {
		b.Run(trackCountName(numTracks), func(b *testing.B) {
			sender := newBenchSender(b, numTracks)
			defer func() { _ = sender.Close() }()

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				sender.updateBitrate(5_000_000)
			}
		})
	}
}

// BenchmarkFrameBufferRead measures the non-blocking Read path.
func BenchmarkFrameBufferRead_Empty(b *testing.B) {
	fb := NewFrameBuffer(1280, 720)
	defer func() { _ = fb.Close() }()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, release, _ := fb.Read()
		if release != nil {
			release()
		}
	}
}

// BenchmarkFrameBufferRead_WithFrame measures Read when a frame is available.
func BenchmarkFrameBufferRead_WithFrame(b *testing.B) {
	fb := NewFrameBuffer(1280, 720)
	defer func() { _ = fb.Close() }()

	testImg := image.NewYCbCr(
		image.Rect(0, 0, 1280, 720),
		image.YCbCrSubsampleRatio420,
	)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = fb.SendFrame(testImg)

		_, release, _ := fb.Read()
		if release != nil {
			release()
		}
	}
}

func trackCountName(n int) string {
	switch n {
	case 1:
		return "1_track"
	case 4:
		return "4_tracks"
	case 14:
		return "14_tracks"
	default:
		return "tracks"
	}
}
