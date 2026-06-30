//go:build !js

// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

// absCaptureTimeURI is the WebRTC experiment URI for the "Absolute Capture Time"
// RTP header extension. It carries the NTP timestamp at which a frame was
// captured so receivers (and a forwarding SFU like LiveKit) can recover the
// original capture instant.
const absCaptureTimeURI = "http://www.webrtc.org/experiments/rtp-hdrext/abs-capture-time"

// absCaptureTimeInterceptor stamps outgoing RTP packets with the abs-capture-time
// header extension. The capture time is supplied out-of-band, per SSRC, via
// SetCaptureTSUs immediately before each WriteSample call; because packetization
// and the interceptor write run synchronously in the same goroutine as
// WriteSample, the stored value is correct for the packets produced by that
// sample. Keying by SSRC keeps concurrent per-track encode goroutines from
// stamping each other's frames.
type absCaptureTimeInterceptor struct {
	interceptor.NoOp

	mu    sync.Mutex
	slots map[uint32]*atomic.Int64 // ssrc -> capture time (unix nanoseconds), 0 = none
}

func newAbsCaptureTimeInterceptor() *absCaptureTimeInterceptor {
	return &absCaptureTimeInterceptor{slots: make(map[uint32]*atomic.Int64)}
}

// slot returns the (lazily created) capture-time slot for an SSRC.
func (it *absCaptureTimeInterceptor) slot(ssrc uint32) *atomic.Int64 {
	it.mu.Lock()
	defer it.mu.Unlock()

	s, ok := it.slots[ssrc]
	if !ok {
		s = &atomic.Int64{}
		it.slots[ssrc] = s
	}

	return s
}

// SetCaptureTime records the capture time to stamp on the SSRC's next frame.
// A zero time disables stamping until the next non-zero value.
func (it *absCaptureTimeInterceptor) SetCaptureTime(ssrc uint32, t time.Time) {
	if t.IsZero() {
		it.slot(ssrc).Store(0)

		return
	}
	it.slot(ssrc).Store(t.UnixNano())
}

// SetCaptureTSUs records the capture time for an SSRC from a unix-microsecond
// timestamp (the units used throughout the encode pipeline). 0 disables stamping.
func (it *absCaptureTimeInterceptor) SetCaptureTSUs(ssrc uint32, captureTSUs int64) {
	if captureTSUs == 0 {
		it.slot(ssrc).Store(0)

		return
	}
	it.slot(ssrc).Store(captureTSUs * int64(time.Microsecond))
}

// BindLocalStream returns a writer that stamps the abs-capture-time extension on
// the first RTP packet of each frame. If the extension was not negotiated for
// this stream, the original writer is returned unchanged.
func (it *absCaptureTimeInterceptor) BindLocalStream(
	info *interceptor.StreamInfo,
	writer interceptor.RTPWriter,
) interceptor.RTPWriter {
	extID := 0
	for _, ext := range info.RTPHeaderExtensions {
		if ext.URI == absCaptureTimeURI {
			extID = ext.ID

			break
		}
	}

	// Valid one/two-byte extension IDs are >= 1; 0 means not negotiated.
	if extID == 0 {
		return writer
	}

	slot := it.slot(info.SSRC)

	var (
		hasLast bool
		lastTS  uint32
	)

	return interceptor.RTPWriterFunc(
		func(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
			// Stamp only the first packet of each frame, matching libwebrtc.
			firstOfFrame := !hasLast || header.Timestamp != lastTS
			lastTS = header.Timestamp
			hasLast = true

			if firstOfFrame {
				if nano := slot.Load(); nano != 0 {
					ext := rtp.NewAbsCaptureTimeExtension(time.Unix(0, nano))
					if buf, err := ext.Marshal(); err == nil {
						_ = header.SetExtension(uint8(extID), buf) //nolint:gosec // extID is a negotiated 1..255 ext ID
					}
				}
			}

			return writer.Write(header, payload, attributes)
		},
	)
}

// absCaptureTimeFactory adapts a pre-built absCaptureTimeInterceptor to the
// interceptor.Factory interface so the same instance (which RTCSender keeps a
// reference to for SetCaptureTSUs) is used by the PeerConnection.
type absCaptureTimeFactory struct {
	it *absCaptureTimeInterceptor
}

func (f *absCaptureTimeFactory) NewInterceptor(string) (interceptor.Interceptor, error) {
	return f.it, nil
}
