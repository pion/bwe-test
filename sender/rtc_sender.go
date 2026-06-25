//go:build !js

// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
	"github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/interceptor/pkg/report"
	"github.com/pion/interceptor/pkg/stats"
	"github.com/pion/logging"
	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec"
	"github.com/pion/mediadevices/pkg/codec/vpx"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

const (
	transportCCRtcpfb   = "transport-cc"
	encodeHealthTimeout = 2 * time.Second
)

// Static errors for err113 compliance.
var (
	ErrTrackAlreadyExists          = errors.New("track already exists")
	ErrTrackNotFound               = errors.New("track not found")
	ErrTrackDoesNotSupportFrames   = errors.New("track does not support frame injection")
	ErrInvalidNegativeValue        = errors.New("invalid value: must be non-negative")
	ErrAllocationSumMustBePositive = errors.New("sum of allocation values must be greater than 0")
	ErrFailedToCastVideoTrack      = errors.New("failed to cast media track to VideoTrack")
	ErrMissingEncoderConfig        = errors.New("either EncoderBuilder or InitialBitrate must be provided")
	ErrForceKeyFrameNotSupported   = errors.New("encoder does not support ForceKeyFrame")
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
	// bitrateMu serializes access to bitrateTracker; AddFrame runs on the
	// encode goroutine and GetBitrate runs on the BWE update path.
	bitrateMu sync.Mutex
	// encoderMu protects the encoder lifecycle. The per-track encode
	// goroutine holds RLock from encodedReader.Read through release();
	// recreateEncoder takes Lock to swap the reader without racing the
	// goroutine's in-flight release callback.
	encoderMu sync.RWMutex
	mimeType  string

	// rtpSender is set after AddTrack and used to look up the SSRC when
	// resolving network stats from the Pion stats interceptor.
	rtpSender *webrtc.RTPSender

	// Per-frame timing counters used by GetTrackStats. Updated atomically by
	// the encode goroutine in encodeAndSendTrack.
	framesEncoded        atomic.Uint64
	totalEncodeTimeNs    atomic.Uint64
	totalSendQueueTimeNs atomic.Uint64
	lastEncodeAtWallUs   atomic.Int64

	// targetBitrate is this track's allocated share (bps) last pushed to the
	// encoder by updateBitrate. Read by GetTrackStats as the W3C
	// outbound-rtp `targetBitrate`.
	targetBitrate atomic.Int64

	// Synthesized quality-limitation accounting (pion provides no encoder
	// feedback, so we infer it). qlCPUNs / qlBandwidthNs accumulate the wall
	// time spent CPU- / bandwidth-limited; qlLastTickWallUs and qlLastOverruns
	// are the per-tick baselines used by accumulateQualityLimitation.
	qlCPUNs          atomic.Uint64
	qlBandwidthNs    atomic.Uint64
	qlLastTickWallUs atomic.Int64
	qlLastOverruns   atomic.Uint64

	// encodeOverruns counts frames evicted from a full buffer because the
	// producer outpaced the encode loop — the CPU-limited signal.
	encodeOverruns atomic.Uint64

	encodeCancel context.CancelFunc
	encodeDone   chan struct{}
}

// EncodedFrameCallback is called after each VP8 frame is encoded with the
// track ID, encoded data, and whether it's a keyframe. Callers can use this
// to route encoded frames to a ring buffer or other sink without a second
// encode pass.
type EncodedFrameCallback func(trackID string, data []byte, isKeyframe bool)

