//go:build !js
// +build !js

// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
	"github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/logging"
	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec"
	"github.com/pion/mediadevices/pkg/codec/vpx"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

const transportCCRtcpfb = "transport-cc"

// Static errors for err113 compliance.
var (
	ErrTrackAlreadyExists          = errors.New("track already exists")
	ErrTrackNotFound               = errors.New("track not found")
	ErrTrackDoesNotSupportFrames   = errors.New("track does not support frame injection")
	ErrInvalidNegativeValue        = errors.New("invalid value: must be non-negative")
	ErrAllocationSumMustBePositive = errors.New("sum of allocation values must be greater than 0")
	ErrFailedToCastVideoTrack      = errors.New("failed to cast media track to VideoTrack")
	ErrMissingEncoderConfig        = errors.New("either EncoderBuilder or InitialBitrate must be provided")
)

// VideoTrackInfo holds information about a video track.
type VideoTrackInfo struct {
	TrackID        string
	Width          int
	Height         int
	EncoderBuilder codec.VideoEncoderBuilder
	InitialBitrate int // Optional: if EncoderBuilder is nil, a default VP8 encoder with this bitrate will be created
}

// EncodedTrack represents an encoded video track.
type EncodedTrack struct {
	info           VideoTrackInfo
	videoTrack     *webrtc.TrackLocalStaticSample
	mediaTrack     *mediadevices.VideoTrack
	encodedReader  mediadevices.EncodedReadCloser
	encoderBuilder codec.VideoEncoderBuilder
	videoSource    VideoSource
	bitrateTracker *codec.BitrateTracker
}

// RTCSender is a generic sender that can work with any frame source.
type RTCSender struct {
	// WebRTC components
	peerConnection *webrtc.PeerConnection
	settingEngine  *webrtc.SettingEngine
	mediaEngine    *webrtc.MediaEngine
	registry       *interceptor.Registry

	// Video tracks
	tracks   map[string]*EncodedTrack
	tracksMu sync.RWMutex

	// Bandwidth estimation
	estimatorChan chan cc.BandwidthEstimator

	// Bitrate allocation (protected by tracksMu)
	bitrateAllocation map[string]float64 // Track ID -> percentage (0.0 to 1.0)

	// Frame processing status (protected by tracksMu)
	noEncodedFrame bool

	// Logging
	ccLogWriter io.Writer
	log         logging.LeveledLogger
}

// NewRTCSender creates a new generic sender with GCC bandwidth estimation by default.
func NewRTCSender(opts ...Option) (*RTCSender, error) {
	sender := &RTCSender{
		settingEngine:     &webrtc.SettingEngine{},
		mediaEngine:       &webrtc.MediaEngine{},
		registry:          &interceptor.Registry{},
		tracks:            make(map[string]*EncodedTrack),
		estimatorChan:     make(chan cc.BandwidthEstimator, 1), // Buffered to avoid blocking
		bitrateAllocation: make(map[string]float64),
		noEncodedFrame:    false,
		ccLogWriter:       io.Discard,
		log:               logging.NewDefaultLoggerFactory().NewLogger("nuro_sender"),
	}

	// Register default codecs
	if err := sender.mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}

	// Set up GCC bandwidth estimation by default
	if err := sender.setupGCC(1_000_000); err != nil { // Default initial bitrate: 1Mbps
		return nil, err
	}

	// Apply options directly to RTCSender
	for _, opt := range opts {
		if err := opt(sender); err != nil {
			return nil, err
		}
	}

	return sender, nil
}

