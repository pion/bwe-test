// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
	"github.com/pion/logging"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"golang.org/x/sync/errgroup"
)

type MediaSource interface {
	SetTargetBitrate(int)
	SetWriter(func(sample media.Sample) error)
	Start(ctx context.Context) error
}

type Sender struct {
	settingEngine *webrtc.SettingEngine
	mediaEngine   *webrtc.MediaEngine

	peerConnection *webrtc.PeerConnection
	videoTrack     *webrtc.TrackLocalStaticSample

	source        MediaSource
	estimator     cc.BandwidthEstimator
	estimatorChan chan cc.BandwidthEstimator

	registry *interceptor.Registry

	ccLogWriter io.Writer

	log logging.LeveledLogger
}

func NewSender(source MediaSource, opts ...Option) (*Sender, error) {
	sender := &Sender{
		settingEngine:  &webrtc.SettingEngine{},
		mediaEngine:    &webrtc.MediaEngine{},
		peerConnection: nil,
		videoTrack:     nil,
		source:         source,
		estimator:      nil,
		estimatorChan:  make(chan cc.BandwidthEstimator),
		registry:       &interceptor.Registry{},
		ccLogWriter:    io.Discard,
		log:            logging.NewDefaultLoggerFactory().NewLogger("sender"),
	}
	if err := sender.mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}
	for _, opt := range opts {
		if err := opt(sender); err != nil {
			return nil, err
		}
	}

	return sender, nil
}

func (s *Sender) SetupPeerConnection() error {
	// Create a new RTCPeerConnection
	peerConnection, err := webrtc.NewAPI(
		webrtc.WithSettingEngine(*s.settingEngine),
		webrtc.WithInterceptorRegistry(s.registry),
		webrtc.WithMediaEngine(s.mediaEngine),
	).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return err
	}
	s.peerConnection = peerConnection

	// Create a video track
	videoTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "pion")
	if err != nil {
		return err
	}
	s.videoTrack = videoTrack

	rtpSender, err := s.peerConnection.AddTrack(s.videoTrack)
	if err != nil {
		return err
	}

	// Read incoming RTCP packets
	// Before these packets are returned they are processed by interceptors. For things
	// like NACK this needs to be called.
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

	// Set the handler for ICE connection state
	// This will notify you when the peer has connected/disconnected
	s.peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		s.log.Infof("Sender Connection State has changed %s \n", connectionState.String())
	})
	// Set the handler for Peer connection state
	// This will notify you when the peer has connected/disconnected
	s.peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		s.log.Infof("Sender Peer Connection State has changed: %s\n", state.String())
	})
	peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
		s.log.Infof("Sender candidate: %v\n", i)
	})
	return nil
}

func (s *Sender) CreateOffer() (*webrtc.SessionDescription, error) {
	if s.peerConnection == nil {
		return nil, fmt.Errorf("no PeerConnection created")
	}
	offer, err := s.peerConnection.CreateOffer(nil)
	if err != nil {
		return nil, err
	}

	// Create channel that is blocked until ICE Gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(s.peerConnection)
	if err = s.peerConnection.SetLocalDescription(offer); err != nil {
		return nil, err
	}
	// Block until ICE Gathering is complete, disabling trickle ICE
	// we do this because we only can exchange one signaling message
	// in a production application you should exchange ICE Candidates via OnICECandidate
	<-gatherComplete
	s.log.Infof("Sender gatherComplete: %v\n", s.peerConnection.ICEGatheringState())

	return s.peerConnection.LocalDescription(), nil
}

func (s *Sender) AcceptAnswer(answer *webrtc.SessionDescription) error {
	// Sets the LocalDescription, and starts our UDP listeners
	return s.peerConnection.SetRemoteDescription(*answer)
}

func (s *Sender) Start(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	lastLog := time.Now()
	lastBitrate := initialBitrate

	s.source.SetWriter(s.videoTrack.WriteSample)

	wg, ctx := errgroup.WithContext(ctx)

	wg.Go(func() error {
		var estimator cc.BandwidthEstimator
		select {
		case estimator = <-s.estimatorChan:
		case <-ctx.Done():
			return nil
		}
		for {
			select {
			case now := <-ticker.C:
				targetBitrate := estimator.GetTargetBitrate()
				if now.Sub(lastLog) >= time.Second {
					s.log.Infof("targetBitrate = %v\n", targetBitrate)
					lastLog = now
				}
				if lastBitrate != targetBitrate {
					s.source.SetTargetBitrate(targetBitrate)
					lastBitrate = targetBitrate
				}
				fmt.Fprintf(s.ccLogWriter, "%v, %v\n", now.UnixMilli(), targetBitrate)
			case <-ctx.Done():
				return nil
			}
		}
	})

	wg.Go(func() error {
		return s.source.Start(ctx)
	})

	defer s.peerConnection.Close()
	return wg.Wait()
}

func (s *Sender) SignalHTTP(addr, route string) error {
	offer, err := s.CreateOffer()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(offer)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s/%s", addr, route)
	s.log.Infof("connecting to '%v'\n", url)
	resp, err := http.Post(url, "application/json; charset=utf-8", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("signaling received unexpected status code: %v: %v", resp.StatusCode, resp.Status)
	}
	answer := webrtc.SessionDescription{}
	if sdpErr := json.NewDecoder(resp.Body).Decode(&answer); sdpErr != nil {
		panic(sdpErr)
	}

	return s.AcceptAnswer(&answer)
}
