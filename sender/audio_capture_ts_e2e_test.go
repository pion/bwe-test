//go:build !js

// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"testing"
	"time"

	"github.com/pion/bwe-test/receiver"
	"github.com/pion/rtp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// audioClockRate is the RTP clock a negotiated Opus audio stream uses.
const audioClockRate = uint32(48000)

// audioQuantUs is the round-trip quantization bound (one 48 kHz tick), matching
// the receiver-side recovery tolerance.
const audioQuantUs = 1_000_000/int64(audioClockRate) + 1

// stampAndEmitAudio runs a capture time through the real captureTimestampInterceptor
// at the Opus (48 kHz) clock and returns the RTP timestamp it wrote on the wire.
func stampAndEmitAudio(t *testing.T, it *captureTimestampInterceptor, captureUs int64) uint32 {
	t.Helper()

	sink := &captureCollector{}
	w := bindCaptureRate(it, sink, audioClockRate)

	it.SetCaptureTSUs(testCaptureSSRC, captureUs)
	_, err := w.Write(&rtp.Header{Timestamp: 42}, nil, nil)
	require.NoError(t, err)

	require.Len(t, sink.timestamps, 1, "one packet should have been emitted")

	return sink.timestamps[0]
}

// TestAudioCaptureTS_RTPTimestampRoundTrip ties the real sender stamping path
// (captureTimestampInterceptor at 48 kHz) to the real receiver recovery path
// (receiver.GlassToGlassLatency): a capture time encoded into the outgoing Opus
// RTP timestamp must yield, at the receiver, a glass-to-glass latency equal to
// the injected delay within RTP-clock quantization. Mirrors the video test for
// audio, exercised end-to-end without a codec or network.
func TestAudioCaptureTS_RTPTimestampRoundTrip(t *testing.T) {
	it := newCaptureTimestampInterceptor()

	// Capture instant "at the microphone" on the sender.
	captureUs := time.Now().UnixMicro()
	emittedTS := stampAndEmitAudio(t, it, captureUs)

	latencies := []time.Duration{
		0,
		5 * time.Millisecond,
		50 * time.Millisecond,
		250 * time.Millisecond,
		time.Second,
	}

	for _, lat := range latencies {
		// Receiver observes the frame `lat` after capture.
		nowUs := captureUs + lat.Microseconds()

		gotLatency := receiver.GlassToGlassLatency(emittedTS, audioClockRate, nowUs)
		assert.InDeltaf(t, lat.Microseconds(), gotLatency.Microseconds(), float64(audioQuantUs),
			"injected latency %s: measured audio glass-to-glass latency out of tolerance", lat)
	}
}

// TestAudioCaptureTS_LiveElapsed asserts the round trip works against real
// wall-clock elapsed time: stamp now, wait, then recover the latency at receipt.
func TestAudioCaptureTS_LiveElapsed(t *testing.T) {
	it := newCaptureTimestampInterceptor()

	const sleep = 20 * time.Millisecond

	captureUs := time.Now().UnixMicro()
	emittedTS := stampAndEmitAudio(t, it, captureUs)

	time.Sleep(sleep)
	nowUs := time.Now().UnixMicro()

	gotLatency := receiver.GlassToGlassLatency(emittedTS, audioClockRate, nowUs)

	// At least the sleep (minus one tick of quantization) and not wildly above
	// it; the upper bound leaves generous slack for scheduler jitter.
	assert.GreaterOrEqual(t, gotLatency.Microseconds(), sleep.Microseconds()-audioQuantUs,
		"measured latency should be at least the elapsed sleep")
	assert.Less(t, gotLatency, sleep+500*time.Millisecond,
		"measured latency should track the elapsed sleep, not run away")
}
