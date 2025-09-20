// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

// Package sender implements WebRTC sender functionality for bandwidth estimation tests.
package sender

import (
	"io"
	"time"

	"github.com/pion/bwe-test/logging"
	"github.com/pion/interceptor/pkg/cc"
	"github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/interceptor/pkg/packetdump"
	plogging "github.com/pion/logging"
	"github.com/pion/transport/v3/vnet"
	"github.com/pion/webrtc/v4"
)

// Option is a function that configures a Sender.
type Option func(*Sender) error

// PacketLogWriter returns an Option that configures RTP and RTCP packet logging.
func PacketLogWriter(rtpWriter, rtcpWriter io.Writer) Option {
	return func(sndr *Sender) error {
		formatter := logging.RTPFormatter{}
		rtpLogger, err := packetdump.NewSenderInterceptor(
			packetdump.RTPFormatter(formatter.RTPFormat),
			packetdump.RTPWriter(rtpWriter),
		)
		if err != nil {
			return err
		}
		rtcpLogger, err := packetdump.NewReceiverInterceptor(
			packetdump.RTCPFormatter(logging.RTCPFormat),
			packetdump.RTCPWriter(rtcpWriter),
		)
		if err != nil {
			return err
		}
		sndr.registry.Add(rtpLogger)
		sndr.registry.Add(rtcpLogger)

		return nil
	}
}

// DefaultInterceptors returns an Option that registers the default WebRTC interceptors.
func DefaultInterceptors() Option {
	return func(s *Sender) error {
		return webrtc.RegisterDefaultInterceptors(s.mediaEngine, s.registry)
	}
}

// CCLogWriter returns an Option that configures congestion control logging.
func CCLogWriter(w io.Writer) Option {
	return func(s *Sender) error {
		s.ccLogWriter = w

		return nil
	}
}

// GCC returns an Option that configures Google Congestion Control with the specified initial bitrate.
func GCC(initialBitrate int) Option {
	return func(sndr *Sender) error {
		controller, err := cc.NewInterceptor(func() (cc.BandwidthEstimator, error) {
			return gcc.NewSendSideBWE(gcc.SendSideBWEInitialBitrate(initialBitrate))
		})
		if err != nil {
			return err
		}
		controller.OnNewPeerConnection(func(_ string, estimator cc.BandwidthEstimator) {
			go func() {
				sndr.estimatorChan <- estimator
			}()
		})
		sndr.registry.Add(controller)
		if err = webrtc.ConfigureTWCCHeaderExtensionSender(sndr.mediaEngine, sndr.registry); err != nil {
			return err
		}

		return nil
	}
}

// SetVnet returns an Option that configures the virtual network for testing.
func SetVnet(v *vnet.Net, publicIPs []string) Option {
	return func(s *Sender) error {
		s.settingEngine.SetNet(v)
		s.settingEngine.SetICETimeouts(time.Second, time.Second, 200*time.Millisecond)
		s.settingEngine.SetNAT1To1IPs(publicIPs, webrtc.ICECandidateTypeHost)

		return nil
	}
}

// SetMediaSource returns an Option that sets the media source for the sender.
func SetMediaSource(source MediaSource) Option {
	return func(s *Sender) error {
		s.source = source

		return nil
	}
}

// SetLoggerFactory returns an Option that configures the logger factory.
func SetLoggerFactory(loggerFactory plogging.LoggerFactory) Option {
	return func(s *Sender) error {
		s.settingEngine.LoggerFactory = loggerFactory
		s.log = loggerFactory.NewLogger("sender")

		return nil
	}
}
