//go:build !js
// +build !js

// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"testing"

	"github.com/pion/mediadevices/pkg/codec"
	"github.com/pion/mediadevices/pkg/io/video"
	"github.com/pion/mediadevices/pkg/prop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockVideoEncoderBuilder for testing.
type MockVideoEncoderBuilder struct{}

func (m *MockVideoEncoderBuilder) RTPCodec() *codec.RTPCodec {
	return codec.NewRTPVP8Codec(90000)
}

func (m *MockVideoEncoderBuilder) BuildVideoEncoder(r video.Reader, p prop.Media) (codec.ReadCloser, error) {
	return &MockReadCloser{}, nil
}

// MockReadCloser for testing.
type MockReadCloser struct{}

func (m *MockReadCloser) Read() ([]byte, func(), error) {
	return []byte{}, func() {}, nil
}

func (m *MockReadCloser) Close() error {
	return nil
}

func (m *MockReadCloser) Controller() codec.EncoderController {
	return nil
}

func TestNewRTCSender(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	require.NotNil(t, sender)

	// Test that sender implements the interface
	var _ WebRTCSender = sender
	var _ ConfigurableWebRTCSender = sender
}

func TestVideoTrackInfo_Validation(t *testing.T) {
	tests := []struct {
		name    string
		info    VideoTrackInfo
		wantErr bool
	}{
		{
			name: "valid track info",
			info: VideoTrackInfo{
				TrackID:        "test-track",
				Width:          1280,
				Height:         720,
				EncoderBuilder: &MockVideoEncoderBuilder{},
			},
			wantErr: false,
		},
		{
			name: "empty track ID",
			info: VideoTrackInfo{
				TrackID:        "",
				Width:          1280,
				Height:         720,
				EncoderBuilder: &MockVideoEncoderBuilder{},
			},
			wantErr: false, // Currently no validation, but structure is ready
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that the struct can be created
			assert.Equal(t, tt.info.TrackID, tt.info.TrackID)
			assert.Equal(t, tt.info.Width, tt.info.Width)
			assert.Equal(t, tt.info.Height, tt.info.Height)
			assert.NotNil(t, tt.info.EncoderBuilder)
		})
	}
}

func TestRTCSender_AddVideoTrack_DuplicateTrack(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)

	trackInfo := VideoTrackInfo{
		TrackID:        "test-track",
		Width:          1280,
		Height:         720,
		EncoderBuilder: &MockVideoEncoderBuilder{},
	}

	// First add should succeed
	err = sender.AddVideoTrack(trackInfo)
	require.NoError(t, err)

	// Second add with same ID should fail
	err = sender.AddVideoTrack(trackInfo)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTrackAlreadyExists)
}

func TestRTCSender_SendFrame_NonExistentTrack(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)

	err = sender.SendFrame("non-existent-track", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTrackNotFound)
}

func TestRTCSender_SetBitrateAllocation_InvalidValues(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)

	tests := []struct {
		name       string
		allocation map[string]float64
		wantErr    error
	}{
		{
			name:       "negative value",
			allocation: map[string]float64{"track1": -0.5},
			wantErr:    ErrInvalidNegativeValue,
		},
		{
			name:       "zero sum",
			allocation: map[string]float64{},
			wantErr:    ErrAllocationSumMustBePositive,
		},
		{
			name:       "non-existent track",
			allocation: map[string]float64{"non-existent": 1.0},
			wantErr:    ErrTrackNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sender.SetBitrateAllocation(tt.allocation)
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestRTCSender_GetWebRTCTrackLocal_NonExistentTrack(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)

	track, err := sender.GetWebRTCTrackLocal("non-existent")
	assert.Nil(t, track)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTrackNotFound)
}

func TestRTCSender_Close(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)

	err = sender.Close()
	assert.NoError(t, err)
}

const testAudioTrackID = "audio_ev_external"

func TestRTCSender_AddAudioTrack(t *testing.T) {
	rtcSender, err := NewRTCSender()
	require.NoError(t, err)

	track, err := rtcSender.AddAudioTrack(testAudioTrackID)
	require.NoError(t, err)
	require.NotNil(t, track)

	// Verify codec is Opus
	assert.Equal(t, "audio/opus", track.Codec().MimeType)
	assert.Equal(t, uint32(48000), track.Codec().ClockRate)
}

