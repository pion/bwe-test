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
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/logging"
	"github.com/pion/rtp"
	"github.com/pion/transport/v3/vnet"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/ivfwriter"
)

// Receiver manages a WebRTC connection for receiving media.
type Receiver struct {
	settingEngine *webrtc.SettingEngine
	mediaEngine   *webrtc.MediaEngine

	peerConnection *webrtc.PeerConnection

	registry *interceptor.Registry

	log logging.LeveledLogger

	outputBasePath string                           // Base path for output files
	videoWriters   *map[string]io.WriteCloser       // Writers for each track, indexed by track identifier
	ivfWriters     *map[string]*ivfwriter.IVFWriter // Pion's IVF writers for each track, indexed by track identifier
	trackCounter   int                              // Counter for naming tracks
}

// NewReceiver creates a new WebRTC receiver with the given options.
func NewReceiver(opts ...Option) (*Receiver, error) {
	videoWritersMap := make(map[string]io.WriteCloser)
	ivfWritersMap := make(map[string]*ivfwriter.IVFWriter)

	receiver := &Receiver{
		settingEngine:  &webrtc.SettingEngine{},
		mediaEngine:    &webrtc.MediaEngine{},
		peerConnection: &webrtc.PeerConnection{},
		registry:       &interceptor.Registry{},
		log:            logging.NewDefaultLoggerFactory().NewLogger("receiver"),
		outputBasePath: "", // Will be set by SaveVideo option if needed
		videoWriters:   &videoWritersMap,
		ivfWriters:     &ivfWritersMap,
		trackCounter:   0,
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
	// Close IVF writers if they exist
	if r.ivfWriters != nil {
		for _, ivfWriter := range *r.ivfWriters {
			if err := ivfWriter.Close(); err != nil {
				r.log.Errorf("Failed to close IVF writer: %v", err)
			}
		}
	}

	// Close video writers if they exist
	if r.videoWriters != nil {
		for trackID, videoWriter := range *r.videoWriters {
			if err := videoWriter.Close(); err != nil {
				r.log.Errorf("Failed to close video writer for track %s: %v", trackID, err)
			}
		}
	}

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

	// Setup track info and initialize
	trackInfo := r.setupTrackInfo(trackRemote)
	if !trackInfo.isVP8 {
		r.handleNonVP8Track(ctx, trackRemote, rtpReceiver, trackInfo)

		return
	}

	// Setup VP8 processing
	frameAssembler, _, _ := r.setupVP8Processing(trackInfo)
	stats := &trackStats{}

	// Start statistics goroutine
	bytesReceivedChan := make(chan int)
	r.startStatsGoroutine(ctx, bytesReceivedChan, stats)

	// Main packet processing loop
	r.processPackets(
		ctx, trackRemote, rtpReceiver, trackInfo, frameAssembler,
		bytesReceivedChan, stats,
	)
}

type trackInfo struct {
	identifier string
	isVideo    bool
	isVP8      bool
}

type trackStats struct {
	rtpPacketsReceived atomic.Int64
	framesAssembled    int
	keyframesReceived  int
	startTime          time.Time
}

// setupTrackInfo initializes track information and creates output file if needed.
func (r *Receiver) setupTrackInfo(trackRemote *webrtc.TrackRemote) *trackInfo {
	// Check if this is a video track
	isVideo := trackRemote.Kind() == webrtc.RTPCodecTypeVideo
	isVP8 := isVideo && trackRemote.Codec().MimeType == webrtc.MimeTypeVP8

	// Use track counter for consistent naming instead of WebRTC-generated ID
	r.trackCounter++
	trackIdentifier := fmt.Sprintf("track-%d", r.trackCounter)

	// Create separate output file for this track if base path is provided
	if r.outputBasePath != "" && isVP8 && (*r.videoWriters)[trackIdentifier] == nil {
		r.createOutputFile(trackIdentifier)
	}

	return &trackInfo{
		identifier: trackIdentifier,
		isVideo:    isVideo,
		isVP8:      isVP8,
	}
}

// createOutputFile creates a secure output file for the track.
func (r *Receiver) createOutputFile(trackIdentifier string) {
	// Create track-specific filename using our consistent identifier
	filename := fmt.Sprintf("%s_%s.ivf", r.outputBasePath, trackIdentifier)
	// Clean the path to prevent directory traversal
	cleanFilename := filepath.Clean(filename)
	// Ensure the filename doesn't contain any path traversal attempts
	if filepath.IsAbs(cleanFilename) || cleanFilename != filename {
		r.log.Errorf("Invalid filename for security reasons: %s", filename)

		return
	}

	file, err := os.Create(cleanFilename) // #nosec G304 - filename is validated above
	if err != nil {
		r.log.Errorf("Failed to create output file for %s: %v", trackIdentifier, err)

		return
	}

	(*r.videoWriters)[trackIdentifier] = file
	r.log.Infof("Created output file: %s", cleanFilename)
}

// handleNonVP8Track handles non-VP8 tracks by simply reading packets.
func (r *Receiver) handleNonVP8Track(
	ctx context.Context, trackRemote *webrtc.TrackRemote,
	rtpReceiver *webrtc.RTPReceiver, _ *trackInfo,
) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if err := r.setReadDeadlines(rtpReceiver, trackRemote); err != nil {
				continue
			}

			_, _, err := trackRemote.ReadRTP()
			if errors.Is(err, io.EOF) {
				r.log.Infof("trackRemote.ReadRTP received EOF")

				return
			}
			if err != nil {
				r.log.Infof("trackRemote.ReadRTP returned error: %v", err)

				continue
			}
		}
	}
}