// setupGCC sets up Google Congestion Control with the specified initial bitrate.
func (s *RTCSender) setupGCC(initialBitrate int) error {
	controller, err := cc.NewInterceptor(func() (cc.BandwidthEstimator, error) {
		return gcc.NewSendSideBWE(gcc.SendSideBWEInitialBitrate(initialBitrate))
	})
	if err != nil {
		return err
	}

	controller.OnNewPeerConnection(func(_ string, estimator cc.BandwidthEstimator) {
		go func() {
			s.estimatorChan <- estimator
		}()
	})

	s.registry.Add(controller)

	if err = webrtc.ConfigureTWCCHeaderExtensionSender(s.mediaEngine, s.registry); err != nil {
		return err
	}
	// Register transport-cc feedback for media
	s.mediaEngine.RegisterFeedback(webrtc.RTCPFeedback{Type: transportCCRtcpfb}, webrtc.RTPCodecTypeVideo)
	s.mediaEngine.RegisterFeedback(webrtc.RTCPFeedback{Type: transportCCRtcpfb}, webrtc.RTPCodecTypeAudio)

	return nil
}

// getOrCreateEncoderBuilder returns the encoder builder from info or creates a default VP8 encoder.
func getOrCreateEncoderBuilder(info VideoTrackInfo) (codec.VideoEncoderBuilder, error) {
	if info.EncoderBuilder != nil {
		return info.EncoderBuilder, nil
	}

	// Create encoder even for zero bitrate tracks (like NuroSender)
	// Use minimum 1000 bps for zero-bitrate tracks to allow creation
	bitrate := info.InitialBitrate
	if bitrate <= 0 {
		bitrate = 1000 // 1 Kbps minimum for zero-allocation tracks
	}

	params, err := vpx.NewVP8Params()
	if err != nil {
		return nil, fmt.Errorf("failed to create default VP8 encoder: %w", err)
	}
	params.BitRate = bitrate

	return &params, nil
}

// AddVideoTrack adds a new video track.
func (s *RTCSender) AddVideoTrack(info VideoTrackInfo) error {
	s.tracksMu.Lock()
	defer s.tracksMu.Unlock()

	if _, exists := s.tracks[info.TrackID]; exists {
		return fmt.Errorf("%w: %s", ErrTrackAlreadyExists, info.TrackID)
	}

	encoderBuilder, err := getOrCreateEncoderBuilder(info)
	if err != nil {
		return err
	}

	// Create codec selector with the encoder builder
	codecSelector := mediadevices.NewCodecSelector(
		mediadevices.WithVideoEncoders(encoderBuilder),
	)

	// Create frame buffer for this track
	frameSource := NewFrameBuffer(info.Width, info.Height)

	// Create media track with encoder
	mediaTrack := mediadevices.NewVideoTrack(frameSource, codecSelector)

	// Create encoded reader
	mimeType := encoderBuilder.RTPCodec().MimeType
	encodedReader, err := mediaTrack.NewEncodedReader(mimeType)
	if err != nil {
		return err
	}

	// Create WebRTC track
	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: mimeType},
		info.TrackID,
		info.TrackID,
	)
	if err != nil {
		_ = encodedReader.Close()
		_ = mediaTrack.Close()

		return err
	}

	// Store track info
	videoMediaTrack, ok := mediaTrack.(*mediadevices.VideoTrack)
	if !ok {
		_ = encodedReader.Close()
		_ = mediaTrack.Close()

		return ErrFailedToCastVideoTrack
	}

	s.tracks[info.TrackID] = &EncodedTrack{
		info:           info,
		videoTrack:     videoTrack,
		mediaTrack:     videoMediaTrack,
		encodedReader:  encodedReader,
		encoderBuilder: info.EncoderBuilder,
		videoSource:    frameSource,
		bitrateTracker: codec.NewBitrateTracker(300 * time.Millisecond),
	}

	// Mark the frame buffer as initialized after successful track creation
	frameSource.SetInitialized()

	// Add track to peer connection if it exists
	if s.peerConnection != nil {
		rtpSender, err := s.peerConnection.AddTrack(videoTrack)
		if err != nil {
			return err
		}

		// Handle incoming RTCP
		go func() {
			rtcpBuf := make([]byte, 1500)
			for {
				if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
					return
				}
			}
		}()
	}

	return nil
}

