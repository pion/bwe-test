//go:build !js

// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/mediadevices/pkg/wave"
)

// silenceFrameDuration is the duration of the silence chunk returned during
// encoder initialization (matches the Opus Latency20ms framing).
const silenceFrameDuration = 20 * time.Millisecond

// getSilenceChunk returns a zero-filled (silent) interleaved PCM chunk carrying
// the declared sample rate / channel count. It is used during encoder
// initialization, where mediadevices reads exactly one chunk to detect the
// audio properties (sample rate, channel count) before encoding begins.
func getSilenceChunk(sampleRate, channels int) *wave.Int16Interleaved {
	samplesPerChannel := int(float64(sampleRate) * silenceFrameDuration.Seconds())

	return wave.NewInt16Interleaved(wave.ChunkInfo{
		Len:          samplesPerChannel,
		Channels:     channels,
		SamplingRate: sampleRate,
	})
}

// chunkWithMeta pairs a PCM chunk with an opaque capture timestamp
// (microseconds). Zero captureTSUs means no timestamp (legacy PushPCM or the
// init silence chunk). It is the audio counterpart of frameWithMeta.
type chunkWithMeta struct {
	chunk       *wave.Int16Interleaved
	captureTSUs int64
}

// AudioBuffer is a simple in-memory PCM buffer that implements
// mediadevices.AudioSource (audio.Reader + Source). It is the audio counterpart
// of FrameBuffer: callers push raw interleaved PCM via PushPCM and the
// mediadevices Opus encoder reads chunks from it, letting GCC drive the audio
// bitrate just like video.
type AudioBuffer struct {
	chunkChan         chan chunkWithMeta
	closeChan         chan struct{}
	notifyChan        chan struct{}
	closeOnce         sync.Once
	id                string
	initialized       bool
	sampleRate        int // declared format, used for the init silence fallback
	channels          int
	lastCaptureTSUs   atomic.Int64
	lastDequeueWallUs atomic.Int64
}

// NewAudioBuffer creates a new audio buffer for the given declared format.
// sampleRate/channels describe the PCM that will be pushed (e.g. 48000, 2) and
// are used to synthesize a silence chunk for encoder property detection.
func NewAudioBuffer(sampleRate, channels int) *AudioBuffer {
	return &AudioBuffer{
		chunkChan:  make(chan chunkWithMeta, 8), // ~160ms of 20ms chunks
		closeChan:  make(chan struct{}),
		notifyChan: make(chan struct{}, 1),
		id:         "audio-buffer",
		sampleRate: sampleRate,
		channels:   channels,
	}
}

// ID returns the identifier for this audio source.
func (a *AudioBuffer) ID() string {
	return a.id
}

// FrameReady returns a channel that receives a value when a chunk is enqueued.
// It is buffered (cap 1) and edge-triggered: signal() drops the notification if
// one is already pending, so a consumer that drains all available chunks after
// each wake-up never misses one. This lets the encode loop block for a chunk
// instead of polling. Mirrors FrameBuffer.FrameReady.
func (a *AudioBuffer) FrameReady() <-chan struct{} {
	return a.notifyChan
}

// Closed returns a channel closed when the buffer is closed, so a consumer
// blocked on FrameReady can wake and observe ErrBufferClosed.
func (a *AudioBuffer) Closed() <-chan struct{} {
	return a.closeChan
}

// signal performs a non-blocking notify that a chunk is available. If a
// notification is already pending it is a no-op (edge-triggered).
func (a *AudioBuffer) signal() {
	select {
	case a.notifyChan <- struct{}{}:
	default:
	}
}

// LastCaptureTSUs returns the capture timestamp (unix microseconds) of the most
// recently dequeued chunk, or 0 if none carried one. The init silence chunk and
// legacy PushPCM leave it unchanged / zero.
func (a *AudioBuffer) LastCaptureTSUs() int64 {
	return a.lastCaptureTSUs.Load()
}

// LastDequeueWallUs returns the wall-clock time (unix microseconds) at which the
// most recent real chunk was dequeued in Read.
func (a *AudioBuffer) LastDequeueWallUs() int64 {
	return a.lastDequeueWallUs.Load()
}

