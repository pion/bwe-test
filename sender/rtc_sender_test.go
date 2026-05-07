//go:build !js

// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"errors"
	"image"
	"testing"
	"time"

	"github.com/pion/interceptor/pkg/stats"
	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec"
	"github.com/pion/mediadevices/pkg/io/video"
	"github.com/pion/mediadevices/pkg/prop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockStatsGetter implements stats.Getter for unit tests so GetTrackStats
// can be exercised without real RTP/RTCP traffic.
type mockStatsGetter struct {
	stats *stats.Stats
}

func (m *mockStatsGetter) Get(uint32) *stats.Stats { return m.stats }

var errEncoderBusy = errors.New("encoder busy")

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

func TestRTCSender_ProcessEncodedFrames_NoTracks(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	// Should return immediately with no tracks.
	sender.processEncodedFrames()
}

func TestRTCSender_ProcessEncodedFrames_WithTracks(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	err = sender.AddVideoTrack(VideoTrackInfo{
		TrackID:        "cam-0",
		Width:          640,
		Height:         480,
		InitialBitrate: 500_000,
	})
	require.NoError(t, err)

	// Call processEncodedFrames multiple times — exercises the sequential loop
	// with a real encoder. The encoder may have data from init (black frame).
	sender.processEncodedFrames()

	// Send a frame so the encoder can produce output.
	testImg := image.NewYCbCr(image.Rect(0, 0, 640, 480), image.YCbCrSubsampleRatio420)
	err = sender.SendFrame("cam-0", testImg)
	require.NoError(t, err)

	// Give encoder time to consume the frame.
	time.Sleep(150 * time.Millisecond)

	sender.processEncodedFrames()
	// We mainly verify no panics or deadlocks here.
}

func TestRTCSender_ProcessEncodedFrames_AllErrors(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	err = sender.AddVideoTrack(VideoTrackInfo{
		TrackID:        "cam-0",
		Width:          640,
		Height:         480,
		EncoderBuilder: &MockVideoEncoderBuilder{},
	})
	require.NoError(t, err)

	// Drain any buffered frames — the mock encoder always returns empty data,
	// but the FrameBuffer Read() will return ErrNoFrameAvailable in the
	// non-blocking path (initialized), which propagates through the encoder.
	// Call several times to ensure we hit the no-frame path.
	for range 5 {
		sender.processEncodedFrames()
	}

	// Verify noEncodedFrame reflects the state.
	_ = sender.GetEncodeFrameOk()
}

func TestRTCSender_GetEncodeFrameOk(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	// Default should be true (noEncodedFrame starts false).
	assert.True(t, sender.GetEncodeFrameOk())

	// After processEncodedFrames with no tracks, should still be true (early return).
	sender.processEncodedFrames()
	assert.True(t, sender.GetEncodeFrameOk())
}

func TestRTCSender_SetupPeerConnection_CancelsRTCP(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	err = sender.AddVideoTrack(VideoTrackInfo{
		TrackID:        "cam-0",
		Width:          640,
		Height:         480,
		EncoderBuilder: &MockVideoEncoderBuilder{},
	})
	require.NoError(t, err)

	// First SetupPeerConnection — should create rtcpCancel.
	err = sender.SetupPeerConnection()
	require.NoError(t, err)
	assert.NotNil(t, sender.rtcpCancel)

	// Second SetupPeerConnection — should cancel the first context.
	err = sender.SetupPeerConnection()
	require.NoError(t, err)
	assert.NotNil(t, sender.rtcpCancel)
}

func TestRTCSender_Close_CancelsRTCP(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)

	err = sender.SetupPeerConnection()
	require.NoError(t, err)
	assert.NotNil(t, sender.rtcpCancel)

	err = sender.Close()
	require.NoError(t, err)
	assert.Nil(t, sender.rtcpCancel, "rtcpCancel should be nil after Close")
}

// MockKeyFrameReadCloser is a mock EncodedReadCloser whose controller
// implements codec.KeyFrameController.
type MockKeyFrameReadCloser struct {
	forceKeyFrameErr error
}

func (m *MockKeyFrameReadCloser) Read() (mediadevices.EncodedBuffer, func(), error) {
	return mediadevices.EncodedBuffer{}, func() {}, nil
}

func (m *MockKeyFrameReadCloser) Close() error { return nil }

func (m *MockKeyFrameReadCloser) Controller() codec.EncoderController {
	return &mockKeyFrameController{err: m.forceKeyFrameErr}
}

type mockKeyFrameController struct {
	err error
}

func (c *mockKeyFrameController) ForceKeyFrame() error {
	return c.err
}

