package sender

import (
	"io"
	"time"

	"github.com/pion/bwe-test/logging"
	"github.com/pion/interceptor/nada/pkg/nada"
	"github.com/pion/interceptor/pkg/cc"
	"github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/interceptor/pkg/packetdump"
	"github.com/pion/transport/vnet"
	"github.com/pion/webrtc/v3"
)

type Option func(*Sender) error

func PacketLogWriter(outboundRTPWriter, outboundRTCPWriter, inboundRTCPWriter io.Writer) Option {
	return func(s *Sender) error {
		outboundFormatter := logging.RTPFormatter{}
		rtpLogger, err := packetdump.NewSenderInterceptor(
			packetdump.RTPFormatter(outboundFormatter.RTPFormat),
			packetdump.RTCPFormatter(logging.RTCPFormat),
			packetdump.RTPWriter(outboundRTPWriter),
			packetdump.RTCPWriter(outboundRTCPWriter),
		)
		if err != nil {
			return err
		}
		rtcpLogger, err := packetdump.NewReceiverInterceptor(
			packetdump.RTCPFormatter(logging.RTCPFormat),
			packetdump.RTCPWriter(inboundRTCPWriter),
		)
		if err != nil {
			return err
		}
		s.registry.Add(rtpLogger)
		s.registry.Add(rtcpLogger)
		return nil
	}
}

func DefaultInterceptors() Option {
	return func(s *Sender) error {
		return webrtc.RegisterDefaultInterceptors(s.mediaEngine, s.registry)
	}
}

func NACK() Option {
	return func(s *Sender) error {
		return webrtc.ConfigureNack(s.mediaEngine, s.registry)
	}
}

func RTCPReports() Option {
	return func(s *Sender) error {
		return webrtc.ConfigureRTCPReports(s.registry)
	}
}

func CCLogWriter(w io.Writer) Option {
	return func(s *Sender) error {
		s.ccLogWriter = w
		return nil
	}
}

func NADA() Option {
	return func(s *Sender) error {
		controller, err := cc.NewInterceptor(func() (cc.BandwidthEstimator, error) {
			return nada.NewBandwidthEstimator(), nil
		})
		if err != nil {
			return err
		}
		controller.OnNewPeerConnection(func(_ string, estimator cc.BandwidthEstimator) {
			go func() {
				s.estimatorChan <- estimator
			}()
		})
		s.registry.Add(controller)
		return nil
	}
}

func GCC(initialBitrate, minBitrate, maxBitrate int, twcc bool) Option {
	return func(s *Sender) error {
		controller, err := cc.NewInterceptor(func() (cc.BandwidthEstimator, error) {
			return gcc.NewSendSideBWE(
				gcc.SendSideBWEInitialBitrate(initialBitrate),
				gcc.SendSideBWEMinBitrate(minBitrate),
				gcc.SendSideBWEMaxBitrate(maxBitrate),
			)
		})
		if err != nil {
			return err
		}
		controller.OnNewPeerConnection(func(_ string, estimator cc.BandwidthEstimator) {
			go func() {
				s.estimatorChan <- estimator
			}()
		})
		s.registry.Add(controller)

		if twcc {
			if err = webrtc.ConfigureTWCCHeaderExtensionSender(s.mediaEngine, s.registry); err != nil {
				return err
			}
		}
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