// FrameSentCallback fires per encoded frame (after WriteSample) and per
// frame evicted from a full buffer. captureTSUs echoes the value passed
// to SendFrameWithCaptureTS (0 = none). dequeuedAtWallUs / encodeDoneAtWallUs
// / sentAtWallUs are time.Now().UnixMicro() at FrameBuffer.Read return,
// encodedReader.Read return, and WriteSample return respectively. On
// dropped=true (WriteSample error or eviction), dequeuedAtWallUs and
// encodeDoneAtWallUs are 0. Runs on the encode goroutine for completed
// sends, on the SendFrame caller's goroutine for evictions; must not block.
type FrameSentCallback func(trackID string,
	captureTSUs, dequeuedAtWallUs, encodeDoneAtWallUs, sentAtWallUs int64,
	dropped bool)

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

	// Audio tracks (pre-encoded Opus, no encoder management needed)
	audioTracks map[string]*webrtc.TrackLocalStaticSample

	// Bandwidth estimation
	estimatorChan chan cc.BandwidthEstimator

	// availableOutgoingBitrate is the latest GCC estimate (bps), connection-level.
	// Stored by the Start loop, read by GetTrackStats.
	availableOutgoingBitrate atomic.Int64
	// maxBitrate is the configured GCC cap (bps); 0 means no cap. Used to infer
	// the bandwidth-limited state for quality-limitation accounting.
	maxBitrate int

	// Bitrate allocation (protected by tracksMu)
	bitrateAllocation map[string]float64 // Track ID -> percentage (0.0 to 1.0)

	// Cancel function for RTCP reader goroutines. Replaced on each SetupPeerConnection.
	rtcpCancel context.CancelFunc

	// Optional callback invoked after each VP8 frame is encoded.
	onEncodedFrame EncodedFrameCallback

	// Optional callback invoked once per frame after WriteSample returns.
	// Used by callers measuring end-to-end onboard latency (capture_ts ->
	// frame enqueued into RTP send pipeline).
	onFrameSent FrameSentCallback

	// statsGetter is populated by the Pion stats interceptor's
	// OnNewPeerConnection callback. Used by GetTrackStats to read RTP/RTCP
	// counters (PacketsSent, RoundTripTime) per SSRC.
	statsGetter   stats.Getter
	statsGetterMu sync.RWMutex

	// Logging
	ccLogWriter io.Writer
	log         logging.LeveledLogger

	// gccConfigured is true when GCC was set up via the GCC option.
	gccConfigured bool
}

// SetOnEncodedFrame registers a callback invoked after each VP8 frame is
// encoded and sent to the WebRTC track. Thread-safe (called under tracksMu).
func (s *RTCSender) SetOnEncodedFrame(cb EncodedFrameCallback) {
	s.tracksMu.Lock()
	defer s.tracksMu.Unlock()
	s.onEncodedFrame = cb
}

// SetOnFrameSent registers a per-frame send-completion callback.
// Thread-safe. See FrameSentCallback for the contract.
func (s *RTCSender) SetOnFrameSent(cb FrameSentCallback) {
	s.tracksMu.Lock()
	defer s.tracksMu.Unlock()
	s.onFrameSent = cb
}

// NewRTCSender creates a new generic sender with GCC bandwidth estimation by default.
func NewRTCSender(opts ...Option) (*RTCSender, error) {
	sender := &RTCSender{
		settingEngine:     &webrtc.SettingEngine{},
		mediaEngine:       &webrtc.MediaEngine{},
		registry:          &interceptor.Registry{},
		tracks:            make(map[string]*EncodedTrack),
		audioTracks:       make(map[string]*webrtc.TrackLocalStaticSample),
		estimatorChan:     make(chan cc.BandwidthEstimator, 1), // Buffered to avoid blocking
		bitrateAllocation: make(map[string]float64),
		ccLogWriter:       io.Discard,
		log:               logging.NewDefaultLoggerFactory().NewLogger("nuro_sender"),
	}

	// Register default codecs
	if err := sender.mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}

	// Register the stats interceptor so GetTrackStats can return RTP/RTCP
	// counters (PacketsSent, RoundTripTime) per track.
	if err := sender.setupStats(); err != nil {
		return nil, err
	}

	// Apply options first (may include custom GCC config)
	for _, opt := range opts {
		if err := opt(sender); err != nil {
			return nil, err
		}
	}

	// Set up default GCC only if no GCC option was provided
	if !sender.gccConfigured {
		if err := sender.setupGCC(1_000_000, 0); err != nil { // Default initial bitrate: 1Mbps, no max
			return nil, err
		}
	}

	return sender, nil
}