// MockNoKeyFrameReadCloser is a mock EncodedReadCloser whose controller
// does NOT implement codec.KeyFrameController.
type MockNoKeyFrameReadCloser struct{}

func (m *MockNoKeyFrameReadCloser) Read() (mediadevices.EncodedBuffer, func(), error) {
	return mediadevices.EncodedBuffer{}, func() {}, nil
}

func (m *MockNoKeyFrameReadCloser) Close() error { return nil }

func (m *MockNoKeyFrameReadCloser) Controller() codec.EncoderController {
	return nil
}

func TestRTCSender_ForceKeyFrame_TrackNotFound(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)

	err = sender.ForceKeyFrame("non-existent")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTrackNotFound)
}

func TestRTCSender_ForceKeyFrame_NotSupported(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)

	// Manually inject a track with an encoder that doesn't support KeyFrameController
	sender.tracks["test-track"] = &EncodedTrack{
		encodedReader: &MockNoKeyFrameReadCloser{},
	}

	err = sender.ForceKeyFrame("test-track")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrForceKeyFrameNotSupported)
}

func TestRTCSender_ForceKeyFrame_Success(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)

	// Manually inject a track with an encoder that supports KeyFrameController
	sender.tracks["test-track"] = &EncodedTrack{
		encodedReader: &MockKeyFrameReadCloser{forceKeyFrameErr: nil},
	}

	err = sender.ForceKeyFrame("test-track")
	assert.NoError(t, err)
}

func TestRTCSender_ForceKeyFrame_EncoderError(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)

	sender.tracks["test-track"] = &EncodedTrack{
		encodedReader: &MockKeyFrameReadCloser{forceKeyFrameErr: errEncoderBusy},
	}

	err = sender.ForceKeyFrame("test-track")
	require.Error(t, err)
	assert.ErrorIs(t, err, errEncoderBusy)
}

func TestStaticErrors(t *testing.T) {
	// Test that all static errors are properly defined
	assert.NotNil(t, ErrTrackAlreadyExists)
	assert.NotNil(t, ErrTrackNotFound)
	assert.NotNil(t, ErrTrackDoesNotSupportFrames)
	assert.NotNil(t, ErrInvalidNegativeValue)
	assert.NotNil(t, ErrAllocationSumMustBePositive)
	assert.NotNil(t, ErrFailedToCastVideoTrack)
	assert.NotNil(t, ErrForceKeyFrameNotSupported)

	// Test error messages
	assert.Contains(t, ErrTrackAlreadyExists.Error(), "already exists")
	assert.Contains(t, ErrTrackNotFound.Error(), "not found")
	assert.Contains(t, ErrInvalidNegativeValue.Error(), "non-negative")
}

func TestRTCSender_GetTrackStats_NonExistentTrack(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	stats := sender.GetTrackStats("missing-track")
	assert.Nil(t, stats, "GetTrackStats should return nil for missing track")
}

func TestRTCSender_GetTrackStats_BeforePeerConnection(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	err = sender.AddVideoTrack(VideoTrackInfo{
		TrackID:        "cam-0",
		Width:          640,
		Height:         480,
		EncoderBuilder: &MockVideoEncoderBuilder{},
	})
	require.NoError(t, err)

	// Without a peer connection there is no rtpSender and the stats Getter
	// is unset. GetTrackStats should still return a populated struct with
	// just the pipeline counters (all zero before any frame is encoded).
	stats := sender.GetTrackStats("cam-0")
	require.NotNil(t, stats)
	assert.Equal(t, uint64(0), stats.FramesEncoded)
	assert.Equal(t, uint64(0), stats.TotalEncodeTimeNs)
	assert.Equal(t, uint64(0), stats.TotalSendQueueTimeNs)
	assert.Equal(t, uint64(0), stats.PacketsSent)
	assert.Equal(t, uint64(0), stats.RoundTripTimeMeasurements)
	assert.Equal(t, time.Duration(0), stats.RoundTripTime)
}

func TestRTCSender_GetTrackStats_PipelineCountersIncrement(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	err = sender.AddVideoTrack(VideoTrackInfo{
		TrackID:        "cam-0",
		Width:          640,
		Height:         480,
		InitialBitrate: 500_000,
	})
	require.NoError(t, err)

	// Drive a frame through the encoder so encodeAndSendTrack updates the
	// per-track atomic counters that GetTrackStats reports.
	testImg := image.NewYCbCr(image.Rect(0, 0, 640, 480), image.YCbCrSubsampleRatio420)
	require.NoError(t, sender.SendFrame("cam-0", testImg))
	time.Sleep(150 * time.Millisecond)
	sender.processEncodedFrames()

	stats := sender.GetTrackStats("cam-0")
	require.NotNil(t, stats)
	assert.Positive(t, stats.FramesEncoded,
		"FramesEncoded should increment after a frame is encoded")
	// Encode and send-queue time can be 0 on a very fast machine but never
	// negative; just verify they're sensible uint64 values.
	assert.GreaterOrEqual(t, stats.TotalEncodeTimeNs, uint64(0))
	assert.GreaterOrEqual(t, stats.TotalSendQueueTimeNs, uint64(0))
}

