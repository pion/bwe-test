// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

// Package receiver implements WebRTC receiver functionality for bandwidth estimation tests.
package receiver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/logging"
	"github.com/pion/transport/v3/vnet"
	"github.com/pion/webrtc/v4"
)

// Receiver manages a WebRTC connection for receiving media.
type Receiver struct {
	settingEngine *webrtc.SettingEngine
	mediaEngine   *webrtc.MediaEngine

	peerConnection *webrtc.PeerConnection

	registry *interceptor.Registry

	log logging.LeveledLogger
}

// NewReceiver creates a new WebRTC receiver with the given options.
func NewReceiver(opts ...Option) (*Receiver, error) {
	receiver := &Receiver{
		settingEngine:  &webrtc.SettingEngine{},
		mediaEngine:    &webrtc.MediaEngine{},
		peerConnection: &webrtc.PeerConnection{},
		registry:       &interceptor.Registry{},
		log:            logging.NewDefaultLoggerFactory().NewLogger("receiver"),
	}
	if err := receiver.mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}
	for _, opt := range opts {
		if err := opt(receiver); err != nil {
			return nil, err
		}
	}

	return receiver, nil
}

// Close stops and cleans up the receiver.
func (r *Receiver) Close() error {
	return r.peerConnection.Close()
}

// SetupPeerConnection initializes the WebRTC peer connection.
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
		r.log.Infof("Receiver Connection State has changed %s", connectionState.String())
	})

	// Set the handler for Peer connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		r.log.Infof("Receiver Peer Connection State has changed: %s", s.String())
	})

	peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
		r.log.Infof("Receiver candidate: %v", i)
	})

	peerConnection.OnTrack(r.onTrack)

	r.peerConnection = peerConnection

	return nil
}

// AcceptOffer processes a WebRTC offer from the remote peer and creates an answer.
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
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				delta := now.Sub(last)
				bits := float64(bytesReceived) * 8.0
				rate := bits / delta.Seconds()
				mBitPerSecond := rate / float64(vnet.MBit)
				r.log.Infof("throughput: %.2f Mb/s", mBitPerSecond)
				bytesReceived = 0
				last = now
			case newBytesReceived := <-bytesReceivedChan:
				bytesReceived += newBytesReceived
			}
		}
	}(ctx)
	for {
		if err := rtpReceiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			r.log.Infof("failed to SetReadDeadline for rtpReceiver: %v", err)
		}
		if err := trackRemote.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			r.log.Infof("failed to SetReadDeadline for trackRemote: %v", err)
		}

		p, _, err := trackRemote.ReadRTP()
		if errors.Is(err, io.EOF) {
			r.log.Infof("trackRemote.ReadRTP received EOF")

			return
		}
		if err != nil {
			r.log.Infof("trackRemote.ReadRTP returned error: %v", err)

			continue
		}
		bytesReceivedChan <- p.MarshalSize()
	}
}

// SDPHandler returns an HTTP handler for WebRTC signaling.
func (r *Receiver) SDPHandler() http.HandlerFunc {
	return http.HandlerFunc(func(respWriter http.ResponseWriter, req *http.Request) {
		sdp := webrtc.SessionDescription{}
		if err := json.NewDecoder(req.Body).Decode(&sdp); err != nil {
			r.log.Errorf("failed to decode SDP offer: %v", err)
			respWriter.WriteHeader(http.StatusBadRequest)

			return
		}
		answer, err := r.AcceptOffer(&sdp)
		if err != nil {
			respWriter.WriteHeader(http.StatusBadRequest)

			return
		}
		// Send our answer to the HTTP server listening in the other process
		payload, err := json.Marshal(answer)
		if err != nil {
			r.log.Errorf("failed to marshal SDP answer: %v", err)
			respWriter.WriteHeader(http.StatusInternalServerError)

			return
		}
		respWriter.Header().Set("Content-Type", "application/json")
		if _, err := respWriter.Write(payload); err != nil {
			r.log.Errorf("failed to write signaling response: %v", err)
		}
	})
}
