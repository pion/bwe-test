// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js

// Package sender implements WebRTC sender functionality for bandwidth estimation tests.
package sender

import (
	"errors"
	"io"
	"time"

	"github.com/pion/bwe-test/logging"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
	"github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/interceptor/pkg/packetdump"
	plogging "github.com/pion/logging"
	"github.com/pion/transport/v4/vnet"
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
			packetdump.RTPBinaryFormatter(formatter.RTPFormat),
			packetdump.RTPWriter(rtpWriter),
		)
		if err != nil {
			return err
		}
		rtcpLogger, err := packetdump.NewReceiverInterceptor(
			packetdump.RTCPBinaryFormatter(logging.RTCPFormat),
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

var errCaptureTimestampUnsupported = errors.New(
	"CaptureTimestampRewrite requires an *RTCSender")

// CaptureTimestampRewrite returns an Option that encodes each frame's capture
// time (supplied via SetCaptureTSUs) into the outgoing RTP timestamp, so the
// capture instant survives an SFU that strips header extensions on egress.
//
// Off by default, and unsafe for tracks whose sending is gated outside the RTP
// stack: a track that is idle and then starts sending mid-session begins with a
// timestamp base derived from wall time, and receivers do not render it. There
// is no way to both preserve the packetizer's timeline and carry an absolute
// capture instant in the same field, so prefer an out-of-band channel (e.g. a
// data channel keyed by RTP timestamp) when tracks may start or stop.
func CaptureTimestampRewrite() Option {
	return func(sender ConfigurableWebRTCSender) error {
		rtcSender, ok := sender.(*RTCSender)
		if !ok {
			return errCaptureTimestampUnsupported
		}
		rtcSender.captureTimestamp.SetRewriteEnabled(true)

		return nil
	}
}

// CCLogWriter returns an Option that configures congestion control logging.
func CCLogWriter(w io.Writer) Option {
	return func(sender ConfigurableWebRTCSender) error {
		sender.SetCCLogWriter(w)

		return nil
	}
}

// GCC returns an Option that configures Google Congestion Control with the
// specified initial bitrate and max bitrate (in bps). A maxBitrate of 0 means
// no cap (uses GCC default of 50 Mbps).
// GCC configures send-side bandwidth estimation. extra is passed through to the
// estimator, so a caller can set options the two bitrate arguments do not cover.
func GCC(initialBitrate, maxBitrate int, extra ...gcc.Option) Option {
	return func(sender ConfigurableWebRTCSender) error {
		if rtcSender, ok := sender.(*RTCSender); ok {
			rtcSender.gccConfigured = true

			return rtcSender.setupGCC(initialBitrate, maxBitrate, extra...)
		}
		// Fallback for other ConfigurableWebRTCSender types.
		controller, err := cc.NewInterceptor(newGCCFactory(initialBitrate, maxBitrate, extra...))
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
		if err := settingEngine.SetICEAddressRewriteRules(webrtc.ICEAddressRewriteRule{
			External:        publicIPs,
			AsCandidateType: webrtc.ICECandidateTypeHost,
		}); err != nil {
			return err
		}

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