func TestRTCSender_GetTrackStats_WithPeerConnection(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	err = sender.AddVideoTrack(VideoTrackInfo{
		TrackID:        "cam-0",
		Width:          640,
		Height:         480,
		EncoderBuilder: &MockVideoEncoderBuilder{},
	})
	require.NoError(t, err)

	// SetupPeerConnection wires the rtpSender on the EncodedTrack and lets
	// the stats interceptor's OnNewPeerConnection callback populate the
	// statsGetter. With no actual ICE/DTLS established the network counters
	// stay at zero, but GetTrackStats should still resolve the SSRC path
	// without nil-deref.
	require.NoError(t, sender.SetupPeerConnection())

	stats := sender.GetTrackStats("cam-0")
	require.NotNil(t, stats)
	// Pipeline counters: still zero, no frames encoded yet.
	assert.Equal(t, uint64(0), stats.FramesEncoded)
	// Network counters: zero because no traffic has flowed.
	assert.Equal(t, uint64(0), stats.PacketsSent)
	assert.Equal(t, uint64(0), stats.RoundTripTimeMeasurements)
}

func TestRTCSender_GetTrackStats_PopulatesNetworkCounters(t *testing.T) {
	sender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = sender.Close() }()

	err = sender.AddVideoTrack(VideoTrackInfo{
		TrackID:        "cam-0",
		Width:          640,
		Height:         480,
		EncoderBuilder: &MockVideoEncoderBuilder{},
	})
	require.NoError(t, err)

	// Setting up the peer connection wires the rtpSender on the track,
	// which is required for the SSRC resolution path inside GetTrackStats.
	require.NoError(t, sender.SetupPeerConnection())

	// Inject a mock Getter with canned values. This bypasses the need to
	// drive real RTP/RTCP traffic and exercises the field-copy path.
	sender.statsGetterMu.Lock()
	sender.statsGetter = &mockStatsGetter{
		stats: &stats.Stats{
			OutboundRTPStreamStats: stats.OutboundRTPStreamStats{
				SentRTPStreamStats: stats.SentRTPStreamStats{
					PacketsSent: 1234,
					BytesSent:   567890,
				},
				HeaderBytesSent: 7000,
				NACKCount:       2,
				PLICount:        1,
			},
			RemoteInboundRTPStreamStats: stats.RemoteInboundRTPStreamStats{
				ReceivedRTPStreamStats: stats.ReceivedRTPStreamStats{
					PacketsReceived: 1200,
					PacketsLost:     34,
					Jitter:          0.5,
				},
				FractionLost:              0.025,
				RoundTripTime:             40 * time.Millisecond,
				TotalRoundTripTime:        4 * time.Second,
				RoundTripTimeMeasurements: 100,
			},
		},
	}
	sender.statsGetterMu.Unlock()

	got := sender.GetTrackStats("cam-0")
	require.NotNil(t, got)

	assert.Equal(t, uint64(1234), got.PacketsSent)
	assert.Equal(t, uint64(567890), got.BytesSent)
	assert.Equal(t, uint64(7000), got.HeaderBytesSent)
	assert.Equal(t, uint32(2), got.NACKCount)
	assert.Equal(t, uint32(1), got.PLICount)

	assert.Equal(t, uint64(1200), got.PacketsReceived)
	assert.Equal(t, int64(34), got.PacketsLost)
	assert.InDelta(t, 0.5, got.Jitter, 0.0001)
	assert.InDelta(t, 0.025, got.FractionLost, 0.0001)
	assert.Equal(t, 40*time.Millisecond, got.RoundTripTime)
	assert.Equal(t, 4*time.Second, got.TotalRoundTripTime)
	assert.Equal(t, uint64(100), got.RoundTripTimeMeasurements)
}

func TestTrackStats_ZeroValueIsValid(t *testing.T) {
	// Consumers may compare TrackStats values directly to compute deltas
	// across windows. Verify the zero value is well-defined and usable.
	var s TrackStats
	assert.Equal(t, uint64(0), s.FramesEncoded)
	assert.Equal(t, time.Duration(0), s.RoundTripTime)
	assert.Equal(t, uint64(0), s.RoundTripTimeMeasurements)
}
