//go:build !js

// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/mediadevices/pkg/codec/opus"
	"github.com/pion/mediadevices/pkg/wave"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	audioSampleRate = 48000
	audioChannels   = 2
	// 20ms of 48kHz audio = 960 samples per channel.
	audioFrameSamplesPerChannel = audioSampleRate / 50
)

// pcm20ms returns 20ms of interleaved 16-bit PCM (silence) for the test format.
func pcm20ms() []int16 {
	return make([]int16, audioFrameSamplesPerChannel*audioChannels)
}

func TestNewAudioBuffer(t *testing.T) {
	ab := NewAudioBuffer(audioSampleRate, audioChannels)
	require.NotNil(t, ab)
	assert.Equal(t, "audio-buffer", ab.ID())

	// Must satisfy mediadevices.AudioSource (audio.Reader + Source).
	var _ interface {
		ID() string
		Close() error
		Read() (wave.Audio, func(), error)
	} = ab
}

func TestAudioBuffer_PushAndRead(t *testing.T) {
	ab := NewAudioBuffer(audioSampleRate, audioChannels)
	defer func() { _ = ab.Close() }()

	require.NoError(t, ab.PushPCM(pcm20ms(), audioSampleRate, audioChannels))

	ab.SetInitialized()

	chunk, release, err := ab.Read()
	require.NoError(t, err)
	require.NotNil(t, chunk)
	require.NotNil(t, release)

	info := chunk.ChunkInfo()
	assert.Equal(t, audioFrameSamplesPerChannel, info.Len)
	assert.Equal(t, audioChannels, info.Channels)
	assert.Equal(t, audioSampleRate, info.SamplingRate)
	release()
}

func TestAudioBuffer_CaptureTSRoundTrip(t *testing.T) {
	ab := NewAudioBuffer(audioSampleRate, audioChannels)
	defer func() { _ = ab.Close() }()

	const captureUs = int64(1_751_000_000_000_000)
	require.NoError(t, ab.PushPCMWithCaptureTS(pcm20ms(), audioSampleRate, audioChannels, captureUs))

	ab.SetInitialized()

	_, release, err := ab.Read()
	require.NoError(t, err)
	release()

	assert.Equal(t, captureUs, ab.LastCaptureTSUs())
	assert.Positive(t, ab.LastDequeueWallUs())
}

func TestAudioBuffer_PushPCMZeroCapture(t *testing.T) {
	ab := NewAudioBuffer(audioSampleRate, audioChannels)
	defer func() { _ = ab.Close() }()

	require.NoError(t, ab.PushPCM(pcm20ms(), audioSampleRate, audioChannels))
	ab.SetInitialized()

	_, release, err := ab.Read()
	require.NoError(t, err)
	release()

	assert.Zero(t, ab.LastCaptureTSUs())
}

func TestAudioBuffer_SilenceDoesNotClobberCapture(t *testing.T) {
	ab := NewAudioBuffer(audioSampleRate, audioChannels)
	defer func() { _ = ab.Close() }()

	// Read a real stamped chunk first.
	const captureUs = int64(1_751_000_000_000_000)
	require.NoError(t, ab.PushPCMWithCaptureTS(pcm20ms(), audioSampleRate, audioChannels, captureUs))
	ab.SetInitialized()
	_, release, err := ab.Read()
	require.NoError(t, err)
	release()
	require.Equal(t, captureUs, ab.LastCaptureTSUs())

	// A silence-timeout read (uninitialized) must not overwrite the last stamp.
	ab.initialized = false
	_, release2, err := ab.Read()
	require.NoError(t, err)
	release2()

	assert.Equal(t, captureUs, ab.LastCaptureTSUs())
}

func TestAudioBuffer_FrameReadySignalOnPush(t *testing.T) {
	ab := NewAudioBuffer(audioSampleRate, audioChannels)
	defer func() { _ = ab.Close() }()

	require.NoError(t, ab.PushPCM(pcm20ms(), audioSampleRate, audioChannels))

	signaled := false
	select {
	case <-ab.FrameReady():
		signaled = true
	default:
	}
	assert.True(t, signaled, "expected FrameReady signal after PushPCM")
}

