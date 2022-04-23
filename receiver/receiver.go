package receiver

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/pion/bwe-test/stats"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/packetdump"
	"github.com/pion/rtp"
	"github.com/pion/transport/vnet"
	"github.com/pion/webrtc/v3"
)

type Receiver struct {
	settingEngine  *webrtc.SettingEngine
	peerConnection *webrtc.PeerConnection
	rtpWriter      io.Writer

	statsServer *stats.Server
}

func NewReceiver() *Receiver {
	r := &Receiver{
		settingEngine: &webrtc.SettingEngine{},
		rtpWriter:     io.Discard,
		statsServer:   stats.New(),
	}
	go r.statsServer.Start()
	return r
}

func (r *Receiver) Close() error {
	if err := r.peerConnection.Close(); err != nil {
		fmt.Println(err)
	}
	return r.statsServer.Shutdown(context.Background())
}

func (r *Receiver) SetVnet(v *vnet.Net, publicIPs []string) {
	r.settingEngine.SetVNet(v)
	r.settingEngine.SetICETimeouts(time.Second, time.Second, 200*time.Millisecond)
	r.settingEngine.SetNAT1To1IPs(publicIPs, webrtc.ICECandidateTypeHost)
}

func (r *Receiver) SetupPeerConnection() error {
	i := &interceptor.Registry{}
	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		return err
	}

	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		return err
	}

	rtpLogger, err := packetdump.NewReceiverInterceptor(
		packetdump.RTPFormatter(rtpFormat),
		packetdump.RTPWriter(r.rtpWriter),
	)
	if err != nil {
		return err
	}
	i.Add(rtpLogger)

	peerConnection, err := webrtc.NewAPI(
		webrtc.WithSettingEngine(*r.settingEngine),
		webrtc.WithInterceptorRegistry(i),
		webrtc.WithMediaEngine(m),
	).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return err
	}

	// Set the handler for ICE connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("Receiver Connection State has changed %s \n", connectionState.String())
	})

	// Set the handler for Peer connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		fmt.Printf("Receiver Peer Connection State has changed: %s\n", s.String())
	})

	peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
		fmt.Printf("Receiver candidate: %v\n", i)
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

	go func(ctx context.Context) {
		bytesReceived := 0
		ticker := time.NewTicker(time.Second)
		last := time.Now()
		start := last
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				delta := now.Sub(last)
				bits := float64(bytesReceived) * 8.0
				rate := bits / delta.Seconds()
				mBitPerSecond := rate / float64(vnet.MBit)
				fmt.Printf("throughput: %.2f Mb/s\n", mBitPerSecond)
				bytesReceived = 0
				last = now

				r.statsServer.Add(stats.DataPoint{
					Label:     "receiver_throughput",
					Timestamp: now.Sub(start).Milliseconds(),
					Value:     rate,
				})

			case newBytesReceived := <-bytesReceivedChan:
				bytesReceived += newBytesReceived
			}
		}
	}(ctx)
	for {
		if err := rtpReceiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			fmt.Printf("failed to SetReadDeadline for rtpReceiver: %v", err)
		}
		if err := trackRemote.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			fmt.Printf("failed to SetReadDeadline for trackRemote: %v", err)
		}

		p, _, err := trackRemote.ReadRTP()
		if err == io.EOF {
			fmt.Println("trackRemote.ReadRTP received EOF")
			return
		}
		if err != nil {
			fmt.Printf("trackRemote.ReadRTP returned error: %v\n", err)
			continue
		}
		bytesReceivedChan <- p.MarshalSize()
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