// SendFrame sends a frame to a specific track.
func (s *RTCSender) SendFrame(trackID string, frame image.Image) error {
	s.tracksMu.RLock()
	track, exists := s.tracks[trackID]
	s.tracksMu.RUnlock()

	if !exists {
		return fmt.Errorf("%w: %s", ErrTrackNotFound, trackID)
	}

	// Use the video source's SendFrame method if it's a FrameBuffer
	if frameBuffer, ok := track.videoSource.(*FrameBuffer); ok {
		return frameBuffer.SendFrame(frame)
	}

	// If it's not a FrameBuffer, we can't send frames to it
	return fmt.Errorf("%w: %s", ErrTrackDoesNotSupportFrames, trackID)
}

// SetupPeerConnection initializes the WebRTC peer connection.
func (s *RTCSender) SetupPeerConnection() error {
	// Create peer connection
	pc, err := webrtc.NewAPI(
		webrtc.WithSettingEngine(*s.settingEngine),
		webrtc.WithInterceptorRegistry(s.registry),
		webrtc.WithMediaEngine(s.mediaEngine),
	).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return err
	}
	s.peerConnection = pc

	// Add existing tracks to peer connection
	s.tracksMu.RLock()
	for _, track := range s.tracks {
		rtpSender, err := s.peerConnection.AddTrack(track.videoTrack)
		if err != nil {
			s.tracksMu.RUnlock()

			return err
		}

		// Handle incoming RTCP
		go func() {
			rtcpBuf := make([]byte, 1500)
			for {
				if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
					return
				}
			}
		}()
	}
	s.tracksMu.RUnlock()

	// Set up connection state handlers
	s.peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		s.log.Infof("ICE Connection State: %s", state.String())
	})

	s.peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		s.log.Infof("Peer Connection State: %s", state.String())
	})

	s.peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
		s.log.Infof("Sender candidate: %v", i)
	})

	return nil
}

// CreateOffer creates a WebRTC offer.
func (s *RTCSender) CreateOffer() (*webrtc.SessionDescription, error) {
	offer, err := s.peerConnection.CreateOffer(nil)
	if err != nil {
		return nil, err
	}

	gatherComplete := webrtc.GatheringCompletePromise(s.peerConnection)
	if err = s.peerConnection.SetLocalDescription(offer); err != nil {
		return nil, err
	}
	<-gatherComplete

	return s.peerConnection.LocalDescription(), nil
}

// AcceptAnswer processes the WebRTC answer.
func (s *RTCSender) AcceptAnswer(answer *webrtc.SessionDescription) error {
	return s.peerConnection.SetRemoteDescription(*answer)
}

// Start begins the encoding and bitrate control loop.
func (s *RTCSender) Start(ctx context.Context) error {
	// Wait for estimator
	var estimator cc.BandwidthEstimator
	select {
	case estimator = <-s.estimatorChan:
	case <-ctx.Done():
		return nil
	}

	// Combined encoding and bitrate control loop
	ticker := time.NewTicker(100 * time.Millisecond) // 10 Hz for encoding (matching 10fps input)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// Check bitrate every 100ms
			targetBitrate := estimator.GetTargetBitrate()
			s.updateBitrate(targetBitrate)

			// Process encoded frames for all tracks
			s.processEncodedFrames()
		}
	}
}

// calculateTrackBitrate calculates the bitrate for a track based on allocation.
func (s *RTCSender) calculateTrackBitrate(trackID string, targetBitrate, equalShare int, useCustomAllocation bool) int {
	if !useCustomAllocation {
		return equalShare
	}

	percentage, exists := s.bitrateAllocation[trackID]
	if !exists || percentage < 1e-6 {
		return 0
	}

	return int(float64(targetBitrate) * percentage)
}

