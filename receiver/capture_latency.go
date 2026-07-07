// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js

package receiver

import "time"

// CaptureTimeUsFromRTP recovers the capture instant (unix microseconds) that the
// sender encoded into an RTP timestamp via captureUs*ClockRate/1e6 (mod 2^32)
// (see sender's captureTimestampInterceptor). nowUnixUs is a recent wall-clock
// reading near packet receipt, used to disambiguate the 32-bit wrap; the capture
// instant is taken to be the most recent time whose encoded low 32 bits match
// rtpTS and which is not in the future relative to nowUnixUs. clockRate 0 falls
// back to 90 kHz, matching the sender. Mirrors the browser recovery
// getSynchronizationSources()[].rtpTimestamp * 1e6/ClockRate.
//
// The recovered value carries a quantization error of at most 1e6/ClockRate
// microseconds (~11 µs at 90 kHz, ~21 µs at 48 kHz) from the sender's and this
// function's integer divisions.
func CaptureTimeUsFromRTP(rtpTS uint32, clockRate uint32, nowUnixUs int64) int64 {
	rate := int64(clockRate)
	if rate == 0 {
		rate = 90000
	}

	// Reduce ClockRate/1e6 to lowest terms so nowUnixUs*num and ticks*den stay
	// within int64: nowUnixUs is unix micros (~1.7e15) and nowUnixUs*ClockRate
	// would overflow. 90 kHz -> 9/100, 48 kHz -> 6/125. Mirrors the sender.
	g := gcd(rate, 1_000_000)
	num := rate / g
	den := int64(1_000_000) / g

	// Full (non-wrapped) tick count at nowUnixUs; may exceed 2^32.
	nowTicks := nowUnixUs * num / den

	// Splice rtpTS into the high bits of nowTicks, then snap to the wrap nearest
	// nowTicks. Capture is within half a wrap period of receipt for any realistic
	// latency or cross-machine clock skew (the wrap period is 2^32 ticks, ~13.25 h
	// at 90 kHz / ~24.9 h at 48 kHz), so a candidate more than 2^31 ticks away is a
	// wrap artifact. Using a half-period guard band (rather than "pull back
	// whenever candidate > nowTicks") keeps a sender clock that runs slightly ahead
	// mapping to a small negative latency instead of collapsing a full 2^32 wrap
	// (~24.9 h) onto it.
	candidate := (nowTicks &^ 0xFFFFFFFF) | int64(rtpTS)
	switch {
	case candidate-nowTicks > 1<<31:
		candidate -= 1 << 32
	case nowTicks-candidate > 1<<31:
		candidate += 1 << 32
	}

	return candidate * den / num
}

// GlassToGlassLatency returns nowUnixUs minus the capture instant recovered from
// the RTP timestamp: the elapsed time from frame capture to this observation.
func GlassToGlassLatency(rtpTS uint32, clockRate uint32, nowUnixUs int64) time.Duration {
	captureUs := CaptureTimeUsFromRTP(rtpTS, clockRate, nowUnixUs)

	return time.Duration(nowUnixUs-captureUs) * time.Microsecond
}

// gcd returns the greatest common divisor of a and b (both assumed positive).
func gcd(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}

	return a
}
