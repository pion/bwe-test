//go:build !js

// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"testing"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAbsCaptureExtID = 5
	testAbsCaptureSSRC  = 0x1234
)

// captureWriter records the headers passed through the interceptor's writer.
type captureWriter struct {
	headers []*rtp.Header
}

func (w *captureWriter) Write(header *rtp.Header, _ []byte, _ interceptor.Attributes) (int, error) {
	// Copy the header so later mutations don't affect recorded values.
	clone := *header
	clone.Extensions = append([]rtp.Extension(nil), header.Extensions...)
	w.headers = append(w.headers, &clone)

	return 0, nil
}

func absCaptureExt(t *testing.T, header *rtp.Header) (time.Time, bool) {
	t.Helper()
	payload := header.GetExtension(testAbsCaptureExtID)
	if payload == nil {
		return time.Time{}, false
	}
	var ext rtp.AbsCaptureTimeExtension
	require.NoError(t, ext.Unmarshal(payload))

	return ext.CaptureTime(), true
}

func negotiatedStreamInfo() *interceptor.StreamInfo {
	return &interceptor.StreamInfo{
		SSRC: testAbsCaptureSSRC,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{
			{URI: absCaptureTimeURI, ID: testAbsCaptureExtID},
		},
	}
}

func TestAbsCaptureTimeInterceptor_StampsFirstPacketOfFrame(t *testing.T) {
	it := newAbsCaptureTimeInterceptor()
	rec := &captureWriter{}
	writer := it.BindLocalStream(negotiatedStreamInfo(), rec)

	capture := time.Unix(1_700_000_000, 123_000_000) // arbitrary wall-clock time
	it.SetCaptureTime(testAbsCaptureSSRC, capture)

	// Frame 1: two packets sharing the same RTP timestamp.
	_, _ = writer.Write(&rtp.Header{Timestamp: 1000}, []byte("a"), nil)
	_, _ = writer.Write(&rtp.Header{Timestamp: 1000}, []byte("b"), nil)
	// Frame 2: new RTP timestamp.
	_, _ = writer.Write(&rtp.Header{Timestamp: 2000}, []byte("c"), nil)

	require.Len(t, rec.headers, 3)

	// First packet of frame 1 is stamped.
	got, ok := absCaptureExt(t, rec.headers[0])
	require.True(t, ok, "first packet of frame should carry abs-capture-time")
	// NTP conversion is lossy to ~sub-ms; allow a small tolerance.
	assert.WithinDuration(t, capture, got, time.Millisecond)

	// Second packet of the same frame is NOT stamped.
	_, ok = absCaptureExt(t, rec.headers[1])
	assert.False(t, ok, "non-first packet of frame should not be stamped")

	// First packet of frame 2 is stamped again.
	_, ok = absCaptureExt(t, rec.headers[2])
	assert.True(t, ok, "first packet of next frame should be stamped")
}

func TestAbsCaptureTimeInterceptor_SetCaptureTSUs(t *testing.T) {
	it := newAbsCaptureTimeInterceptor()
	rec := &captureWriter{}
	writer := it.BindLocalStream(negotiatedStreamInfo(), rec)

	capture := time.Unix(1_700_000_000, 123_000_000)
	it.SetCaptureTSUs(testAbsCaptureSSRC, capture.UnixMicro())

	_, _ = writer.Write(&rtp.Header{Timestamp: 1000}, []byte("a"), nil)

	require.Len(t, rec.headers, 1)
	got, ok := absCaptureExt(t, rec.headers[0])
	require.True(t, ok)
	assert.WithinDuration(t, capture, got, time.Millisecond)
}

func TestAbsCaptureTimeInterceptor_NoExtensionWhenNotNegotiated(t *testing.T) {
	it := newAbsCaptureTimeInterceptor()
	rec := &captureWriter{}
	// StreamInfo without the abs-capture-time extension.
	writer := it.BindLocalStream(&interceptor.StreamInfo{}, rec)

	it.SetCaptureTime(testAbsCaptureSSRC, time.Unix(1_700_000_000, 0))
	_, _ = writer.Write(&rtp.Header{Timestamp: 1000}, []byte("a"), nil)

	require.Len(t, rec.headers, 1)
	_, ok := absCaptureExt(t, rec.headers[0])
	assert.False(t, ok, "extension must not be added when not negotiated")
}

func TestAbsCaptureTimeInterceptor_NoStampWhenCaptureUnset(t *testing.T) {
	it := newAbsCaptureTimeInterceptor()
	rec := &captureWriter{}
	writer := it.BindLocalStream(negotiatedStreamInfo(), rec)

	// No SetCaptureTime call -> captureNano is 0 -> no stamp.
	_, _ = writer.Write(&rtp.Header{Timestamp: 1000}, []byte("a"), nil)

	require.Len(t, rec.headers, 1)
	_, ok := absCaptureExt(t, rec.headers[0])
	assert.False(t, ok, "no extension should be stamped when capture time is unset")
}
