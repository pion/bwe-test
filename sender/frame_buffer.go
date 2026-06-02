// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"errors"
	"image"
	"sync"
	"sync/atomic"
	"time"
)

// Static errors for err113 compliance.
var (
	ErrBufferClosed              = errors.New("buffer closed")
	ErrNoFrameAvailable          = errors.New("no frame available")
	ErrFailedToAddFrameAfterDrop = errors.New("failed to add frame after dropping oldest")
)

// Global shared black frame to avoid repeated allocations.
var (
	sharedBlackFrame *image.YCbCr //nolint:gochecknoglobals // Performance optimization for shared black frame
	blackFrameOnce   sync.Once    //nolint:gochecknoglobals // Required for thread-safe initialization
)

// getBlackFrame returns a shared black frame for the given dimensions
// The black frame is created once on first call and reused for all subsequent calls.
func getBlackFrame(width, height int) *image.YCbCr {
	blackFrameOnce.Do(func() {
		// Create YUV420 black frame
		ySize := width * height
		uvSize := ySize / 4
		totalSize := ySize + 2*uvSize // Y + U + V planes

		// Allocate buffer for YUV420 data
		data := make([]byte, totalSize)

		// Set Y plane to black (0 = black in YUV)
		// Y plane is already zero-initialized (black)

		// Set U and V planes to neutral (128 = neutral chroma) - optimized
		// Use range-based loop for better performance
		uvPlanes := data[ySize:] // Both U and V planes
		for i := range uvPlanes {
			uvPlanes[i] = 128
		}

		sharedBlackFrame = &image.YCbCr{
			Y:              data[:ySize],               // Y plane: full resolution
			Cb:             data[ySize : ySize+uvSize], // U plane: 1/4 resolution
			Cr:             data[ySize+uvSize:],        // V plane: 1/4 resolution
			YStride:        width,                      // Y plane stride
			CStride:        width / 2,                  // Chroma planes stride
			Rect:           image.Rect(0, 0, width, height),
			SubsampleRatio: image.YCbCrSubsampleRatio420,
		}
	})

	return sharedBlackFrame
}

// frameWithMeta pairs a frame with an opaque capture timestamp
// (microseconds). Zero captureTSUs means no timestamp (legacy SendFrame
// or black-frame init).
type frameWithMeta struct {
	img         image.Image
	captureTSUs int64
}

// FrameBuffer is a simple in-memory frame buffer that implements VideoSource
// It can be used as a virtual video driver for testing or programmatic frame injection.
type FrameBuffer struct {
	frameChan         chan frameWithMeta
	closeChan         chan struct{}
	closeOnce         sync.Once
	width             int
	height            int
	id                string
	initialized       bool
	lastCaptureTSUs   atomic.Int64
	lastDequeueWallUs atomic.Int64
}

// NewFrameBuffer creates a new frame buffer with the specified dimensions.
//
// Capacity 2: one in-flight + one staged. Sized for low-latency real-time
// senders where source > encoder rate. When the source is steady (no bursts
// and no encoder slack to drain a backlog), encoder throughput is set by
// encoder CPU capacity regardless of capacity, so a deeper queue only adds
// wait time without delivering more frames. Capacity 2 tolerates one frame
// of arrival/encode jitter; capacity 1 would drop on any same-tick burst.
func NewFrameBuffer(width, height int) *FrameBuffer {
	return &FrameBuffer{
		frameChan: make(chan frameWithMeta, 2),
		closeChan: make(chan struct{}),
		width:     width,
		height:    height,
		id:        "frame-buffer",
	}
}

// ID returns the identifier for this video source.
func (f *FrameBuffer) ID() string {
	return f.id
}

// Close stops the frame buffer and releases resources.
func (f *FrameBuffer) Close() error {
	f.closeOnce.Do(func() {
		close(f.closeChan)
	})

	return nil
}

// SetInitialized marks the frame buffer as initialized.
func (f *FrameBuffer) SetInitialized() {
	f.initialized = true
}

