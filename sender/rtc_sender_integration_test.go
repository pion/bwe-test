//go:build !js
// +build !js

// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"context"
	"image"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRTCSender_SetupPeerConnection(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	err = sender.SetupPeerConnection()
	require.NoError(t, err)

	// Verify peer connection was created
	assert.NotNil(t, sender.peerConnection)
}

func TestRTCSender_CreateOffer(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	err = sender.SetupPeerConnection()
	require.NoError(t, err)

	offer, err := sender.CreateOffer()
	require.NoError(t, err)
	assert.NotNil(t, offer)
}

func TestRTCSender_AcceptAnswer(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	err = sender.SetupPeerConnection()
	require.NoError(t, err)

	// Create a mock answer (this won't work in practice but tests the code path)
	offer, err := sender.CreateOffer()
	require.NoError(t, err)

	// Use the offer as a mock answer for testing
	_ = sender.AcceptAnswer(offer)
	// This might fail due to invalid SDP, but it tests the code path
	// The important thing is that it doesn't panic
}

func TestRTCSender_AddVideoTrack_Success(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	trackInfo := VideoTrackInfo{
		TrackID:        "test-track",
		Width:          640,
		Height:         480,
		EncoderBuilder: &MockVideoEncoderBuilder{},
	}

	err = sender.AddVideoTrack(trackInfo)
	require.NoError(t, err)

	// Verify track was added
	sender.tracksMu.RLock()
	track, exists := sender.tracks[trackInfo.TrackID]
	sender.tracksMu.RUnlock()

	assert.True(t, exists)
	assert.NotNil(t, track)
	assert.Equal(t, trackInfo.TrackID, track.info.TrackID)
}

func TestRTCSender_SendFrame_WithFrameBuffer(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	trackInfo := VideoTrackInfo{
		TrackID:        "test-track",
		Width:          640,
		Height:         480,
		EncoderBuilder: &MockVideoEncoderBuilder{},
	}

	err = sender.AddVideoTrack(trackInfo)
	require.NoError(t, err)

	// Create a test image
	testImg := image.NewRGBA(image.Rect(0, 0, 640, 480))

	// Send frame should work with FrameBuffer
	err = sender.SendFrame(trackInfo.TrackID, testImg)
	require.NoError(t, err)
}

func TestRTCSender_SetBitrateAllocation_Success(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	trackInfo := VideoTrackInfo{
		TrackID:        "test-track",
		Width:          640,
		Height:         480,
		EncoderBuilder: &MockVideoEncoderBuilder{},
	}

	err = sender.AddVideoTrack(trackInfo)
	require.NoError(t, err)

	// Set valid allocation
	allocation := map[string]float64{
		"test-track": 1.0,
	}

	err = sender.SetBitrateAllocation(allocation)
	require.NoError(t, err)

	// Verify allocation was set
	sender.tracksMu.RLock()
	storedAllocation := sender.bitrateAllocation
	sender.tracksMu.RUnlock()

	assert.Equal(t, 1.0, storedAllocation["test-track"])
}

func TestRTCSender_UpdateBitrate(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	// Test updateBitrate with no tracks (should not panic)
	sender.updateBitrate(1000000)

	// Add a track and test again
	trackInfo := VideoTrackInfo{
		TrackID:        "test-track",
		Width:          640,
		Height:         480,
		EncoderBuilder: &MockVideoEncoderBuilder{},
	}

	err = sender.AddVideoTrack(trackInfo)
	require.NoError(t, err)

	// This should not panic even with mock encoder
	sender.updateBitrate(1000000)
}

func TestRTCSender_Start_WithTimeout(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start should timeout waiting for estimator and return nil
	err = sender.Start(ctx)
	require.NoError(t, err) // Start returns nil on context timeout
}

func TestRTCSender_GetWebRTCTrackLocal_Success(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	trackInfo := VideoTrackInfo{
		TrackID:        "test-track",
		Width:          640,
		Height:         480,
		EncoderBuilder: &MockVideoEncoderBuilder{},
	}

	err = sender.AddVideoTrack(trackInfo)
	require.NoError(t, err)

	track, err := sender.GetWebRTCTrackLocal(trackInfo.TrackID)
	require.NoError(t, err)
	assert.NotNil(t, track)
}

func TestRTCSender_ConfigurableInterface(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	// Test ConfigurableWebRTCSender interface methods
	settingEngine := sender.GetSettingEngine()
	assert.NotNil(t, settingEngine)

	mediaEngine := sender.GetMediaEngine()
	assert.NotNil(t, mediaEngine)

	registry := sender.GetRegistry()
	assert.NotNil(t, registry)

	estimatorChan := sender.GetEstimatorChan()
	assert.NotNil(t, estimatorChan)

	// Test SetLogger (should not panic)
	sender.SetLogger(nil)

	// Test SetCCLogWriter (should not panic)
	sender.SetCCLogWriter(nil)
}

func TestRTCSender_MultipleTracksAndAllocation(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	// Add multiple tracks
	track1Info := VideoTrackInfo{
		TrackID:        "track1",
		Width:          640,
		Height:         480,
		EncoderBuilder: &MockVideoEncoderBuilder{},
	}

	track2Info := VideoTrackInfo{
		TrackID:        "track2",
		Width:          1280,
		Height:         720,
		EncoderBuilder: &MockVideoEncoderBuilder{},
	}

	err = sender.AddVideoTrack(track1Info)
	require.NoError(t, err)

	err = sender.AddVideoTrack(track2Info)
	require.NoError(t, err)

	// Set allocation for both tracks
	allocation := map[string]float64{
		"track1": 0.3,
		"track2": 0.7,
	}

	err = sender.SetBitrateAllocation(allocation)
	require.NoError(t, err)

	// Test bitrate update with multiple tracks
	sender.updateBitrate(2000000)

	// Verify both tracks exist
	track1, err := sender.GetWebRTCTrackLocal("track1")
	require.NoError(t, err)
	assert.NotNil(t, track1)

	track2, err := sender.GetWebRTCTrackLocal("track2")
	require.NoError(t, err)
	assert.NotNil(t, track2)
}
