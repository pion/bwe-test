package sender

import (
	"io"
	"time"

	"github.com/pion/transport/vnet"
	"github.com/pion/webrtc/v3"
)

type Option func(*Sender) error

func RTPLogWriter(w io.Writer) Option {
	return func(s *Sender) error {
		s.rtpWriter = w
		return nil
	}
}

func SetVnet(v *vnet.Net, publicIPs []string) Option {
	return func(s *Sender) error {
		s.settingEngine.SetVNet(v)
		s.settingEngine.SetICETimeouts(time.Second, time.Second, 200*time.Millisecond)
		s.settingEngine.SetNAT1To1IPs(publicIPs, webrtc.ICECandidateTypeHost)
		return nil
	}
}

func SetMediaSource(source MediaSource) Option {
	return func(s *Sender) error {
		s.source = source
		return nil
	}
}
