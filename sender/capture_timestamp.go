//go:build !js

// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"sync"
	"sync/atomic"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

// captureTimestampInterceptor encodes each frame's capture time into the
// outgoing RTP timestamp, expressed in the stream's own RTP clock:
// (captureUs * ClockRate / 1e6) mod 2^32 — 90 kHz ticks for video, 48 kHz for
// Opus audio. This lets the capture instant survive an SFU that re-forwards RTP
// timestamps but strips header extensions on egress (e.g. LiveKit); the browser
// recovers it via receiver.getSynchronizationSources()[].rtpTimestamp * 1e6/ClockRate.
//
// The capture time is supplied out-of-band, per SSRC, via SetCaptureTSUs
// immediately before each WriteSample call; because packetization and the
// interceptor write run synchronously in the same goroutine as WriteSample, the
// stored value is correct for the packets produced by that sample. Keying by
// SSRC keeps concurrent per-track encode goroutines from stamping each other's
// frames.
type captureTimestampInterceptor struct {
	interceptor.NoOp

	mu    sync.Mutex
	slots map[uint32]*atomic.Int64 // ssrc -> capture time (unix microseconds), 0 = none
}

func newCaptureTimestampInterceptor() *captureTimestampInterceptor {
	return &captureTimestampInterceptor{slots: make(map[uint32]*atomic.Int64)}
}

// slot returns the (lazily created) capture-time slot for an SSRC.
func (it *captureTimestampInterceptor) slot(ssrc uint32) *atomic.Int64 {
	it.mu.Lock()
	defer it.mu.Unlock()

	s, ok := it.slots[ssrc]
	if !ok {
		s = &atomic.Int64{}
		it.slots[ssrc] = s
	}

	return s
}

// SetCaptureTSUs records the capture time (unix microseconds) to encode on the
// SSRC's next frame. 0 disables encoding until the next non-zero value.
func (it *captureTimestampInterceptor) SetCaptureTSUs(ssrc uint32, captureTSUs int64) {
	it.slot(ssrc).Store(captureTSUs)
}

// RemoveSSRC drops the capture-time slot for an SSRC. Called when a track's
// sender is replaced on reconnect so stale entries don't accumulate. A live
// BindLocalStream writer still holds its own *atomic.Int64 reference, so this
// only affects future SetCaptureTSUs/slot lookups for the removed SSRC.
func (it *captureTimestampInterceptor) RemoveSSRC(ssrc uint32) {
	it.mu.Lock()
	defer it.mu.Unlock()

	delete(it.slots, ssrc)
}

// BindLocalStream returns a writer that overwrites each frame's RTP timestamp
// with the capture time expressed in the stream's RTP clock ticks. Frames with
// no capture time keep their original packetizer timestamp.
func (it *captureTimestampInterceptor) BindLocalStream(
	info *interceptor.StreamInfo,
	writer interceptor.RTPWriter,
) interceptor.RTPWriter {
	slot := it.slot(info.SSRC)

	// RTP ticks are captureUs * ClockRate / 1e6 (90 kHz video, 48 kHz Opus).
	// Fall back to 90 kHz if the negotiated clock rate is missing so a stream
	// that reports no rate keeps the prior video behavior instead of scaling to
	// zero. Captured once per stream — the rate is fixed for the stream's life.
	clockRate := int64(info.ClockRate)
	if clockRate == 0 {
		clockRate = 90000
	}
	// Reduce ClockRate/1e6 to lowest terms so captureUs*num stays within int64:
	// captureUs is unix micros (~1.7e15) and captureUs*ClockRate would overflow.
	// 90 kHz -> 9/100, 48 kHz -> 6/125. Computed once per stream.
	tickGCD := gcd(clockRate, 1_000_000)
	tickNum := clockRate / tickGCD
	tickDen := int64(1_000_000) / tickGCD

	var (
		hasLast    bool
		lastOrigTS uint32 // original (packetizer) timestamp of the current frame
		frameRTPTS uint32 // capture-derived timestamp applied to every packet of the frame
		frameValid bool   // the current frame had a capture time to encode
	)

	return interceptor.RTPWriterFunc(
		func(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
			// Detect the first packet of a frame using the ORIGINAL packetizer
			// timestamp, captured before the rewrite below changes header.Timestamp.
			firstOfFrame := !hasLast || header.Timestamp != lastOrigTS
			if firstOfFrame {
				lastOrigTS = header.Timestamp
				hasLast = true
				frameValid = false

				if captureUs := slot.Load(); captureUs != 0 {
					// RTP ticks = captureUs * ClockRate / 1e6, using the reduced
					// fraction to avoid int64 overflow. The uint32 conversion
					// applies the required mod 2^32.
					frameRTPTS = uint32(captureUs * tickNum / tickDen) //nolint:gosec // intentional 32-bit wrap
					frameValid = true
				}
			}

			// Apply the capture-derived timestamp to every packet of the frame,
			// keeping it constant across the frame's packets.
			if frameValid {
				header.Timestamp = frameRTPTS
			}

			return writer.Write(header, payload, attributes)
		},
	)
}

// captureTimestampFactory adapts a pre-built captureTimestampInterceptor to the
// interceptor.Factory interface so the same instance (which RTCSender keeps a
// reference to for SetCaptureTSUs) is used by the PeerConnection.
type captureTimestampFactory struct {
	it *captureTimestampInterceptor
}

func (f *captureTimestampFactory) NewInterceptor(string) (interceptor.Interceptor, error) {
	return f.it, nil
}

// gcd returns the greatest common divisor of a and b (both assumed positive).
func gcd(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}

	return a
}
