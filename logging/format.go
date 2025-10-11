// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
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

// RTPFormat formats an RTP packet as a string for logging.
func (f *RTPFormatter) RTPFormat(pkt *rtp.Packet, _ interceptor.Attributes) string {
	var twcc rtp.TransportCCExtension
	unwrappedSeqNr := f.seqnr.Unwrap(pkt.SequenceNumber)
	var twccNr uint16
	if len(pkt.GetExtensionIDs()) > 0 {
		ext := pkt.GetExtension(pkt.GetExtensionIDs()[0])
		if err := twcc.Unmarshal(ext); err != nil {
			return fmt.Sprintf("Error unmarshaling TWCC extension: %v", err)
		}
		twccNr = twcc.TransportSequence
	}

	return fmt.Sprintf("%v, %v, %v, %v, %v, %v, %v, %v, %v\n",
		time.Now().UnixMilli(),
		pkt.PayloadType,
		pkt.SSRC,
		pkt.SequenceNumber,
		pkt.Timestamp,
		pkt.Marker,
		pkt.MarshalSize(),
		twccNr,
		unwrappedSeqNr,
	)
}

// RTCPFormat formats RTCP packets as a string for logging.
func RTCPFormat(pkts []rtcp.Packet, _ interceptor.Attributes) string {
	now := time.Now().UnixMilli()
	size := 0
	for _, pkt := range pkts {
		switch feedback := pkt.(type) {
		case *rtcp.TransportLayerCC:
			size += int(feedback.Len())
		case *rtcp.RawPacket:
			size += len(*feedback)
		}
	}

	return fmt.Sprintf("%v, %v\n", now, size)
}
