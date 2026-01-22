// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

// Package sender implements WebRTC sender functionality for bandwidth estimation tests.
package sender

import (
	"io"
	"time"

	"github.com/pion/bwe-test/logging"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
	"github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/interceptor/pkg/packetdump"
	plogging "github.com/pion/logging"
	"github.com/pion/transport/v3/vnet"
	"github.com/pion/webrtc/v4"
)

// ConfigurableWebRTCSender defines the interface that both Sender and RTCSender implement
// to allow shared option configuration.
type ConfigurableWebRTCSender interface {
	GetSettingEngine() *webrtc.SettingEngine
	GetMediaEngine() *webrtc.MediaEngine
	GetRegistry() *interceptor.Registry
	GetEstimatorChan() chan cc.BandwidthEstimator
	SetLogger(plogging.LeveledLogger)
	SetCCLogWriter(io.Writer) // For Sender compatibility
}

// Option is a function that configures a ConfigurableWebRTCSender.
type Option func(ConfigurableWebRTCSender) error

// PacketLogWriter returns an Option that configures RTP and RTCP packet logging.
func PacketLogWriter(rtpWriter, rtcpWriter io.Writer) Option {
	return func(sender ConfigurableWebRTCSender) error {
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
		sender.GetRegistry().Add(rtpLogger)
		sender.GetRegistry().Add(rtcpLogger)

		return nil
	}
}

// DefaultInterceptors returns an Option that registers the default WebRTC interceptors.
func DefaultInterceptors() Option {
	return func(sender ConfigurableWebRTCSender) error {
		return webrtc.RegisterDefaultInterceptors(sender.GetMediaEngine(), sender.GetRegistry())
	}
}

// CCLogWriter returns an Option that configures congestion control logging.
func CCLogWriter(w io.Writer) Option {
	return func(sender ConfigurableWebRTCSender) error {
		sender.SetCCLogWriter(w)

		return nil
	}
}

// GCC returns an Option that configures Google Congestion Control with the specified initial bitrate.
func GCC(initialBitrate int) Option {
	return func(sender ConfigurableWebRTCSender) error {
		controller, err := cc.NewInterceptor(func() (cc.BandwidthEstimator, error) {
			return gcc.NewSendSideBWE(gcc.SendSideBWEInitialBitrate(initialBitrate))
		})
		if err != nil {
			return err
		}
		controller.OnNewPeerConnection(func(_ string, estimator cc.BandwidthEstimator) {
			go func() {
				sender.GetEstimatorChan() <- estimator
			}()
		})
		sender.GetRegistry().Add(controller)
		if err = webrtc.ConfigureTWCCHeaderExtensionSender(sender.GetMediaEngine(), sender.GetRegistry()); err != nil {
			return err
		}

		return nil
	}
}

// SetVnet returns an Option that configures the virtual network for testing.
func SetVnet(v *vnet.Net, publicIPs []string) Option {
	return func(sender ConfigurableWebRTCSender) error {
		settingEngine := sender.GetSettingEngine()
		settingEngine.SetNet(v)
		settingEngine.SetICETimeouts(time.Second, time.Second, 200*time.Millisecond)
		settingEngine.SetNAT1To1IPs(publicIPs, webrtc.ICECandidateTypeHost)

		return nil
	}
}

// SetMediaSource returns an Option that sets the media source for the sender.
// Note: This only works with the original Sender type, not RTCSender.
func SetMediaSource(source MediaSource) Option {
	return func(sender ConfigurableWebRTCSender) error {
		if s, ok := sender.(*Sender); ok {
			s.source = source
		}
		// Silently ignore for RTCSender since it manages tracks differently
		return nil
	}
}

// SetLoggerFactory returns an Option that configures the logger factory.
func SetLoggerFactory(loggerFactory plogging.LoggerFactory) Option {
	return func(sender ConfigurableWebRTCSender) error {
		settingEngine := sender.GetSettingEngine()
		settingEngine.LoggerFactory = loggerFactory
		sender.SetLogger(loggerFactory.NewLogger("sender"))

		return nil
	}
}