// ResetInitialized temporarily marks the frame buffer as uninitialized.
// This causes Read() to return black frames on timeout instead of ErrNoFrameAvailable,
// which is needed when recreating an encoder (NewEncodedReader reads one frame for
// property detection during initialization).
func (f *FrameBuffer) ResetInitialized() {
	f.initialized = false
}

// Read returns the next available frame from the buffer.
// When initialized (normal operation), it returns immediately with
// ErrNoFrameAvailable if no frame is ready. The vpx encoder's Read holds
// its internal mutex across the inner Read call, so blocking here would
// also block concurrent DynamicQPControl bitrate updates that try to take
// the same mutex. runEncodeLoop handles ErrNoFrameAvailable by sleeping
// briefly before retrying. When not initialized (encoder init), it blocks
// up to 100ms and returns a black frame for codec property detection.
//
// Side effect: stores the popped frame's captureTSUs for LastCaptureTSUs.
// Black-frame timeouts preserve the previous value so encoders with
// lookahead can still correlate the previously-read real frame.
func (f *FrameBuffer) Read() (image.Image, func(), error) {
	if f.initialized {
		// Check closeChan first so a closed buffer always reports
		// ErrBufferClosed instead of racing with the default branch below
		// (Go's select picks ready cases pseudo-randomly).
		select {
		case <-f.closeChan:
			return nil, func() {}, ErrBufferClosed
		default:
		}
		// Non-blocking fast path for normal operation.
		select {
		case fm := <-f.frameChan:
			f.lastCaptureTSUs.Store(fm.captureTSUs)
			f.lastDequeueWallUs.Store(time.Now().UnixMicro())

			return fm.img, func() {}, nil
		default:
			return nil, func() {}, ErrNoFrameAvailable
		}
	}

	// Blocking path for encoder initialization — returns black frame on timeout.
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()

	select {
	case fm := <-f.frameChan:
		f.lastCaptureTSUs.Store(fm.captureTSUs)
		f.lastDequeueWallUs.Store(time.Now().UnixMicro())

		return fm.img, func() {}, nil
	case <-f.closeChan:
		return nil, func() {}, ErrBufferClosed
	case <-timer.C:
		blackFrame := getBlackFrame(f.width, f.height)

		return blackFrame, func() {}, nil
	}
}

// LastCaptureTSUs returns the captureTSUs of the most recently popped
// frame, or 0 if no real frame has been consumed yet. Intended for the
// encode loop to call right after encodedReader.Read.
func (f *FrameBuffer) LastCaptureTSUs() int64 {
	return f.lastCaptureTSUs.Load()
}

// LastDequeueWallUs returns time.Now().UnixMicro() at the most recent
// real-frame Read (the queue-exit instant), or 0 if no real frame has
// been consumed yet. Intended for the encode loop to read right after
// encodedReader.Read.
func (f *FrameBuffer) LastDequeueWallUs() int64 {
	return f.lastDequeueWallUs.Load()
}

// SendFrame adds a frame to the buffer with no capture timestamp.
// If the buffer is full, it drops the oldest frame and adds the new one.
func (f *FrameBuffer) SendFrame(frame image.Image) error {
	_, err := f.SendFrameWithCaptureTS(frame, 0)

	return err
}

// SendFrameWithCaptureTS adds a frame with an opaque capture timestamp
// (microseconds), retrievable via LastCaptureTSUs after the encode
// pipeline pops it. Pass 0 to opt out. evicted is true when the buffer
// was full and the oldest entry was discarded to make room.
func (f *FrameBuffer) SendFrameWithCaptureTS(frame image.Image, captureTSUs int64) (evicted bool, err error) {
	select {
	case <-f.closeChan:
		return false, ErrBufferClosed
	default:
	}

	fm := frameWithMeta{img: frame, captureTSUs: captureTSUs}

	select {
	case f.frameChan <- fm:
		return false, nil
	default:
		// Buffer full - drop oldest frame and add the new one.
		select {
		case <-f.frameChan:
			evicted = true
		default:
			// Buffer drained between the full-select and here.
		}

		select {
		case f.frameChan <- fm:
			return evicted, nil
		default:
			return evicted, ErrFailedToAddFrameAfterDrop
		}
	}
}
