// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js

package receiver

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// encodeCaptureRTP reproduces the sender's captureTimestampInterceptor encoding:
// the outgoing RTP timestamp is captureUs*ClockRate/1e6 truncated to 32 bits.
// It uses the same reduced fraction as the interceptor so captureUs*num does not
// overflow int64 (captureUs*ClockRate would).
func encodeCaptureRTP(captureUs int64, clockRate uint32) uint32 {
	rate := int64(clockRate)
	g := gcd(rate, 1_000_000)
	num := rate / g
	den := int64(1_000_000) / g

	return uint32(captureUs * num / den) //nolint:gosec // intentional 32-bit wrap
}

// quantUs is the maximum round-trip error introduced by the sender's and the
// recovery's integer divisions: one tick, i.e. 1e6/ClockRate microseconds.
func quantUs(clockRate uint32) int64 {
	return 1_000_000/int64(clockRate) + 1
}

// TestCaptureTimeFromRTP_RoundTrip asserts that a capture time encoded with the
// sender's formula is recovered within one RTP tick, and that the resulting
// glass-to-glass latency matches the injected delay, across clock rates and
// delays.
func TestCaptureTimeFromRTP_RoundTrip(t *testing.T) {
	// A recent, realistic capture instant (unix micros).
	const baseCaptureUs = int64(1_751_000_000_000_000)

	clockRates := []uint32{90000, 48000}
	latencies := []time.Duration{
		0,
		5 * time.Millisecond,
		50 * time.Millisecond,
		250 * time.Millisecond,
		time.Second,
	}

	for _, rate := range clockRates {
		for _, lat := range latencies {
			captureUs := baseCaptureUs
			rtpTS := encodeCaptureRTP(captureUs, rate)

			latUs := lat.Microseconds()
			nowUs := captureUs + latUs

			recovered := CaptureTimeUsFromRTP(rtpTS, rate, nowUs)
			assert.InDeltaf(t, captureUs, recovered, float64(quantUs(rate)),
				"rate=%d lat=%s: recovered capture time out of tolerance", rate, lat)

			gotLatency := GlassToGlassLatency(rtpTS, rate, nowUs)
			assert.InDeltaf(t, latUs, gotLatency.Microseconds(), float64(quantUs(rate)),
				"rate=%d lat=%s: recovered latency out of tolerance", rate, lat)
		}
	}
}

// TestCaptureTimeFromRTP_WrapBoundary asserts recovery is correct when the
// capture instant's encoded ticks and nowUnixUs's ticks straddle a 2^32
// boundary, exercising the wrap-correction branch.
func TestCaptureTimeFromRTP_WrapBoundary(t *testing.T) {
	const rate = uint32(90000)

	// Find a capture time whose encoded 90 kHz ticks sit just below a 2^32
	// multiple, so that a small added latency pushes nowUnixUs's ticks across
	// the boundary. 2^32 ticks / 90000 ticks-per-second = wrap period; work in
	// microseconds: wrapUs = 2^32 * 1e6 / 90000.
	wrapUs := int64(1) << 32 * 1_000_000 / int64(rate)
	// Capture 1 ms of ticks before the 5th wrap boundary.
	captureUs := 5*wrapUs - time.Millisecond.Microseconds()

	rtpTS := encodeCaptureRTP(captureUs, rate)
	// 50 ms later — nowUnixUs is past the boundary while rtpTS is from before it.
	nowUs := captureUs + 50*time.Millisecond.Microseconds()

	recovered := CaptureTimeUsFromRTP(rtpTS, rate, nowUs)
	assert.InDelta(t, captureUs, recovered, float64(quantUs(rate)),
		"recovery must cross the 2^32 wrap boundary correctly")
}

// TestCaptureTimeFromRTP_ClockRateZeroFallsBackTo90k asserts a zero clock rate
// is treated as 90 kHz, matching the sender's fallback.
func TestCaptureTimeFromRTP_ClockRateZeroFallsBackTo90k(t *testing.T) {
	const baseCaptureUs = int64(1_751_000_000_000_000)

	rtpTS := encodeCaptureRTP(baseCaptureUs, 90000)
	nowUs := baseCaptureUs + 30*time.Millisecond.Microseconds()

	withZero := CaptureTimeUsFromRTP(rtpTS, 0, nowUs)
	with90k := CaptureTimeUsFromRTP(rtpTS, 90000, nowUs)
	assert.Equal(t, with90k, withZero, "clockRate 0 must behave like 90 kHz")
}

// TestReportGlassToGlassLatency asserts the receiver's read-loop helper recovers
// a recent capture instant from a live RTP timestamp and reports a latency close
// to the true elapsed time, at both the audio (48 kHz) and video (90 kHz) rates.
func TestReportGlassToGlassLatency(t *testing.T) {
	r, err := NewReceiver()
	require.NoError(t, err)

	const elapsed = 40 * time.Millisecond
	// Generous upper bound: recovery quantization plus scheduler/test slack.
	const tol = 25 * time.Millisecond

	for _, rate := range []uint32{48000, 90000} {
		// A frame captured `elapsed` ago, stamped as the sender would.
		captureUs := time.Now().UnixMicro() - elapsed.Microseconds()
		rtpTS := encodeCaptureRTP(captureUs, rate)

		got := r.reportGlassToGlassLatency("track-test", rtpTS, rate)
		assert.InDeltaf(t, elapsed.Microseconds(), got.Microseconds(), float64(tol.Microseconds()),
			"rate=%d: reported latency %s should track elapsed %s", rate, got, elapsed)
	}
}