// updateEncoderBitrate updates the encoder controller for a track.
func updateEncoderBitrate(track *EncodedTrack, currentBitrate, targetBitrate int) bool {
	// Try QPController first (standard pion mediadevices)
	if qpController, ok := track.encodedReader.Controller().(codec.QPController); ok && qpController != nil {
		_ = qpController.DynamicQPControl(currentBitrate, targetBitrate)

		return true
	}

	// Try generic interface with SetBitrate method (for forks/custom implementations)
	controller := track.encodedReader.Controller()
	if controller == nil {
		return false
	}

	if setBitrateMethod, ok := any(controller).(interface {
		SetBitrate(int, int)
	}); ok {
		setBitrateMethod.SetBitrate(currentBitrate, targetBitrate)

		return true
	}

	return false
}

// updateBitrate updates bitrate for all tracks.
func (s *RTCSender) updateBitrate(targetBitrate int) {
	s.tracksMu.RLock()
	defer s.tracksMu.RUnlock()

	if len(s.tracks) == 0 {
		return
	}

	useCustomAllocation := len(s.bitrateAllocation) > 0
	equalShare := targetBitrate / len(s.tracks)

	for trackID, track := range s.tracks {
		trackBitrate := s.calculateTrackBitrate(trackID, targetBitrate, equalShare, useCustomAllocation)

		// Only update encoder for tracks with positive bitrate (like NuroSender)
		if trackBitrate > 0 {
			currentBitrate := int(track.bitrateTracker.GetBitrate())
			if !updateEncoderBitrate(track, currentBitrate, trackBitrate) {
				s.log.Warnf("No compatible encoder controller for track %s", track.info.TrackID)
			}
		}
		// Note: tracks with 0 bitrate still remain active, just don't get encoder updates
	}

	if useCustomAllocation {
		s.log.Infof("Updated bitrate to %d bps using custom allocation", targetBitrate)
	} else {
		s.log.Infof("Updated bitrate to %d bps (%d per track)", targetBitrate, equalShare)
	}
}

// processEncodedFrames reads and sends encoded frames for all tracks.
func (s *RTCSender) processEncodedFrames() {
	s.tracksMu.RLock()

	totalTracks := len(s.tracks)
	if totalTracks == 0 {
		s.tracksMu.RUnlock()

		return
	}

	// Process tracks in parallel to reduce total processing time
	var wg sync.WaitGroup
	results := make(chan trackResult, totalTracks)

	// Start parallel processing for each track
	for trackID, track := range s.tracks {
		wg.Add(1)
		go func(trackID string, track *EncodedTrack) {
			defer wg.Done()

			result := trackResult{
				trackID: trackID,
				success: false,
			}

			// Try to read an encoded frame (non-blocking)
			encoded, release, err := track.encodedReader.Read()
			if err != nil {
				result.error = err
				results <- result

				return
			}

			// Track the actual bitrate
			track.bitrateTracker.AddFrame(len(encoded.Data), time.Now())

			// Send to WebRTC track
			err = track.videoTrack.WriteSample(media.Sample{
				Data:     encoded.Data,
				Duration: time.Second / 10, // Assuming 10fps
			})

			release()

			result.success = true
			result.error = err
			results <- result
		}(trackID, track)
	}

	// Wait for all tracks to complete
	wg.Wait()
	close(results)

	// Release read lock now that all goroutines are done accessing tracks
	s.tracksMu.RUnlock()

	// Process results and determine noEncodedFrame status
	allHaveErrors := true
	for result := range results {
		if result.error != nil {
			// ErrNoFrameAvailable is expected during normal operation (timing gaps between frames)
			// Log it at Debug level to reduce noise; other errors remain at Error level
			if errors.Is(result.error, ErrNoFrameAvailable) {
				s.log.Debugf("No frame available for track %s", result.trackID)
			} else {
				s.log.Errorf("Error processing track %s: %v", result.trackID, result.error)
			}
		} else {
			// At least one result has no error
			allHaveErrors = false
		}
	}

	// Update noEncodedFrame: true if all results have errors, false otherwise
	s.tracksMu.Lock()
	s.noEncodedFrame = allHaveErrors
	s.tracksMu.Unlock()
}

