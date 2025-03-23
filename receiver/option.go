// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package receiver implements WebRTC receiver functionality for bandwidth estimation tests.
package receiver

import (
	"io"
	"time"

	"github.com/pion/bwe-test/logging"
	"github.com/pion/interceptor/pkg/packetdump"
	plogging "github.com/pion/logging"
	"github.com/pion/transport/v3/vnet"
	"github.com/pion/webrtc/v4"
)

// Option is a function that configures a Receiver.
type Option func(*Receiver) error

// PacketLogWriter returns an Option that configures RTP and RTCP packet logging.
func PacketLogWriter(rtpWriter, rtcpWriter io.Writer) Option {
	return func(receiver *Receiver) error {
		formatter := logging.RTPFormatter{}
		rtpLogger, err := packetdump.NewReceiverInterceptor(
			packetdump.RTPFormatter(formatter.RTPFormat),
			packetdump.RTPWriter(rtpWriter),
		)
		if err != nil {
			return err
		}
		rtcpLogger, err := packetdump.NewSenderInterceptor(
			packetdump.RTCPFormatter(logging.RTCPFormat),
			packetdump.RTCPWriter(rtcpWriter),
		)
		if err != nil {
			return err
		}
		receiver.registry.Add(rtpLogger)
		receiver.registry.Add(rtcpLogger)

		return nil
	}
}

// DefaultInterceptors returns an Option that registers the default WebRTC interceptors.
func DefaultInterceptors() Option {
	return func(r *Receiver) error {
		return webrtc.RegisterDefaultInterceptors(r.mediaEngine, r.registry)
	}
}

// SetVnet returns an Option that configures the virtual network for testing.
func SetVnet(v *vnet.Net, publicIPs []string) Option {
	return func(r *Receiver) error {
		r.settingEngine.SetNet(v)
		r.settingEngine.SetICETimeouts(time.Second, time.Second, 200*time.Millisecond)
		r.settingEngine.SetNAT1To1IPs(publicIPs, webrtc.ICECandidateTypeHost)

		return nil
	}
}

// SetLoggerFactory returns an Option that configures the logger factory.
func SetLoggerFactory(loggerFactory plogging.LoggerFactory) Option {
	return func(s *Receiver) error {
		s.settingEngine.LoggerFactory = loggerFactory
		s.log = loggerFactory.NewLogger("receiver")

		return nil
	}
}