// Close stops the audio buffer and releases resources.
func (a *AudioBuffer) Close() error {
	a.closeOnce.Do(func() {
		close(a.closeChan)
	})

	return nil
}

// SetInitialized marks the audio buffer as initialized.
func (a *AudioBuffer) SetInitialized() {
	a.initialized = true
}

// Read returns the next available PCM chunk from the buffer.
// When initialized (normal operation), returns immediately with
// ErrNoFrameAvailable if no chunk is ready — this must never block because the
// encode loop reads under the sender's write lock. When not initialized
// (encoder init), blocks up to 100ms and returns a silence chunk so
// mediadevices can detect the audio properties.
func (a *AudioBuffer) Read() (wave.Audio, func(), error) {
	if a.initialized {
		// Check closeChan first so a closed buffer always reports ErrBufferClosed
		// instead of racing with the chunkChan/default branch below (Go's select
		// picks ready cases pseudo-randomly). Mirrors FrameBuffer.Read.
		select {
		case <-a.closeChan:
			return nil, func() {}, ErrBufferClosed
		default:
		}
		// Non-blocking fast path for normal operation.
		select {
		case cm := <-a.chunkChan:
			a.recordDequeue(cm)

			return cm.chunk, func() {}, nil
		default:
			return nil, func() {}, ErrNoFrameAvailable
		}
	}

	// Blocking path for encoder initialization — returns silence on timeout.
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()

	select {
	case cm := <-a.chunkChan:
		a.recordDequeue(cm)

		return cm.chunk, func() {}, nil
	case <-a.closeChan:
		return nil, func() {}, ErrBufferClosed
	case <-timer.C:
		// Silence timeout: do not touch the capture-time atomics (mirrors
		// FrameBuffer's black-frame behavior — preserve the last real stamp).
		return getSilenceChunk(a.sampleRate, a.channels), func() {}, nil
	}
}

// recordDequeue stores the dequeued chunk's capture time and the wall-clock
// dequeue instant for GetTrackStats / capture-timestamp stamping.
func (a *AudioBuffer) recordDequeue(cm chunkWithMeta) {
	a.lastCaptureTSUs.Store(cm.captureTSUs)
	a.lastDequeueWallUs.Store(time.Now().UnixMicro())
}

// PushPCM adds an interleaved PCM chunk to the buffer with no capture timestamp.
// Equivalent to PushPCMWithCaptureTS(samples, sampleRate, channels, 0).
func (a *AudioBuffer) PushPCM(samples []int16, sampleRate, channels int) error {
	return a.PushPCMWithCaptureTS(samples, sampleRate, channels, 0)
}

// PushPCMWithCaptureTS adds an interleaved PCM chunk together with an opaque
// capture timestamp (unix microseconds; 0 = none).
// samples is interleaved 16-bit PCM (len must be a multiple of channels).
// sampleRate/channels must stay constant across pushes: the Opus encoder's
// internal accumulator discards partial data if the channel count changes.
// If the buffer is full, the oldest chunk is dropped to add the new one.
func (a *AudioBuffer) PushPCMWithCaptureTS(samples []int16, sampleRate, channels int, captureTSUs int64) error {
	if channels <= 0 || len(samples)%channels != 0 {
		return ErrInvalidPCMChunk
	}

	select {
	case <-a.closeChan:
		return ErrBufferClosed
	default:
	}

	chunk := &wave.Int16Interleaved{
		Data: samples,
		Size: wave.ChunkInfo{
			Len:          len(samples) / channels,
			Channels:     channels,
			SamplingRate: sampleRate,
		},
	}

	return a.enqueue(chunkWithMeta{chunk: chunk, captureTSUs: captureTSUs})
}

// enqueue adds a chunk to the bounded queue, dropping the oldest chunk if full,
// then signals a waiting reader.
func (a *AudioBuffer) enqueue(cm chunkWithMeta) error {
	select {
	case a.chunkChan <- cm:
		a.signal()

		return nil
	default:
	}

	// Buffer full - drop oldest chunk and add the new one.
	select {
	case <-a.chunkChan: // Remove oldest
	default: // Buffer was emptied by a concurrent reader (race)
	}

	select {
	case a.chunkChan <- cm:
		a.signal()

		return nil
	default:
		return ErrFailedToAddFrameAfterDrop
	}
}