// trackResult holds the result of processing a single track.
type trackResult struct {
	trackID string
	success bool
	error   error
}

// SetBitrateAllocation sets custom bitrate allocation for tracks.
// allocation is a map of track ID to arbitrary positive numbers representing relative weights
// The values will be normalized internally (each value divided by sum of all values)
// Pass nil to return to equal distribution.
func (s *RTCSender) SetBitrateAllocation(allocation map[string]float64) error {
	s.tracksMu.Lock()
	defer s.tracksMu.Unlock()

	if allocation == nil {
		s.bitrateAllocation = make(map[string]float64)
		s.log.Info("Cleared custom bitrate allocation, using equal distribution")

		return nil
	}

	var total float64
	for trackID, value := range allocation {
		if value < 0.0 {
			return fmt.Errorf("%w: %f", ErrInvalidNegativeValue, value)
		}
		if _, exists := s.tracks[trackID]; !exists {
			return fmt.Errorf("%w: %s", ErrTrackNotFound, trackID)
		}
		total += value
	}

	if total == 0.0 {
		return ErrAllocationSumMustBePositive
	}

	// Normalize values by dividing by total sum
	normalizedAllocation := make(map[string]float64)
	for trackID, value := range allocation {
		normalizedAllocation[trackID] = value / total
	}

	s.bitrateAllocation = normalizedAllocation
	s.log.Infof("Updated bitrate allocation: %v (normalized from %v)", normalizedAllocation, allocation)

	return nil
}

// Close releases all resources.
func (s *RTCSender) Close() error {
	s.tracksMu.Lock()
	defer s.tracksMu.Unlock()

	for _, track := range s.tracks {
		_ = track.videoSource.Close()
		_ = track.encodedReader.Close()
		_ = track.mediaTrack.Close()
	}

	if s.peerConnection != nil {
		return s.peerConnection.Close()
	}

	return nil
}

// GetPeerConnection returns the WebRTC peer connection.
func (s *RTCSender) GetPeerConnection() *webrtc.PeerConnection {
	return s.peerConnection
}

// GetWebRTCTrackLocal returns the WebRTC track for a specific track ID.
func (s *RTCSender) GetWebRTCTrackLocal(trackID string) (*webrtc.TrackLocalStaticSample, error) {
	s.tracksMu.RLock()
	defer s.tracksMu.RUnlock()

	track, exists := s.tracks[trackID]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrTrackNotFound, trackID)
	}

	return track.videoTrack, nil
}

// ConfigurableWebRTCSender interface implementation.

// GetSettingEngine returns the setting engine.
func (s *RTCSender) GetSettingEngine() *webrtc.SettingEngine {
	return s.settingEngine
}

// GetMediaEngine returns the media engine.
func (s *RTCSender) GetMediaEngine() *webrtc.MediaEngine {
	return s.mediaEngine
}

// GetRegistry returns the interceptor registry.
func (s *RTCSender) GetRegistry() *interceptor.Registry {
	return s.registry
}

// GetEstimatorChan returns the bandwidth estimator channel.
func (s *RTCSender) GetEstimatorChan() chan cc.BandwidthEstimator {
	return s.estimatorChan
}

// SetLogger sets the logger.
func (s *RTCSender) SetLogger(logger logging.LeveledLogger) {
	s.log = logger
}

// SetCCLogWriter sets the congestion control log writer.
func (s *RTCSender) SetCCLogWriter(w io.Writer) {
	s.ccLogWriter = w
}

// GetEncodeFrameOk returns the current encode frame status.
func (s *RTCSender) GetEncodeFrameOk() bool {
	s.tracksMu.RLock()
	defer s.tracksMu.RUnlock()

	return !s.noEncodedFrame
}
