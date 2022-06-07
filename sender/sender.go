package sender

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/pion/bwe-test/logging"
	"github.com/pion/interceptor/pkg/packetdump"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
	"github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

type MediaSource interface {
	SetTargetBitrate(int)
	SetWriter(func(sample media.Sample) error)
	Start() error
	io.Closer
}

type Sender struct {
	settingEngine *webrtc.SettingEngine

	peerConnection *webrtc.PeerConnection
	videoTrack     *webrtc.TrackLocalStaticSample

	source    MediaSource
	estimator cc.BandwidthEstimator

	rtpWriter io.Writer
	done      chan struct{}
}

func NewSender(source MediaSource, opts ...Option) (*Sender, error) {
	sender := &Sender{
		settingEngine: &webrtc.SettingEngine{},
		source:        source,
		rtpWriter:     io.Discard,
		done:          make(chan struct{}),
	}
	for _, opt := range opts {
		if err := opt(sender); err != nil {
			return nil, err
		}
	}

	return sender, nil
}

func (s *Sender) SetupPeerConnection() error {
	i := &interceptor.Registry{}
	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		return err
	}

	// Create a Congestion Controller. This analyzes inbound and outbound data and provides
	// suggestions on how much we should be sending.
	//
	// Passing `nil` means we use the default Estimation Algorithm which is Google Congestion Control.
	// You can use the other ones that Pion provides, or write your own!
	congestionController, err := cc.NewInterceptor(func() (cc.BandwidthEstimator, error) {
		return gcc.NewSendSideBWE(gcc.SendSideBWEInitialBitrate(initialBitrate))
	})
	if err != nil {
		return err
	}

	estimatorChan := make(chan cc.BandwidthEstimator, 1)
	congestionController.OnNewPeerConnection(func(_ string, estimator cc.BandwidthEstimator) {
		estimatorChan <- estimator
	})

	i.Add(congestionController)
	if err = webrtc.ConfigureTWCCHeaderExtensionSender(m, i); err != nil {
		return err
	}

	if err = webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		return err
	}

	rtpLogger, err := packetdump.NewSenderInterceptor(
		packetdump.RTPFormatter(logging.RTPFormat),
		packetdump.RTPWriter(s.rtpWriter),
	)
	if err != nil {
		return err
	}
	i.Add(rtpLogger)

	// Create a new RTCPeerConnection
	peerConnection, err := webrtc.NewAPI(
		webrtc.WithSettingEngine(*s.settingEngine),
		webrtc.WithInterceptorRegistry(i),
		webrtc.WithMediaEngine(m),
	).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return err
	}
	s.peerConnection = peerConnection

	// Wait until our Bandwidth Estimator has been created
	s.estimator = <-estimatorChan

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
		fmt.Printf("Sender Connection State has changed %s \n", connectionState.String())
	})
	// Set the handler for Peer connection state
	// This will notify you when the peer has connected/disconnected
	s.peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		fmt.Printf("Sender Peer Connection State has changed: %s\n", s.String())
	})
	peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
		fmt.Printf("Sender candidate: %v\n", i)
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
	fmt.Printf("Sender gatherComplete: %v\n", s.peerConnection.ICEGatheringState())
	//fmt.Printf("Sender Offer SDP: \n%v", offer.SDP)

	return s.peerConnection.LocalDescription(), nil
}

func (s *Sender) AcceptAnswer(answer *webrtc.SessionDescription) error {
	// Sets the LocalDescription, and starts our UDP listeners
	return s.peerConnection.SetRemoteDescription(*answer)
}

func (s *Sender) Start() error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	lastLog := time.Now()
	lastBitrate := initialBitrate

	s.source.SetWriter(s.videoTrack.WriteSample)

	go func() {
		for {
			select {
			case now := <-ticker.C:
				targetBitrate := s.estimator.GetTargetBitrate()
				if now.Sub(lastLog) >= time.Second {
					fmt.Printf("targetBitrate = %v\n", targetBitrate)
					lastLog = now
				}
				if lastBitrate != targetBitrate {
					s.source.SetTargetBitrate(targetBitrate)
					lastBitrate = targetBitrate
				}
				//fmt.Println(s.estimator.GetStats())
			case <-s.done:
				return
			}
		}
	}()

	return s.source.Start()
}

// Close closes the sender
// TODO: How to handle multiple errors properly?
func (s *Sender) Close() error {
	close(s.done)
	return s.peerConnection.Close()
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
	log.Printf("connecting to '%v'\n", url)
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