// setupGCC sets up Google Congestion Control with the specified initial and max bitrate.
// A maxBitrate of 0 means no cap (uses GCC default of 50 Mbps).
func (s *RTCSender) setupGCC(initialBitrate, maxBitrate int) error {
	s.maxBitrate = maxBitrate
	controller, err := cc.NewInterceptor(func() (cc.BandwidthEstimator, error) {
		opts := []gcc.Option{gcc.SendSideBWEInitialBitrate(initialBitrate)}
		if maxBitrate > 0 {
			opts = append(opts, gcc.SendSideBWEMaxBitrate(maxBitrate))
		}

		return gcc.NewSendSideBWE(opts...)
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

// setupStats registers the Pion stats interceptor and a sender-report
// interceptor so RTP outbound counters and RTCP-derived RTT are recorded
// per SSRC.
//
// Registration order matters: stats must be added BEFORE senderReports.
// Pion's interceptor chain wraps the transport writer in registration
// order, and each interceptor stores the writer passed into its own
// BindRTCPWriter to emit RTCP. If senderReports were registered first, it
// would store the bare transport writer, and the SR packets it generates
// would bypass the stats recorder — leaving lastSenderReports empty and
// silently disabling RTT extraction in RemoteInboundRTPStreamStats.
func (s *RTCSender) setupStats() error {
	factory, err := stats.NewInterceptor()
	if err != nil {
		return err
	}
	factory.OnNewPeerConnection(func(_ string, getter stats.Getter) {
		s.statsGetterMu.Lock()
		s.statsGetter = getter
		s.statsGetterMu.Unlock()
	})
	s.registry.Add(factory)

	senderReports, err := report.NewSenderInterceptor()
	if err != nil {
		return err
	}
	s.registry.Add(senderReports)

	return nil
}

// TrackStats summarizes per-track network and pipeline statistics for one
// outbound video track. Cumulative since track creation; callers that want
// per-window deltas should snapshot two reads and subtract.
type TrackStats struct {
	// Bandwidth (bps). AvailableOutgoingBitrate is the connection-level GCC
	// estimate; TargetBitrate is this track's allocated share pushed to the
	// encoder (W3C outbound-rtp `targetBitrate`).
	AvailableOutgoingBitrate int
	TargetBitrate            int

	// Pipeline counters from the local encode loop.
	FramesEncoded        uint64
	TotalEncodeTimeNs    uint64 // wall time spent inside encodedReader.Read
	TotalSendQueueTimeNs uint64 // wall time between Read return and WriteSample return

	// Synthesized quality-limitation durations (seconds), W3C outbound-rtp
	// `qualityLimitationDurations` semantics. Bandwidth-limited is inferred from
	// the GCC estimate sitting below the configured max bitrate; CPU-limited is
	// inferred from the encode loop falling behind (buffer evictions). When GCC
	// has no max cap, bandwidth-limited cannot be inferred and stays zero.
	QualityLimitationCPUSeconds       float64
	QualityLimitationBandwidthSeconds float64

	// Network counters from the Pion stats interceptor.
	PacketsSent     uint64
	BytesSent       uint64
	HeaderBytesSent uint64
	NACKCount       uint32
	PLICount        uint32
	FIRCount        uint32

	// Remote receiver counters (from RTCP RR).
	PacketsReceived           uint64
	PacketsLost               int64
	Jitter                    float64
	FractionLost              float64
	RoundTripTime             time.Duration
	TotalRoundTripTime        time.Duration
	RoundTripTimeMeasurements uint64
}

// GetTrackStats returns the latest cumulative TrackStats for the given video
// track. Returns nil when the track does not exist. Network fields are zero
// until the stats interceptor has observed traffic for the track's SSRC and
// (for RTT) at least one RTCP receiver report has arrived.
//
// Pipeline counters approximate the W3C `outbound-rtp` framesEncoded /
// totalEncodeTime / totalPacketSendDelay fields. They are updated each time
// the encode loop produces a frame.
func (s *RTCSender) GetTrackStats(trackID string) *TrackStats {
	s.tracksMu.RLock()
	track, ok := s.tracks[trackID]
	s.tracksMu.RUnlock()
	if !ok {
		return nil
	}

	out := &TrackStats{
		AvailableOutgoingBitrate:          int(s.availableOutgoingBitrate.Load()),
		TargetBitrate:                     int(track.targetBitrate.Load()),
		FramesEncoded:                     track.framesEncoded.Load(),
		TotalEncodeTimeNs:                 track.totalEncodeTimeNs.Load(),
		TotalSendQueueTimeNs:              track.totalSendQueueTimeNs.Load(),
		QualityLimitationCPUSeconds:       float64(track.qlCPUNs.Load()) / 1e9,
		QualityLimitationBandwidthSeconds: float64(track.qlBandwidthNs.Load()) / 1e9,
	}

	s.statsGetterMu.RLock()
	getter := s.statsGetter
	s.statsGetterMu.RUnlock()
	if getter == nil || track.rtpSender == nil {
		return out
	}

	params := track.rtpSender.GetParameters()
	if len(params.Encodings) == 0 {
		return out
	}
	ssrc := uint32(params.Encodings[0].SSRC)
	if ssrc == 0 {
		return out
	}

	netStats := getter.Get(ssrc)
	if netStats == nil {
		return out
	}

	out.PacketsSent = netStats.OutboundRTPStreamStats.PacketsSent
	out.BytesSent = netStats.OutboundRTPStreamStats.BytesSent
	out.HeaderBytesSent = netStats.OutboundRTPStreamStats.HeaderBytesSent
	out.NACKCount = netStats.OutboundRTPStreamStats.NACKCount
	out.PLICount = netStats.OutboundRTPStreamStats.PLICount
	out.FIRCount = netStats.OutboundRTPStreamStats.FIRCount

	out.PacketsReceived = netStats.RemoteInboundRTPStreamStats.PacketsReceived
	out.PacketsLost = netStats.RemoteInboundRTPStreamStats.PacketsLost
	out.Jitter = netStats.RemoteInboundRTPStreamStats.Jitter
	out.FractionLost = netStats.RemoteInboundRTPStreamStats.FractionLost
	out.RoundTripTime = netStats.RemoteInboundRTPStreamStats.RoundTripTime
	out.TotalRoundTripTime = netStats.RemoteInboundRTPStreamStats.TotalRoundTripTime
	out.RoundTripTimeMeasurements = netStats.RemoteInboundRTPStreamStats.RoundTripTimeMeasurements

	return out
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

	if _, exists := s.tracks[info.TrackID]; exists {
		s.tracksMu.Unlock()

		return fmt.Errorf("%w: %s", ErrTrackAlreadyExists, info.TrackID)
	}

	encoderBuilder, err := getOrCreateEncoderBuilder(info)
	if err != nil {
		s.tracksMu.Unlock()

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
		s.tracksMu.Unlock()

		return err
	}

	// Create WebRTC track
	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: mimeType},
		info.TrackID,
		info.TrackID,
	)
	if err != nil {
		s.tracksMu.Unlock()
		_ = encodedReader.Close()
		_ = mediaTrack.Close()

		return err
	}

	// Store track info
	videoMediaTrack, ok := mediaTrack.(*mediadevices.VideoTrack)
	if !ok {
		s.tracksMu.Unlock()
		_ = encodedReader.Close()
		_ = mediaTrack.Close()

		return ErrFailedToCastVideoTrack
	}

	track := &EncodedTrack{
		info:           info,
		videoTrack:     videoTrack,
		mediaTrack:     videoMediaTrack,
		encodedReader:  encodedReader,
		encoderBuilder: info.EncoderBuilder,
		videoSource:    frameSource,
		bitrateTracker: codec.NewBitrateTracker(300 * time.Millisecond),
		mimeType:       mimeType,
		encodeDone:     make(chan struct{}),
	}
	track.lastEncodeAtWallUs.Store(time.Now().UnixMicro())
	s.tracks[info.TrackID] = track

	// Mark the frame buffer as initialized after successful track creation
	frameSource.SetInitialized()

	// Add track to peer connection if it exists
	if s.peerConnection != nil {
		rtpSender, err := s.peerConnection.AddTrack(videoTrack)
		if err != nil {
			delete(s.tracks, info.TrackID)
			_ = track.videoSource.Close()
			_ = track.encodedReader.Close()
			_ = track.mediaTrack.Close()
			s.tracksMu.Unlock()

			return err
		}
		track.rtpSender = rtpSender

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

	//nolint:gosec // G118: trackCancel is stored in track.encodeCancel and called during teardown (Close/RemoveTrack).
	trackCtx, trackCancel := context.WithCancel(context.Background())
	track.encodeCancel = trackCancel
	s.tracksMu.Unlock()
	go s.runEncodeLoop(trackCtx, info.TrackID, track)

	return nil
}

// AddAudioTrack adds a pre-encoded audio track (Opus).
// Audio tracks don't participate in bitrate allocation or encoding — they receive
// pre-encoded Opus frames via WriteSample on the returned track.
func (s *RTCSender) AddAudioTrack(trackID string) (*webrtc.TrackLocalStaticSample, error) {
	s.tracksMu.Lock()
	defer s.tracksMu.Unlock()

	if _, exists := s.audioTracks[trackID]; exists {
		return nil, fmt.Errorf("%w: %s", ErrTrackAlreadyExists, trackID)
	}
	if _, exists := s.tracks[trackID]; exists {
		return nil, fmt.Errorf("%w: %s", ErrTrackAlreadyExists, trackID)
	}

	// Per RFC 7587 §7, Opus must be advertised with 2 channels in SDP.
	// Mono/stereo behavior is controlled by the "stereo" fmtp parameter.
	audioTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeOpus,
			ClockRate:   48000,
			Channels:    2,
			SDPFmtpLine: "minptime=10;useinbandfec=1",
		},
		trackID,
		trackID,
	)
	if err != nil {
		return nil, err
	}

	s.audioTracks[trackID] = audioTrack

	// Audio tracks are NOT added to the PeerConnection here. They are
	// published via LiveKit's PublishTrack, which handles SDP renegotiation
	// for audio codecs correctly.

	s.log.Infof("Added audio track: %s", trackID)

	return audioTrack, nil
}

