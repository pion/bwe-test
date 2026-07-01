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

// TestCaptureTimestampInterceptor_EncodesCaptureTime asserts the RTP timestamp
// is overwritten with captureUs*9/100 (mod 2^32), constant across a frame.
func TestCaptureTimestampInterceptor_EncodesCaptureTime(t *testing.T) {
	it := newCaptureTimestampInterceptor()
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