// setupVP8Processing initializes VP8 frame assembler and video parameters.
func (r *Receiver) setupVP8Processing(_ *trackInfo) (*VP8FrameAssembler, uint16, uint16) {
	// Get video track parameters if this is a video track
	var videoWidth, videoHeight uint16 = 640, 480 // Default to common dimensions

	// Could parse from SDP, but we'll use defaults for simplicity
	r.log.Infof("VP8 video track detected")

	// Create frame assembler
	frameAssembler := NewVP8FrameAssembler(r.log)
	r.log.Infof("Created VP8 frame assembler")

	return frameAssembler, videoWidth, videoHeight
}

// startStatsGoroutine starts the statistics reporting goroutine.
func (r *Receiver) startStatsGoroutine(ctx context.Context, bytesReceivedChan chan int, stats *trackStats) {
	stats.startTime = time.Now()

	go func() {
		bytesReceived := 0
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
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
				r.log.Infof("throughput: %.2f Mb/s | RTP packets: %d | Frames: %d | Keyframes: %d",
					mBitPerSecond, stats.rtpPacketsReceived.Load(), stats.framesAssembled, stats.keyframesReceived)
				bytesReceived = 0
				last = now
			case newBytesReceived := <-bytesReceivedChan:
				bytesReceived += newBytesReceived
			}
		}
	}()
}

// setReadDeadlines sets read deadlines for RTP receiver and track.
func (r *Receiver) setReadDeadlines(rtpReceiver *webrtc.RTPReceiver, trackRemote *webrtc.TrackRemote) error {
	deadline := time.Now().Add(time.Second)
	if err := rtpReceiver.SetReadDeadline(deadline); err != nil {
		r.log.Infof("failed to SetReadDeadline for rtpReceiver: %v", err)

		return err
	}
	if err := trackRemote.SetReadDeadline(deadline); err != nil {
		r.log.Infof("failed to SetReadDeadline for trackRemote: %v", err)

		return err
	}

	return nil
}

// processPackets handles the main packet processing loop for VP8 tracks.
func (r *Receiver) processPackets(ctx context.Context, trackRemote *webrtc.TrackRemote, rtpReceiver *webrtc.RTPReceiver,
	trackInfo *trackInfo, frameAssembler *VP8FrameAssembler,
	bytesReceivedChan chan int, stats *trackStats,
) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if err := r.setReadDeadlines(rtpReceiver, trackRemote); err != nil {
				continue
			}

			packet, _, err := trackRemote.ReadRTP()
			if errors.Is(err, io.EOF) {
				r.log.Infof("trackRemote.ReadRTP received EOF")

				return
			}
			if err != nil {
				r.log.Infof("trackRemote.ReadRTP returned error: %v", err)

				continue
			}

			bytesReceivedChan <- packet.MarshalSize()
			stats.rtpPacketsReceived.Add(1)

			r.processVP8Packet(packet, trackInfo, frameAssembler, stats)
		}
	}
}

// processVP8Packet processes a single VP8 RTP packet.
func (r *Receiver) processVP8Packet(packet *rtp.Packet, trackInfo *trackInfo, frameAssembler *VP8FrameAssembler,
	stats *trackStats,
) {
	// Only process if we have a video writer for this track
	if (*r.videoWriters)[trackInfo.identifier] == nil {
		return
	}

	// Initialize IVF writer if needed
	if (*r.ivfWriters)[trackInfo.identifier] == nil {
		if err := r.initializeIVFWriter(trackInfo.identifier); err != nil {
			r.log.Errorf("Failed to create IVF writer: %v", err)

			return
		}
	}

	// Write RTP packet directly to IVF using Pion's writer
	r.writeRTPToFile(trackInfo.identifier, packet, stats)

	// Also process frames for potential decoder integration
	frameReady, frameData, isKeyFrame, timestamp := frameAssembler.ProcessPacket(packet)
	if frameReady && len(frameData) > 0 {
		if isKeyFrame {
			stats.keyframesReceived++
			elapsedTime := time.Since(stats.startTime)
			r.log.Infof("Assembled VP8 keyframe: size=%d, timestamp=%d, elapsed=%v",
				len(frameData), timestamp, elapsedTime)
		}
		// frameData can be used for decoder.Decode(frameData) in the future
	}
}

// initializeIVFWriter creates and initializes an IVF writer for a track.
func (r *Receiver) initializeIVFWriter(trackIdentifier string) error {
	// Use Pion's IVF writer with VP8 codec
	ivfWriter, err := ivfwriter.NewWith((*r.videoWriters)[trackIdentifier],
		ivfwriter.WithCodec(webrtc.MimeTypeVP8))
	if err != nil {
		return err
	}
	(*r.ivfWriters)[trackIdentifier] = ivfWriter
	r.log.Infof("Created Pion IVF writer for VP8")

	return nil
}

// writeRTPToFile writes an RTP packet to the IVF file using Pion's writer.
func (r *Receiver) writeRTPToFile(
	trackIdentifier string, packet *rtp.Packet, stats *trackStats,
) {
	if err := (*r.ivfWriters)[trackIdentifier].WriteRTP(packet); err != nil {
		r.log.Errorf("Failed to write RTP packet to IVF: %v", err)
	} else {
		stats.framesAssembled++
		r.log.Debugf("Wrote RTP packet to IVF: size=%d, timestamp=%d",
			len(packet.Payload), packet.Timestamp)
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