func TestAudioBuffer_ReadTimeoutReturnsSilence(t *testing.T) {
	ab := NewAudioBuffer(audioSampleRate, audioChannels)
	defer func() { _ = ab.Close() }()

	// Not initialized: the blocking path returns a silence chunk on timeout.
	chunk, release, err := ab.Read()
	require.NoError(t, err)
	require.NotNil(t, chunk)

	info := chunk.ChunkInfo()
	assert.Equal(t, audioChannels, info.Channels)
	assert.Equal(t, audioSampleRate, info.SamplingRate)
	assert.Positive(t, info.Len)
	release()
}

func TestAudioBuffer_ReadInitializedWithoutChunk(t *testing.T) {
	ab := NewAudioBuffer(audioSampleRate, audioChannels)
	defer func() { _ = ab.Close() }()

	ab.SetInitialized()

	// Non-blocking fast path: no chunk available -> ErrNoFrameAvailable.
	chunk, release, err := ab.Read()
	assert.Nil(t, chunk)
	assert.NotNil(t, release)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoFrameAvailable)
}

func TestAudioBuffer_ReadAfterClose(t *testing.T) {
	ab := NewAudioBuffer(audioSampleRate, audioChannels)
	require.NoError(t, ab.Close())

	chunk, release, err := ab.Read()
	assert.Nil(t, chunk)
	assert.NotNil(t, release)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBufferClosed)
}

func TestAudioBuffer_PushAfterClose(t *testing.T) {
	ab := NewAudioBuffer(audioSampleRate, audioChannels)
	require.NoError(t, ab.Close())

	err := ab.PushPCM(pcm20ms(), audioSampleRate, audioChannels)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBufferClosed)
}

func TestAudioBuffer_PushInvalidChunk(t *testing.T) {
	ab := NewAudioBuffer(audioSampleRate, audioChannels)
	defer func() { _ = ab.Close() }()

	// Odd length is not a multiple of the 2 channel count.
	err := ab.PushPCM(make([]int16, 3), audioChannels, audioChannels)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidPCMChunk)

	// Zero channels is invalid.
	err = ab.PushPCM(pcm20ms(), audioSampleRate, 0)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidPCMChunk)
}

func TestAudioBuffer_DropsOldestWhenFull(t *testing.T) {
	ab := NewAudioBuffer(audioSampleRate, audioChannels)
	defer func() { _ = ab.Close() }()

	// Push more than capacity (8): drop-oldest, never errors.
	for range 20 {
		require.NoError(t, ab.PushPCM(pcm20ms(), audioSampleRate, audioChannels))
	}

	ab.SetInitialized()

	// At most 8 chunks should be drainable.
	drained := 0
	for {
		_, release, err := ab.Read()
		if err != nil {
			break
		}
		release()
		drained++
	}
	assert.LessOrEqual(t, drained, 8)
	assert.Positive(t, drained)
}

func TestRTCSender_AddEncodedAudioTrack(t *testing.T) {
	rtcSender, err := NewRTCSender()
	require.NoError(t, err)

	params, err := opus.NewParams()
	require.NoError(t, err)

	require.NoError(t, rtcSender.AddEncodedAudioTrack(testAudioTrackID, params))

	// Retrievable via GetWebRTCTrackLocal and negotiated as Opus.
	track, err := rtcSender.GetWebRTCTrackLocal(testAudioTrackID)
	require.NoError(t, err)
	require.NotNil(t, track)
	assert.Equal(t, "audio/opus", track.Codec().MimeType)
	assert.Equal(t, uint32(48000), track.Codec().ClockRate)
}

func TestRTCSender_AddEncodedAudioTrack_Duplicate(t *testing.T) {
	rtcSender, err := NewRTCSender()
	require.NoError(t, err)

	params, err := opus.NewParams()
	require.NoError(t, err)

	require.NoError(t, rtcSender.AddEncodedAudioTrack(testAudioTrackID, params))

	err = rtcSender.AddEncodedAudioTrack(testAudioTrackID, params)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTrackAlreadyExists)
}

func TestRTCSender_SendAudioFrame_NonExistentTrack(t *testing.T) {
	rtcSender, err := NewRTCSender()
	require.NoError(t, err)

	err = rtcSender.SendAudioFrameWithCaptureTS("missing", pcm20ms(), audioSampleRate, audioChannels, 0)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTrackNotFound)
}

