// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package gcc

import (
	"fmt"
	"time"
)

// ECN represents the ECN bits of an IP packet header.
type ECN uint8

const (
	// ECNNonECT signals Non ECN-Capable Transport, Non-ECT.
	// nolint:misspell
	ECNNonECT ECN = iota // 00

	// ECNECT1 signals ECN Capable Transport, ECT(0).
	// nolint:misspell
	ECNECT1 // 01

	// ECNECT0 signals ECN Capable Transport, ECT(1).
	// nolint:misspell
	ECNECT0 // 10

	// ECNCE signals ECN Congestion Encountered, CE.
	// nolint:misspell
	ECNCE // 11
)

// An Acknowledgment stores send and receive information about a packet.
type Acknowledgment struct {
	SeqNr     uint64
	Size      uint16
	Departure time.Time
	Arrived   bool
	Arrival   time.Time
	ECN       ECN
}

func (a Acknowledgment) String() string {
	return fmt.Sprintf("seq=%v, departure=%v, arrival=%v", a.SeqNr, a.Departure, a.Arrival)
}
