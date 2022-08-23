package receiver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/stats"
	"github.com/pion/logging"
	"github.com/pion/transport/vnet"
	"github.com/pion/webrtc/v3"
)

type Receiver struct {
	settingEngine *webrtc.SettingEngine
	mediaEngine   *webrtc.MediaEngine

	peerConnection *webrtc.PeerConnection

	registry *interceptor.Registry
	stats    stats.Getter
	ssrcs    []uint32

	log logging.LeveledLogger

	lock sync.Mutex
}

func New(opts ...Option) (*Receiver, error) {
	r := &Receiver{
		settingEngine:  &webrtc.SettingEngine{},
		mediaEngine:    &webrtc.MediaEngine{},
		peerConnection: &webrtc.PeerConnection{},
		registry:       &interceptor.Registry{},
		stats:          nil,
		ssrcs:          []uint32{},
		log:            logging.NewDefaultLoggerFactory().NewLogger("receiver"),
		lock:           sync.Mutex{},
	}
	if err := r.mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}
	statsInterceptor, err := stats.NewInterceptor()
	if err != nil {
		return nil, err
	}
	r.registry.Add(statsInterceptor)
	statsInterceptor.OnNewPeerConnection(func(_ string, sg stats.Getter) {
		r.stats = sg
	})
	for _, opt := range opts {
		if err := opt(r); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *Receiver) Close() error {
	return r.peerConnection.Close()
}

func (r *Receiver) GetStats() map[uint32]*stats.Stats {
	r.lock.Lock()
	defer r.lock.Unlock()

	result := make(map[uint32]*stats.Stats, len(r.ssrcs))
	for _, ssrc := range r.ssrcs {
		result[ssrc] = r.stats.Get(ssrc)
	}
	return result
}

func (r *Receiver) SetupPeerConnection() error {
	peerConnection, err := webrtc.NewAPI(
		webrtc.WithSettingEngine(*r.settingEngine),
		webrtc.WithInterceptorRegistry(r.registry),
		webrtc.WithMediaEngine(r.mediaEngine),
	).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return err
	}

	// Set the handler for ICE connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		r.log.Infof("Receiver Connection State has changed %s \n", connectionState.String())
	})

	// Set the handler for Peer connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		r.log.Infof("Receiver Peer Connection State has changed: %s\n", s.String())
	})

	peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
		r.log.Infof("Receiver candidate: %v\n", i)
	})

	peerConnection.OnTrack(r.onTrack)

	r.peerConnection = peerConnection
	return nil
}

func (r *Receiver) AcceptOffer(offer *webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	if err := r.peerConnection.SetRemoteDescription(*offer); err != nil {
		return nil, err
	}

	answer, err := r.peerConnection.CreateAnswer(nil)
	if err != nil {
		return nil, err
	}

	gatherComplete := webrtc.GatheringCompletePromise(r.peerConnection)
	if err = r.peerConnection.SetLocalDescription(answer); err != nil {
		return nil, err
	}
	<-gatherComplete

	return &answer, nil
}

func (r *Receiver) onTrack(trackRemote *webrtc.TrackRemote, rtpReceiver *webrtc.RTPReceiver) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bytesReceivedChan := make(chan int)

	r.lock.Lock()
	r.ssrcs = append(r.ssrcs, uint32(trackRemote.SSRC()))
	r.lock.Unlock()

	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if err := rtpReceiver.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				r.log.Infof("failed to SetReadDeadline for rtpReceiver: %v", err)
			}
			_, _, err := rtpReceiver.Read(rtcpBuf)
			if err != nil {
				r.log.Infof("failed to read RTCP from rtpReceiver: %v", err)
			}
		}
	}()

	go func(ctx context.Context) {
		bytesReceived := 0
		ticker := time.NewTicker(time.Second)
		last := time.Now()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				delta := now.Sub(last)
				bits := float64(bytesReceived) * 8.0
				rate := bits / delta.Seconds()
				mBitPerSecond := rate / float64(vnet.MBit)
				r.log.Infof("throughput: %.2f Mb/s\n", mBitPerSecond)
				bytesReceived = 0
				last = now
			case newBytesReceived := <-bytesReceivedChan:
				bytesReceived += newBytesReceived
			}
		}
	}(ctx)
	for {
		if err := trackRemote.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			r.log.Infof("failed to SetReadDeadline for trackRemote: %v", err)
		}

		p, _, err := trackRemote.ReadRTP()
		if err == io.EOF {
			r.log.Infof("trackRemote.ReadRTP received EOF")
			return
		}
		if err != nil {
			r.log.Infof("trackRemote.ReadRTP returned error: %v\n", err)
			continue
		}
		bytesReceivedChan <- p.MarshalSize()
	}
}

func (r *Receiver) SDPHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		sdp := webrtc.SessionDescription{}
		if err := json.NewDecoder(req.Body).Decode(&sdp); err != nil {
			panic(err)
		}
		answer, err := r.AcceptOffer(&sdp)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// Send our answer to the HTTP server listening in the other process
		payload, err := json.Marshal(answer)
		if err != nil {
			panic(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(payload); err != nil {
			r.log.Errorf("failed to write signaling response: %v", err)
		}
	})
}
