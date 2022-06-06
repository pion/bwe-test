package logging

import (
	"fmt"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"time"
)

func RTPFormat(pkt *rtp.Packet, _ interceptor.Attributes) string {
	return fmt.Sprintf("%v, %v, %v, %v, %v, %v, %v\n",
		time.Now().UnixMilli(),
		pkt.PayloadType,
		pkt.SSRC,
		pkt.SequenceNumber,
		pkt.Timestamp,
		pkt.Marker,
		pkt.MarshalSize(),
	)
}