// SendFrame sends a frame to a specific track with no associated capture
// timestamp. Equivalent to SendFrameWithCaptureTS(trackID, frame, 0).
func (s *RTCSender) SendFrame(trackID string, frame image.Image) error {
	return s.SendFrameWithCaptureTS(trackID, frame, 0)
}

// SendFrameWithCaptureTS sends a frame along with an opaque capture
// timestamp (microseconds) that is echoed to any registered
// FrameSentCallback. Pass 0 to opt out. When the buffer is full and the
// oldest frame is evicted to make room, the callback fires synchronously
// for the evicted frame with dropped=true so downstream SLO accounting
// catches the overload case.
func (s *RTCSender) SendFrameWithCaptureTS(trackID string, frame image.Image, captureTSUs int64) error {
	s.tracksMu.RLock()
	track, exists := s.tracks[trackID]
	s.tracksMu.RUnlock()

	if !exists {
		return fmt.Errorf("%w: %s", ErrTrackNotFound, trackID)
	}

	frameBuffer, ok := track.videoSource.(*FrameBuffer)
	if !ok {
		return fmt.Errorf("%w: %s", ErrTrackDoesNotSupportFrames, trackID)
	}

	evicted, err := frameBuffer.SendFrameWithCaptureTS(frame, captureTSUs)
	if evicted {
		// A full buffer means the encode loop is falling behind the producer:
		// the CPU-limited signal for quality-limitation accounting.
		track.encodeOverruns.Add(1)

		s.tracksMu.RLock()
		onFrameSent := s.onFrameSent
		s.tracksMu.RUnlock()
		if onFrameSent != nil {
			onFrameSent(trackID, 0, 0, 0, time.Now().UnixMicro(), true)
		}
	}

	return err
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

	// Cancel any RTCP goroutines from a previous peer connection before creating new ones.
	if s.rtcpCancel != nil {
		s.rtcpCancel()
	}
	rtcpCtx, rtcpCancel := context.WithCancel(context.Background())
	s.rtcpCancel = rtcpCancel

	// Add existing video tracks to peer connection.
	// Audio tracks are NOT added here — they are published separately via
	// LiveKit's PublishTrack, which handles SDP negotiation for audio codecs.
	s.tracksMu.RLock()
	for _, track := range s.tracks {
		rtpSender, err := s.peerConnection.AddTrack(track.videoTrack)
		if err != nil {
			s.tracksMu.RUnlock()

			return err
		}
		track.rtpSender = rtpSender

		// Handle incoming RTCP. Exits on context cancellation or read error.
		go func() {
			rtcpBuf := make([]byte, 1500)
			for {
				select {
				case <-rtcpCtx.Done():
					return
				default:
				}
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

// Start begins the bitrate control loop.
func (s *RTCSender) Start(ctx context.Context) error {
	// Wait for estimator
	var estimator cc.BandwidthEstimator
	select {
	case estimator = <-s.estimatorChan:
	case <-ctx.Done():
		return nil
	}

	// Keep GCC bitrate updates at 100ms cadence.
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			targetBitrate := estimator.GetTargetBitrate()
			s.availableOutgoingBitrate.Store(int64(targetBitrate))
			s.updateBitrate(targetBitrate)
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
	nowUs := time.Now().UnixMicro()

	for trackID, track := range s.tracks {
		trackBitrate := s.calculateTrackBitrate(trackID, targetBitrate, equalShare, useCustomAllocation)
		track.targetBitrate.Store(int64(trackBitrate))
		s.accumulateQualityLimitation(track, nowUs)

		// Only update encoder for tracks with positive bitrate (like NuroSender)
		if trackBitrate > 0 {
			track.bitrateMu.Lock()
			currentBitrate := int(track.bitrateTracker.GetBitrate())
			track.bitrateMu.Unlock()
			if !updateEncoderBitrate(track, currentBitrate, trackBitrate) {
				s.log.Warnf("No compatible encoder controller for track %s", track.info.TrackID)
			}
		}
		// Note: tracks with 0 bitrate still remain active, just don't get encoder updates
	}

	if useCustomAllocation {
		s.log.Debugf("Updated bitrate to %d bps using custom allocation", targetBitrate)
	} else {
		s.log.Debugf("Updated bitrate to %d bps (%d per track)", targetBitrate, equalShare)
	}
}

// accumulateQualityLimitation attributes the wall time elapsed since the last
// tick to a quality-limitation bucket for the track. It is called once per
// updateBitrate tick (≈100ms) while holding tracksMu. Classification each tick:
//
//   - bandwidth-limited: a GCC cap is configured and the current estimate sits
//     below it, so the rate is being held down by the network;
//   - else CPU-limited: the encode loop fell behind since the last tick (a
//     frame was evicted from a full buffer);
//   - else neither (no accumulation).
//
// The first call only establishes the baselines and accumulates nothing.
func (s *RTCSender) accumulateQualityLimitation(track *EncodedTrack, nowUs int64) {
	lastUs := track.qlLastTickWallUs.Swap(nowUs)
	overruns := track.encodeOverruns.Load()
	prevOverruns := track.qlLastOverruns.Swap(overruns)
	if lastUs == 0 {
		return // baseline tick
	}

	elapsedNs := (nowUs - lastUs) * 1000
	if elapsedNs <= 0 {
		return
	}

	avail := int(s.availableOutgoingBitrate.Load())
	switch {
	case s.maxBitrate > 0 && avail < s.maxBitrate:
		track.qlBandwidthNs.Add(uint64(elapsedNs)) //nolint:gosec // G115: elapsedNs > 0 by guard above
	case overruns > prevOverruns:
		track.qlCPUNs.Add(uint64(elapsedNs)) //nolint:gosec // G115: elapsedNs > 0 by guard above
	}
}

func (s *RTCSender) runEncodeLoop(ctx context.Context, trackID string, track *EncodedTrack) {
	defer close(track.encodeDone)

	// The encode loop waits on the buffer's enqueue signal instead of polling
	// on empty. videoSource is always a *FrameBuffer (set in AddVideoTrack) and
	// never swapped (recreateEncoder only replaces encodedReader), so these
	// channels stay valid across encoder recreation.
	fb, ok := track.videoSource.(*FrameBuffer)
	if !ok {
		s.log.Errorf("track %s has no FrameBuffer source; encode loop exiting", trackID)

		return
	}
	frameReady := fb.FrameReady()
	bufClosed := fb.Closed()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		hasFrame, err := s.encodeAndSendTrack(trackID, track)
		if err != nil {
			if s.handleEncodeErr(ctx, err, trackID, frameReady, bufClosed) {
				return
			}

			continue
		}
		if hasFrame {
			track.lastEncodeAtWallUs.Store(time.Now().UnixMicro())
		}
	}
}

// handleEncodeErr processes an error from encodeAndSendTrack. It returns true
// when the encode loop should exit. For ErrNoFrameAvailable the source had no
// frame this iteration; encodeAndSendTrack has already released the vpx encoder
// mutex (held only across Read), so we wait OUTSIDE it — recreateEncoder and
// DynamicQPControl bitrate updates can acquire encoderMu.Lock while we idle. We
// block on the enqueue signal instead of polling so a freshly pushed frame is
// picked up immediately, eliminating poll-induced buffer-wait latency.
func (s *RTCSender) handleEncodeErr(
	ctx context.Context, err error, trackID string,
	frameReady, bufClosed <-chan struct{},
) bool {
	if ctx.Err() != nil || errors.Is(err, ErrBufferClosed) {
		return true
	}
	if errors.Is(err, ErrNoFrameAvailable) {
		select {
		case <-ctx.Done():
			return true
		case <-bufClosed:
			return true
		case <-frameReady:
			return false
		}
	}
	s.log.Errorf("Error processing track %s: %v", trackID, err)

	return false
}

// encodeAndSendTrack reads, encodes, and sends a single track's frame.
// Returns true if a frame was successfully processed, or an error.
func (s *RTCSender) encodeAndSendTrack(trackID string, track *EncodedTrack) (bool, error) {
	s.tracksMu.RLock()
	videoTrack := track.videoTrack
	videoSource := track.videoSource
	onEncodedFrame := s.onEncodedFrame
	onFrameSent := s.onFrameSent
	s.tracksMu.RUnlock()

	// encoderMu.RLock spans Read..release so recreateEncoder cannot
	// swap or close the encoder underneath an in-flight read.
	track.encoderMu.RLock()
	defer track.encoderMu.RUnlock()

	// Read includes VP8 encode — this is the expensive call.
	tBeforeRead := time.Now()
	encoded, release, err := track.encodedReader.Read()
	if err != nil {
		return false, err
	}
	tAfterRead := time.Now()

	// Per-frame stamps; non-FrameBuffer sources report 0.
	var captureTSUs, dequeuedAtWallUs int64
	if cfs, ok := videoSource.(*FrameBuffer); ok {
		captureTSUs = cfs.LastCaptureTSUs()
		dequeuedAtWallUs = cfs.LastDequeueWallUs()
	}

	track.bitrateMu.Lock()
	track.bitrateTracker.AddFrame(len(encoded.Data), tAfterRead)
	track.bitrateMu.Unlock()

	writeErr := videoTrack.WriteSample(media.Sample{
		Data:     encoded.Data,
		Duration: time.Second / 10,
	})
	if writeErr != nil {
		s.log.Errorf("Error writing sample for track %s: %v", trackID, writeErr)
	}
	tAfterWrite := time.Now()

	track.framesEncoded.Add(1)
	if d := tAfterRead.Sub(tBeforeRead); d > 0 {
		track.totalEncodeTimeNs.Add(uint64(d.Nanoseconds())) //nolint:gosec // G115: bounded > 0 by guard above
	}
	if d := tAfterWrite.Sub(tAfterRead); d > 0 {
		track.totalSendQueueTimeNs.Add(uint64(d.Nanoseconds())) //nolint:gosec // G115: bounded > 0 by guard above
	}

	if onEncodedFrame != nil {
		isKey := len(encoded.Data) > 0 && (encoded.Data[0]&0x01) == 0
		onEncodedFrame(trackID, encoded.Data, isKey)
	}

	if onFrameSent != nil {
		onFrameSent(trackID, captureTSUs, dequeuedAtWallUs,
			tAfterRead.UnixMicro(), tAfterWrite.UnixMicro(), writeErr != nil)
	}

	release()

	return true, nil
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

	s.recreateEncodersForActivatedTracks(normalizedAllocation)

	s.bitrateAllocation = normalizedAllocation
	s.log.Infof("Updated bitrate allocation: %v (normalized from %v)", normalizedAllocation, allocation)

	return nil
}

// recreateEncodersForActivatedTracks recreates VP8 encoders for tracks
// transitioning from inactive (0 allocation) to active (non-zero).
// This resets the encoder's internal tLastFrame timestamp, preventing
// vpx_codec_encode from receiving an extremely large duration value
// that causes VPX_CODEC_INVALID_PARAM (error 8).
// Must be called while holding tracksMu.Lock.
func (s *RTCSender) recreateEncodersForActivatedTracks(newAllocation map[string]float64) {
	for trackID, newValue := range newAllocation {
		if newValue < 1e-6 {
			continue
		}

		oldValue := s.bitrateAllocation[trackID]
		if oldValue >= 1e-6 {
			continue
		}

		track := s.tracks[trackID]

		s.log.Infof("Track %s becoming active (allocation %.4f -> %.4f), recreating encoder", trackID, oldValue, newValue)

		if err := s.recreateEncoder(track); err != nil {
			s.log.Errorf("Failed to recreate encoder for track %s: %v", trackID, err)
		}
	}
}

// recreateEncoder closes and recreates the VP8 encoder for a track.
// This resets the encoder's internal tLastFrame timestamp, preventing
// vpx_codec_encode from receiving an extremely large duration value
// after a track has been idle for a long time.
// Must be called while holding tracksMu.Lock.
func (s *RTCSender) recreateEncoder(track *EncodedTrack) error {
	// encoderMu.Lock excludes the per-track encode goroutine for the
	// duration of the swap. Without this, the goroutine can be inside
	// encodedReader.Read or holding a release() callback while we close
	// the old reader, racing on encoder state and FrameBuffer access.
	track.encoderMu.Lock()
	defer track.encoderMu.Unlock()

	// Temporarily reset FrameBuffer to uninitialized so NewEncodedReader
	// can read a black frame for codec property detection during init.
	// Always restore to initialized afterward, regardless of success or failure.
	if fb, ok := track.videoSource.(*FrameBuffer); ok {
		fb.ResetInitialized()
		defer fb.SetInitialized()
	}

	// Create the new encoder FIRST, before closing the old one.
	// If creation fails, the old encoder remains functional.
	encodedReader, err := track.mediaTrack.NewEncodedReader(track.mimeType)
	if err != nil {
		return fmt.Errorf("failed to recreate encoder: %w", err)
	}

	// New encoder is ready — now close the old one and swap.
	_ = track.encodedReader.Close()
	track.encodedReader = encodedReader

	return nil
}

// ForceKeyFrame forces the next frame for the given track to be a keyframe.
func (s *RTCSender) ForceKeyFrame(trackID string) error {
	s.tracksMu.RLock()
	defer s.tracksMu.RUnlock()

	track, ok := s.tracks[trackID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrTrackNotFound, trackID)
	}

	if kfc, ok := track.encodedReader.Controller().(codec.KeyFrameController); ok {
		return kfc.ForceKeyFrame()
	}

	return fmt.Errorf("%w: %s", ErrForceKeyFrameNotSupported, trackID)
}

// Close releases all resources.
func (s *RTCSender) Close() error {
	// Cancel RTCP reader goroutines before closing the peer connection.
	if s.rtcpCancel != nil {
		s.rtcpCancel()
		s.rtcpCancel = nil
	}

	s.tracksMu.RLock()
	tracks := make([]*EncodedTrack, 0, len(s.tracks))
	for _, track := range s.tracks {
		tracks = append(tracks, track)
	}
	s.tracksMu.RUnlock()

	for _, track := range tracks {
		if track.encodeCancel != nil {
			track.encodeCancel()
		}
	}
	// Close the source first; this makes the encode goroutine's pending
	// Read return ErrBufferClosed.
	for _, track := range tracks {
		_ = track.videoSource.Close()
	}
	// Wait for the encode goroutine to exit before closing encodedReader /
	// mediaTrack so Close cannot race a concurrent Read on those.
	for _, track := range tracks {
		if track.encodeDone != nil {
			<-track.encodeDone
		}
	}
	for _, track := range tracks {
		_ = track.encodedReader.Close()
		_ = track.mediaTrack.Close()
	}

	s.tracksMu.Lock()
	s.tracks = make(map[string]*EncodedTrack)
	// TrackLocalStaticSample has no Close method; reset map to avoid stale handles.
	s.audioTracks = make(map[string]*webrtc.TrackLocalStaticSample)
	s.tracksMu.Unlock()

	if s.peerConnection != nil {
		return s.peerConnection.Close()
	}

	return nil
}

// GetPeerConnection returns the WebRTC peer connection.
func (s *RTCSender) GetPeerConnection() *webrtc.PeerConnection {
	return s.peerConnection
}

// GetWebRTCTrackLocal returns the WebRTC track for a specific track ID (video or audio).
func (s *RTCSender) GetWebRTCTrackLocal(trackID string) (*webrtc.TrackLocalStaticSample, error) {
	s.tracksMu.RLock()
	defer s.tracksMu.RUnlock()

	if track, exists := s.tracks[trackID]; exists {
		return track.videoTrack, nil
	}
	if audioTrack, exists := s.audioTracks[trackID]; exists {
		return audioTrack, nil
	}

	return nil, fmt.Errorf("%w: %s", ErrTrackNotFound, trackID)
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

	if len(s.tracks) == 0 {
		return true
	}

	cutoffUs := time.Now().Add(-encodeHealthTimeout).UnixMicro()
	for _, track := range s.tracks {
		if track.lastEncodeAtWallUs.Load() >= cutoffUs {
			return true
		}
	}

	return false
}