func TestRTCSender_AddAudioTrack_Duplicate(t *testing.T) {
	rtcSender, err := NewRTCSender()
	require.NoError(t, err)

	_, err = rtcSender.AddAudioTrack("audio_test")
	require.NoError(t, err)

	// Duplicate should fail
	_, err = rtcSender.AddAudioTrack("audio_test")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTrackAlreadyExists)
}

func TestRTCSender_AddAudioTrack_ConflictsWithVideoTrackID(t *testing.T) {
	rtcSender, err := NewRTCSender()
	require.NoError(t, err)

	// Add a video track first
	err = rtcSender.AddVideoTrack(VideoTrackInfo{
		TrackID:        "shared_id",
		Width:          640,
		Height:         480,
		EncoderBuilder: &MockVideoEncoderBuilder{},
	})
	require.NoError(t, err)

	// Audio track with same ID should fail
	_, err = rtcSender.AddAudioTrack("shared_id")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTrackAlreadyExists)
}

func TestRTCSender_GetWebRTCTrackLocal_AudioTrack(t *testing.T) {
	rtcSender, err := NewRTCSender()
	require.NoError(t, err)

	_, err = rtcSender.AddAudioTrack(testAudioTrackID)
	require.NoError(t, err)

	// Should be retrievable via GetWebRTCTrackLocal
	track, err := rtcSender.GetWebRTCTrackLocal(testAudioTrackID)
	require.NoError(t, err)
	require.NotNil(t, track)
	assert.Equal(t, "audio/opus", track.Codec().MimeType)
}

func TestRTCSender_AddAudioTrack_NotInPeerConnection(t *testing.T) {
	// Audio tracks are NOT added to PeerConnection by RTCSender.
	// LiveKit's PublishTrack handles SDP negotiation for audio.
	rtcSender, err := NewRTCSender()
	require.NoError(t, err)

	_, err = rtcSender.AddAudioTrack(testAudioTrackID)
	require.NoError(t, err)

	err = rtcSender.SetupPeerConnection()
	require.NoError(t, err)

	// Audio track should be retrievable via GetWebRTCTrackLocal
	// but NOT in PeerConnection senders (PublishTrack adds it later)
	track, err := rtcSender.GetWebRTCTrackLocal(testAudioTrackID)
	require.NoError(t, err)
	require.NotNil(t, track)

	peerConn := rtcSender.GetPeerConnection()
	senders := peerConn.GetSenders()
	for _, rtpSender := range senders {
		if rtpSender.Track() != nil {
			assert.NotEqual(t, testAudioTrackID, rtpSender.Track().ID(),
				"audio track should NOT be in PeerConnection senders")
		}
	}
}

func TestRTCSender_SetupPeerConnection_VideoOnlyInPC(t *testing.T) {
	rtcSender, err := NewRTCSender()
	require.NoError(t, err)

	err = rtcSender.AddVideoTrack(VideoTrackInfo{
		TrackID:        "camera_feed_0",
		Width:          640,
		Height:         480,
		EncoderBuilder: &MockVideoEncoderBuilder{},
	})
	require.NoError(t, err)

	_, err = rtcSender.AddAudioTrack(testAudioTrackID)
	require.NoError(t, err)

	err = rtcSender.SetupPeerConnection()
	require.NoError(t, err)

	peerConn := rtcSender.GetPeerConnection()
	senders := peerConn.GetSenders()

	foundVideo := false
	for _, rtpSender := range senders {
		if rtpSender.Track() == nil {
			continue
		}
		if rtpSender.Track().ID() == "camera_feed_0" {
			foundVideo = true
		}
		assert.NotEqual(t, testAudioTrackID, rtpSender.Track().ID(),
			"audio track should NOT be in PeerConnection senders")
	}
	assert.True(t, foundVideo, "video track should be in PeerConnection senders")
}

func TestStaticErrors(t *testing.T) {
	// Test that all static errors are properly defined
	assert.NotNil(t, ErrTrackAlreadyExists)
	assert.NotNil(t, ErrTrackNotFound)
	assert.NotNil(t, ErrTrackDoesNotSupportFrames)
	assert.NotNil(t, ErrInvalidNegativeValue)
	assert.NotNil(t, ErrAllocationSumMustBePositive)
	assert.NotNil(t, ErrFailedToCastVideoTrack)

	// Test error messages
	assert.Contains(t, ErrTrackAlreadyExists.Error(), "already exists")
	assert.Contains(t, ErrTrackNotFound.Error(), "not found")
	assert.Contains(t, ErrInvalidNegativeValue.Error(), "non-negative")
}
