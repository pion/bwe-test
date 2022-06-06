package receiver

import (
	"io"
	"time"

	"github.com/pion/transport/vnet"
	"github.com/pion/webrtc/v3"
)

type Option func(*Receiver) error

func RTPLogWriter(w io.Writer) Option {
	return func(r *Receiver) error {
		r.rtpWriter = w
		return nil
	}
}

func Vnet(v *vnet.Net, publicIPs []string) Option {
	return func(r *Receiver) error {
		r.settingEngine.SetVNet(v)
		r.settingEngine.SetICETimeouts(time.Second, time.Second, 200*time.Millisecond)
		r.settingEngine.SetNAT1To1IPs(publicIPs, webrtc.ICECandidateTypeHost)
		return nil
	}
}
