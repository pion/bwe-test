//go:build !js

// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"testing"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/stretchr/testify/assert"
)

const (
	testCaptureSSRC = uint32(0x1234)
)

// captureCollector is an interceptor.RTPWriter that records the RTP timestamps
// it is handed, so tests can assert what the interceptor wrote.
type captureCollector struct {
	timestamps []uint32
}

func (c *captureCollector) Write(header *rtp.Header, _ []byte, _ interceptor.Attributes) (int, error) {
	c.timestamps = append(c.timestamps, header.Timestamp)

	return 0, nil
}

func bindCapture(it *captureTimestampInterceptor, sink interceptor.RTPWriter) interceptor.RTPWriter {
	return it.BindLocalStream(&interceptor.StreamInfo{SSRC: testCaptureSSRC}, sink)
}

func bindCaptureRate(
	it *captureTimestampInterceptor, sink interceptor.RTPWriter, clockRate uint32,
) interceptor.RTPWriter {
	return it.BindLocalStream(&interceptor.StreamInfo{SSRC: testCaptureSSRC, ClockRate: clockRate}, sink)
}

// TestCaptureTimestampInterceptor_EncodesCaptureTime asserts the RTP timestamp
// is overwritten with captureUs*9/100 (mod 2^32), constant across a frame.
func TestCaptureTimestampInterceptor_EncodesCaptureTime(t *testing.T) {
	it := newCaptureTimestampInterceptor()
	it.SetRewriteEnabled(true)
	sink := &captureCollector{}
	w := bindCapture(it, sink)

	captureUs := int64(1_751_000_000_000_000)
	want := uint32(captureUs * 9 / 100) //nolint:gosec // intentional 32-bit wrap
	it.SetCaptureTSUs(testCaptureSSRC, captureUs)

	// Two packets of the same frame share one original timestamp; both must
	// come out with the capture-derived timestamp.
	_, _ = w.Write(&rtp.Header{Timestamp: 42}, nil, nil)
	_, _ = w.Write(&rtp.Header{Timestamp: 42}, nil, nil)

	assert.Equal(t, []uint32{want, want}, sink.timestamps)
}

// TestCaptureTimestampInterceptor_ClockRate48k asserts an Opus (48 kHz) stream
// encodes the capture time in 48 kHz ticks (captureUs*48000/1e6 = captureUs*6/125).
func TestCaptureTimestampInterceptor_ClockRate48k(t *testing.T) {
	it := newCaptureTimestampInterceptor()
	sink := &captureCollector{}
	w := bindCaptureRate(it, sink, 48000)

	captureUs := int64(1_751_000_000_000_000)
	want := uint32(captureUs * 6 / 125) //nolint:gosec // intentional 32-bit wrap
	it.SetCaptureTSUs(testCaptureSSRC, captureUs)

	_, _ = w.Write(&rtp.Header{Timestamp: 42}, nil, nil)
	_, _ = w.Write(&rtp.Header{Timestamp: 42}, nil, nil)

	assert.Equal(t, []uint32{want, want}, sink.timestamps)
}

// TestCaptureTimestampInterceptor_ClockRateZeroFallsBackTo90k asserts a stream
// that reports no clock rate keeps the prior 90 kHz video behavior.
func TestCaptureTimestampInterceptor_ClockRateZeroFallsBackTo90k(t *testing.T) {
	it := newCaptureTimestampInterceptor()
	sink := &captureCollector{}
	w := bindCaptureRate(it, sink, 0)

	captureUs := int64(1_751_000_000_000_000)
	want := uint32(captureUs * 9 / 100) //nolint:gosec // intentional 32-bit wrap
	it.SetCaptureTSUs(testCaptureSSRC, captureUs)

	_, _ = w.Write(&rtp.Header{Timestamp: 42}, nil, nil)

	assert.Equal(t, []uint32{want}, sink.timestamps)
}

// TestCaptureTimestampInterceptor_PassthroughWhenUnset asserts a frame with no
// capture time keeps its original packetizer timestamp.
func TestCaptureTimestampInterceptor_PassthroughWhenUnset(t *testing.T) {
	it := newCaptureTimestampInterceptor()
	sink := &captureCollector{}
	w := bindCapture(it, sink)

	_, _ = w.Write(&rtp.Header{Timestamp: 777}, nil, nil)

	assert.Equal(t, []uint32{777}, sink.timestamps)
}

// TestCaptureTimestampInterceptor_RemoveSSRC asserts RemoveSSRC drops the stale
// slot so it does not accumulate across reconnects, and that a fresh slot is
// created (starting empty) on the next lookup for the same SSRC.
func TestCaptureTimestampInterceptor_RemoveSSRC(t *testing.T) {
	it := newCaptureTimestampInterceptor()

	it.SetCaptureTSUs(testCaptureSSRC, 1_751_000_000_000_000)
	assert.Len(t, it.slots, 1)

	it.RemoveSSRC(testCaptureSSRC)
	assert.Empty(t, it.slots)

	// A subsequent lookup re-creates the slot, starting from a cleared state.
	assert.Zero(t, it.slot(testCaptureSSRC).Load())
	// Removing an unknown SSRC is a no-op.
	it.RemoveSSRC(0xDEAD)
	assert.Len(t, it.slots, 1)
}

// TestCaptureTimestampInterceptor_DisabledByDefault asserts the rewrite is off
// unless explicitly enabled, so a capture time supplied for telemetry does not
// silently replace the packetizer's clock. Overwriting it breaks any track whose
// sending is gated outside the RTP stack: such a track starts mid-session with a
// wall-clock-derived base and receivers never render it.
func TestCaptureTimestampInterceptor_DisabledByDefault(t *testing.T) {
	it := newCaptureTimestampInterceptor()
	sink := &captureCollector{}
	w := bindCapture(it, sink)

	it.SetCaptureTSUs(testCaptureSSRC, 1_751_000_000_000_000)
	_, _ = w.Write(&rtp.Header{Timestamp: 4242}, nil, nil)

	// Packetizer timestamp preserved, capture time still recorded for callers
	// that read it for telemetry.
	assert.Equal(t, []uint32{4242}, sink.timestamps)
	assert.Equal(t, int64(1_751_000_000_000_000), it.slot(testCaptureSSRC).Load())
}

// TestCaptureTimestampInterceptor_RewriteToggle asserts the gate takes effect
// per frame, so enabling and disabling at runtime is honored.
func TestCaptureTimestampInterceptor_RewriteToggle(t *testing.T) {
	it := newCaptureTimestampInterceptor()
	sink := &captureCollector{}
	w := bindCapture(it, sink)

	captureUs := int64(1_751_000_000_000_000)
	it.SetCaptureTSUs(testCaptureSSRC, captureUs)
	want := uint32(captureUs * 9 / 100) //nolint:gosec // intentional 32-bit wrap

	_, _ = w.Write(&rtp.Header{Timestamp: 100}, nil, nil)
	it.SetRewriteEnabled(true)
	_, _ = w.Write(&rtp.Header{Timestamp: 200}, nil, nil)
	it.SetRewriteEnabled(false)
	_, _ = w.Write(&rtp.Header{Timestamp: 300}, nil, nil)

	assert.Equal(t, []uint32{100, want, 300}, sink.timestamps)
}
