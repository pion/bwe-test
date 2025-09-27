// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package syncodec

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

// Constants for the statistical codec.
const (
	defaultTargetBitrateBps = 1_000_000 // 1 Mbps
	defaultFPS              = 30
	defaultTau              = 200 * time.Millisecond
	defaultBurstFrameCount  = 8
	defaultBurstFrameSize   = 13_500 // 13.5 KB
	defaultT0               = 33 * time.Millisecond
	defaultB0               = 4_170 // 4.17 KB

	// Scaling parameter of zero-mean laplacian distribution describing
	// deviations in normalized frame interval.
	defaultScaleT = 0.15

	// Scaling parameter of zero-mean laplacian distribution describing
	// deviations in normalized frame size.
	defaultScaleB = 0.15

	defaultRMin = 150_000     // 150 kbps
	defaultRMax = 150_000_000 // 150 Mbps
)

// noiser defines an interface for adding noise to values.
type noiser interface {
	noise() float64
}

// laplaceNoise implements the noiser interface using a Laplace distribution.
type laplaceNoise struct {
	rnd   *rand.Rand
	scale float64
}

// noise returns a random value from a Laplace distribution.
func (l laplaceNoise) noise() float64 {
	if l.rnd == nil {
		//nolint:gosec
		l.rnd = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	e1 := -l.scale * math.Log(l.rnd.Float64())
	e2 := -l.scale * math.Log(l.rnd.Float64())

	return e1 - e2
}

var _ Codec = (*StatisticalCodec)(nil)

// StatisticalCodec implements a codec that produces frames with sizes and timings
// that follow statistical distributions to simulate real-world codecs.
type StatisticalCodec struct {
	// requested target bitrate
	targetBitrateBps int

	// Frames per second
	fps int

	// encoder reaction latency
	tau time.Duration

	// burst duration of transient period in frames
	burstFrameCount int

	// burst frame size during transient period
	burstFrameSize int

	// reference time interval 1/FPS
	t0 time.Duration

	// reference frame size targetBitrateBps / fps
	b0 int

	// min rate supported by video encoder
	rMin int

	// max rate supported by video encoder
	rMax int

	// output writer
	writer FrameWriter

	// scaling parameter of zero-mean laplacian distribution describing
	// deviations in normalized frame size
	scaleB float64

	// scaling parameter of zero-mean laplacian distribution describing
	// deviations in normalized frame interval
	scaleT float64

	// internal types
	targetBitrateLock       sync.Mutex
	targetBitrateChan       chan int
	lastTargetBitrateUpdate time.Time

	remainingBurstFrames int

	frameSizeNoiser     noiser
	frameDurationNoiser noiser

	done chan struct{}
}

// StatisticalCodecOption is a function that configures a StatisticalCodec.
type StatisticalCodecOption func(*StatisticalCodec) error

// WithInitialTargetBitrate sets the initial target bitrate for the codec.
func WithInitialTargetBitrate(targetBitrateBps int) StatisticalCodecOption {
	return func(sc *StatisticalCodec) error {
		sc.targetBitrateBps = targetBitrateBps

		return nil
	}
}

// WithFramesPerSecond sets the frames per second for the codec.
func WithFramesPerSecond(fps int) StatisticalCodecOption {
	return func(sc *StatisticalCodec) error {
		sc.fps = fps

		return nil
	}
}

// WithScaleB sets the scaling parameter for frame size noise.
func WithScaleB(scale float64) StatisticalCodecOption {
	return func(sc *StatisticalCodec) error {
		sc.scaleB = scale

		return nil
	}
}

// WithScaleT sets the scaling parameter for frame timing noise.
func WithScaleT(scale float64) StatisticalCodecOption {
	return func(sc *StatisticalCodec) error {
		sc.scaleT = scale

		return nil
	}
}

// minimum returns the minimum of two integers.
func minimum(a, b int) int {
	if a < b {
		return a
	}

	return b
}

// maximum returns the maximum of two integers.
func maximum(a, b int) int {
	if a > b {
		return a
	}

	return b
}

// NewStatisticalEncoder creates a new StatisticalCodec with the given frame writer and options.
func NewStatisticalEncoder(w FrameWriter, opts ...StatisticalCodecOption) (*StatisticalCodec, error) {
	sc := &StatisticalCodec{
		targetBitrateBps:        defaultTargetBitrateBps,
		fps:                     defaultFPS,
		tau:                     defaultTau,
		burstFrameCount:         defaultBurstFrameCount,
		burstFrameSize:          defaultBurstFrameSize,
		t0:                      defaultT0,
		b0:                      defaultB0,
		rMin:                    defaultRMin,
		rMax:                    defaultRMax,
		writer:                  w,
		scaleB:                  defaultScaleB,
		scaleT:                  defaultScaleT,
		targetBitrateLock:       sync.Mutex{},
		targetBitrateChan:       make(chan int),
		lastTargetBitrateUpdate: time.Time{},
		remainingBurstFrames:    0,
		frameSizeNoiser:         nil,
		frameDurationNoiser:     nil,
		done:                    make(chan struct{}),
	}

	for _, opt := range opts {
		if err := opt(sc); err != nil {
			return nil, err
		}
	}

	sc.frameSizeNoiser = laplaceNoise{
		//nolint:gosec
		rnd:   rand.New(rand.NewSource(time.Now().UnixNano())),
		scale: sc.scaleB,
	}
	sc.frameDurationNoiser = laplaceNoise{
		//nolint:gosec
		rnd:   rand.New(rand.NewSource(time.Now().UnixNano())),
		scale: sc.scaleT,
	}
	sc.SetTargetBitrate(sc.targetBitrateBps)

	return sc, nil
}

// GetTargetBitrate returns the current target bitrate in bit per second.
func (c *StatisticalCodec) GetTargetBitrate() int {
	c.targetBitrateLock.Lock()
	defer c.targetBitrateLock.Unlock()

	return c.targetBitrateBps
}

// SetTargetBitrate sets the target bitrate to r bits per second. If r is
// greater than c.rMax, bitrate will be set to c.rMax. If r is lower than
// c.rMin, bitrate will be set to c.rMin.
func (c *StatisticalCodec) SetTargetBitrate(r int) {
	c.targetBitrateLock.Lock()
	defer c.targetBitrateLock.Unlock()

	if r < c.targetBitrateBps {
		c.targetBitrateBps = maximum(r, c.rMin)

		return
	}
	c.targetBitrateBps = minimum(r, c.rMax)
}

// nextFrame returns the next faked video frame.
func (c *StatisticalCodec) nextFrame() Frame {
	duration := time.Duration((1.0/float64(c.fps))*1000.0) * time.Millisecond

	if c.remainingBurstFrames == c.burstFrameCount {
		return Frame{
			Content:  make([]byte, c.burstFrameSize),
			Duration: duration,
		}
	}

	c.targetBitrateLock.Lock()
	bps := c.targetBitrateBps
	c.targetBitrateLock.Unlock()

	bytesPerFrame := bps / (8.0 * c.fps)

	if c.remainingBurstFrames > 0 {
		size := (bps * c.burstFrameCount) / (c.burstFrameSize + (c.burstFrameCount - 1))

		return Frame{
			Content:  make([]byte, size),
			Duration: duration,
		}
	}

	noisedBytesPerFrame := math.Max(1, float64(bytesPerFrame)*(1-c.frameSizeNoiser.noise()))
	noisedDuration := math.Max(0, float64(duration)*(1-c.frameDurationNoiser.noise()))

	return Frame{
		Content:  make([]byte, int(noisedBytesPerFrame)),
		Duration: time.Duration(noisedDuration),
	}
}

// Start starts the StatisticalCodec.
func (c *StatisticalCodec) Start() {
	timer := time.NewTimer(c.t0)
	for {
		select {
		case <-timer.C:
			nextFrame := c.nextFrame()
			timer.Reset(nextFrame.Duration)
			c.writer.WriteFrame(nextFrame)

		case rate := <-c.targetBitrateChan:
			if time.Since(c.lastTargetBitrateUpdate) < c.tau {
				continue
			}
			c.targetBitrateLock.Lock()
			c.targetBitrateBps = rate
			c.targetBitrateLock.Unlock()
			c.lastTargetBitrateUpdate = time.Now()
			c.remainingBurstFrames = c.burstFrameCount

		case <-c.done:
			return
		}
	}
}

// Close stops and closes the StatisticalCodec.
func (c *StatisticalCodec) Close() error {
	close(c.done)

	return nil
}
