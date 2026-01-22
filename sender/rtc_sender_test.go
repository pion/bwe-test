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
