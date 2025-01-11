// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package logging

import (
	"fmt"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

const (
	maxSequenceNumberPlusOne = int64(65536)
	breakpoint               = 32768 // half of max uint16
)

type unwrapper struct {
	init          bool
	lastUnwrapped int64
}

func isNewer(value, previous uint16) bool {
	if value-previous == breakpoint {
		return value > previous
	}
	return value != previous && (value-previous) < breakpoint
}

func (u *unwrapper) unwrap(i uint16) int64 {
	if !u.init {
		u.init = true
		u.lastUnwrapped = int64(i)
		return u.lastUnwrapped
	}

	lastWrapped := uint16(u.lastUnwrapped)
	delta := int64(i - lastWrapped)
	if isNewer(i, lastWrapped) {
		if delta < 0 {
			delta += maxSequenceNumberPlusOne
		}
	} else if delta > 0 && u.lastUnwrapped+delta-maxSequenceNumberPlusOne >= 0 {
		delta -= maxSequenceNumberPlusOne
	}

	u.lastUnwrapped += int64(delta)
	return u.lastUnwrapped
}

type RTPFormatter struct {
	seqnr unwrapper
}

func (f *RTPFormatter) RTPFormat(pkt *rtp.Packet, _ interceptor.Attributes) string {
	var twcc rtp.TransportCCExtension
	unwrappedSeqNr := f.seqnr.unwrap(pkt.SequenceNumber)
	var twccNr uint16
	if len(pkt.GetExtensionIDs()) > 0 {
		ext := pkt.GetExtension(pkt.GetExtensionIDs()[0])
		if err := twcc.Unmarshal(ext); err != nil {
			panic(err)
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

func RTCPFormat(pkts []rtcp.Packet, _ interceptor.Attributes) string {
	now := time.Now().UnixMilli()
	size := 0
	for _, pkt := range pkts {
		switch feedback := pkt.(type) {
		case *rtcp.TransportLayerCC:
			size += int(feedback.Len())
		case *rtcp.RawPacket:
			size += int(len(*feedback))
		}
	}
	return fmt.Sprintf("%v, %v\n", now, size)
}
