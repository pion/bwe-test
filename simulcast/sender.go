package simulcast

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
	"github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/interceptor/pkg/packetdump"
	"github.com/pion/rtp"
	"github.com/pion/transport/vnet"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/ivfreader"
)

const (
	lowFile    = "low.ivf"
	lowBitrate = 300_000

	medFile    = "med.ivf"
	medBitrate = 1_000_000

	highFile    = "high.ivf"
	highBitrate = 2_500_000

	ivfHeaderSize = 32
)

type Sender struct {
	qualityLevels []struct {
		fileName string
		bitrate  int
	}
	currentQualityLevel int
	settingEngine       *webrtc.SettingEngine

	peerConnection *webrtc.PeerConnection
	videoTrack     *webrtc.TrackLocalStaticSample
	estimator      cc.BandwidthEstimator
	rtpWriter      io.Writer
	done           chan struct{}
}

func NewSender() (*Sender, error) {
	sender := &Sender{
		qualityLevels: []struct {
			fileName string
			bitrate  int
		}{
			{lowFile, lowBitrate},
			{medFile, medBitrate},
			{highFile, highBitrate},
		},
		currentQualityLevel: 0,
		settingEngine:       &webrtc.SettingEngine{},
		rtpWriter:           io.Discard,
		done:                make(chan struct{}),
	}

	for _, level := range sender.qualityLevels {
		_, err := os.Stat(level.fileName)
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file %s was not found", level.fileName)
		}
	}

	return sender, nil
}

func (s *Sender) SetVnet(v *vnet.Net, publicIPs []string) {
	s.settingEngine.SetVNet(v)
	s.settingEngine.SetICETimeouts(time.Second, time.Second, 200*time.Millisecond)
	s.settingEngine.SetNAT1To1IPs(publicIPs, webrtc.ICECandidateTypeHost)
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
		return gcc.NewSendSideBWE(gcc.SendSideBWEInitialBitrate(lowBitrate))
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
		packetdump.RTPFormatter(rtpFormat),
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
	// Open a IVF file and start reading using our IVFReader
	file, err := os.Open(s.qualityLevels[s.currentQualityLevel].fileName)
	if err != nil {
		return err
	}

	ivf, header, err := ivfreader.NewWith(file)
	if err != nil {
		return err
	}

	// Send our video file frame at a time. Pace our sending so we send it at the same speed it should be played back as.
	// This isn't required since the video is timestamped, but we will such much higher loss if we send all at once.
	//
	// It is important to use a time.Ticker instead of time.Sleep because
	// * avoids accumulating skew, just calling time.Sleep didn't compensate for the time spent parsing the data
	// * works around latency issues with Sleep (see https://github.com/golang/go/issues/44343)
	ticker := time.NewTicker(time.Millisecond * time.Duration((float32(header.TimebaseNumerator)/float32(header.TimebaseDenominator))*1000))
	frame := []byte{}
	frameHeader := &ivfreader.IVFFrameHeader{}
	currentTimestamp := uint64(0)

	switchQualityLevel := func(newQualityLevel int) {
		fmt.Printf("Switching from %s to %s \n", s.qualityLevels[s.currentQualityLevel].fileName, s.qualityLevels[newQualityLevel].fileName)
		s.currentQualityLevel = newQualityLevel
		ivf.ResetReader(setReaderFile(s.qualityLevels[s.currentQualityLevel].fileName))
		for {
			if frame, frameHeader, err = ivf.ParseNextFrame(); err != nil {
				break
			} else if frameHeader.Timestamp >= currentTimestamp && frame[0]&0x1 == 0 {
				break
			}
		}
	}

	lastLog := time.Now()
	for {
		select {
		case now := <-ticker.C:
			targetBitrate := s.estimator.GetTargetBitrate()
			if now.Sub(lastLog) >= time.Second {
				fmt.Printf("targetBitrate = %v\n", targetBitrate)
				lastLog = now
			}
			switch {
			// If current quality level is below target bitrate drop to level below
			case s.currentQualityLevel != 0 && targetBitrate < s.qualityLevels[s.currentQualityLevel].bitrate:
				fmt.Printf("targetBitrate = %v\n", targetBitrate)
				switchQualityLevel(s.currentQualityLevel - 1)

				// If next quality level is above target bitrate move to next level
			case len(s.qualityLevels) > (s.currentQualityLevel+1) && targetBitrate > s.qualityLevels[s.currentQualityLevel+1].bitrate:
				fmt.Printf("targetBitrate = %v\n", targetBitrate)
				switchQualityLevel(s.currentQualityLevel + 1)

			// Adjust outbound bandwidth for probing
			default:
				frame, _, err = ivf.ParseNextFrame()
			}

			switch err {
			// No error write the video frame
			case nil:
				currentTimestamp = frameHeader.Timestamp
				if err = s.videoTrack.WriteSample(media.Sample{Data: frame, Duration: time.Second}); err != nil {
					return err
				}
			// If we have reached the end of the file start again
			case io.EOF:
				ivf.ResetReader(setReaderFile(s.qualityLevels[s.currentQualityLevel].fileName))
			// Error besides io.EOF that we dont know how to handle
			default:
				return err
			}
		case <-s.done:
			return nil
		}
	}
}

func (s *Sender) Close() error {
	if err := s.peerConnection.Close(); err != nil {
		fmt.Println(err)
	}
	close(s.done)
	return nil
}

func setReaderFile(filename string) func(_ int64) io.Reader {
	return func(_ int64) io.Reader {
		file, err := os.Open(filename) // nolint
		if err != nil {
			panic(err)
		}
		if _, err = file.Seek(ivfHeaderSize, io.SeekStart); err != nil {
			panic(err)
		}
		return file
	}
}

func rtpFormat(pkt *rtp.Packet, attributes interceptor.Attributes) string {
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
