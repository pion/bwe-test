// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package logging provides utilities for logging in bandwidth estimation tests.
package logging

import (
	"fmt"
	"time"

	"github.com/pion/bwe-test/internal/sequencenumber"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

// RTPFormatter formats RTP packets for logging.
type RTPFormatter struct {
	seqnr sequencenumber.Unwrapper
}

// RTPFormat formats an RTP packet as a binary byte slice for logging.
func (f *RTPFormatter) RTPFormat(pkt *rtp.Packet, _ interceptor.Attributes) ([]byte, error) {
	var twcc rtp.TransportCCExtension
	unwrappedSeqNr := f.seqnr.Unwrap(pkt.SequenceNumber)
	var twccNr uint16
	if len(pkt.GetExtensionIDs()) > 0 {
		ext := pkt.GetExtension(pkt.GetExtensionIDs()[0])
		if err := twcc.Unmarshal(ext); err != nil {
			return nil, fmt.Errorf("error unmarshaling TWCC extension: %w", err)
		}
		twccNr = twcc.TransportSequence
	}

	return []byte(fmt.Sprintf("%v, %v, %v, %v, %v, %v, %v, %v, %v\n",
		time.Now().UnixMilli(),
		pkt.PayloadType,
		pkt.SSRC,
		pkt.SequenceNumber,
		pkt.Timestamp,
		pkt.Marker,
		pkt.MarshalSize(),
		twccNr,
		unwrappedSeqNr,
	)), nil
}

// RTCPFormat formats a single RTCP packet as a binary byte slice for logging.
func RTCPFormat(pkt rtcp.Packet, _ interceptor.Attributes) ([]byte, error) {
	now := time.Now().UnixMilli()
	size := 0
	switch feedback := pkt.(type) {
	case *rtcp.TransportLayerCC:
		size = int(feedback.Len())
	case *rtcp.RawPacket:
		size = len(*feedback)
	}

	return []byte(fmt.Sprintf("%v, %v\n", now, size)), nil
}