func TestRTCSender_SendAudioFrame_RejectsVideoTrack(t *testing.T) {
	rtcSender, err := NewRTCSender()
	require.NoError(t, err)

	require.NoError(t, rtcSender.AddVideoTrack(VideoTrackInfo{
		TrackID:        "cam-0",
		Width:          640,
		Height:         480,
		EncoderBuilder: &MockVideoEncoderBuilder{},
	}))

	err = rtcSender.SendAudioFrameWithCaptureTS("cam-0", pcm20ms(), audioSampleRate, audioChannels, 0)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTrackDoesNotSupportFrames)
}

// TestRTCSender_EncodedAudio_ProducesOpusFrames drives the full PCM -> Opus
// path: push raw PCM and assert the always-running per-track encode loop emits
// Opus frames (observed via the OnFrameSent callback, since the loop owns the
// encoded reader).
func TestRTCSender_EncodedAudio_ProducesOpusFrames(t *testing.T) {
	rtcSender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = rtcSender.Close() }()

	params, err := opus.NewParams()
	require.NoError(t, err)
	require.NoError(t, rtcSender.AddEncodedAudioTrack(testAudioTrackID, params))

	rtcSender.tracksMu.RLock()
	track := rtcSender.tracks[testAudioTrackID]
	rtcSender.tracksMu.RUnlock()
	require.NotNil(t, track)
	require.True(t, track.isAudio)

	var sent atomic.Bool
	rtcSender.SetOnFrameSent(func(_ string, _, _, _, _ int64, dropped bool) {
		if !dropped {
			sent.Store(true)
		}
	})

	// Feed PCM at ~20ms cadence and wait for the encode loop to emit a frame.
	deadline := time.Now().Add(2 * time.Second)
	for i := 0; i < 100 && !sent.Load() && time.Now().Before(deadline); i++ {
		require.NoError(t, rtcSender.SendAudioFrameWithCaptureTS(
			testAudioTrackID, pcm20ms(), audioSampleRate, audioChannels, 0))
		time.Sleep(10 * time.Millisecond)
	}

	assert.True(t, sent.Load(), "expected at least one encoded Opus frame via OnFrameSent")
}

// TestRTCSender_EncodedAudio_StampsCaptureTimestamp asserts a capture timestamp
// supplied via SendAudioFrameWithCaptureTS flows through the audio encode path
// and is echoed on the OnFrameSent success path (the same value the capture-
// timestamp interceptor encodes into the outgoing RTP timestamp).
func TestRTCSender_EncodedAudio_StampsCaptureTimestamp(t *testing.T) {
	rtcSender, err := NewRTCSender()
	require.NoError(t, err)
	defer func() { _ = rtcSender.Close() }()

	params, err := opus.NewParams()
	require.NoError(t, err)
	require.NoError(t, rtcSender.AddEncodedAudioTrack(testAudioTrackID, params))

	const wantCaptureTS int64 = 1_751_000_000_000_000
	var (
		mu  sync.Mutex
		got int64
	)
	rtcSender.SetOnFrameSent(func(_ string, captureTSUs, _, _, _ int64, dropped bool) {
		if dropped || captureTSUs != wantCaptureTS {
			return
		}
		mu.Lock()
		got = captureTSUs
		mu.Unlock()
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		require.NoError(t, rtcSender.SendAudioFrameWithCaptureTS(
			testAudioTrackID, pcm20ms(), audioSampleRate, audioChannels, wantCaptureTS))
		time.Sleep(10 * time.Millisecond)

		mu.Lock()
		found := got == wantCaptureTS
		mu.Unlock()
		if found {
			break
		}
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, wantCaptureTS, got, "captureTS never echoed through the audio encode path")
}

// TestRTCSender_EncodedAudio_BitrateDriven verifies the GCC encoder-update path
// reaches the Opus BitRateController for audio (clamped into the audio band).
func TestRTCSender_EncodedAudio_BitrateDriven(t *testing.T) {
	rtcSender, err := NewRTCSender()
	require.NoError(t, err)

	params, err := opus.NewParams()
	require.NoError(t, err)
	require.NoError(t, rtcSender.AddEncodedAudioTrack(testAudioTrackID, params))

	rtcSender.tracksMu.RLock()
	track := rtcSender.tracks[testAudioTrackID]
	rtcSender.tracksMu.RUnlock()
	require.NotNil(t, track)

	// A target well above the audio max should still be applied (clamped) and
	// return true (a compatible BitRateController was found).
	assert.True(t, updateEncoderBitrate(track, 0, 5_000_000))
	// A zero/low target is clamped up to the floor and still applied.
	assert.True(t, updateEncoderBitrate(track, 0, 1000))
}
