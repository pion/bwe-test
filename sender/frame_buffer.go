// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"errors"
	"image"
	"sync"
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

// FrameBuffer is a simple in-memory frame buffer that implements VideoSource
// It can be used as a virtual video driver for testing or programmatic frame injection.
type FrameBuffer struct {
	frameChan   chan image.Image
	closeChan   chan struct{}
	closeOnce   sync.Once
	width       int
	height      int
	id          string
	initialized bool
}

// NewFrameBuffer creates a new frame buffer with the specified dimensions.
func NewFrameBuffer(width, height int) *FrameBuffer {
	return &FrameBuffer{
		frameChan: make(chan image.Image, 8), // Increased from 2 to 8 for better buffering
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
// When initialized (normal operation), returns immediately with ErrNoFrameAvailable
// if no frame is ready. When not initialized (encoder init), blocks up to 100ms
// and returns a black frame for codec property detection.
func (f *FrameBuffer) Read() (image.Image, func(), error) {
	if f.initialized {
		// Non-blocking fast path for normal operation.
		select {
		case img := <-f.frameChan:
			return img, func() {}, nil
		case <-f.closeChan:
			return nil, func() {}, ErrBufferClosed
		default:
			return nil, func() {}, ErrNoFrameAvailable
		}
	}

	// Blocking path for encoder initialization — returns black frame on timeout.
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()

	select {
	case img := <-f.frameChan:
		return img, func() {}, nil
	case <-f.closeChan:
		return nil, func() {}, ErrBufferClosed
	case <-timer.C:
		blackFrame := getBlackFrame(f.width, f.height)

		return blackFrame, func() {}, nil
	}
}

// SendFrame adds a frame to the buffer.
// If the buffer is full, it drops the oldest frame and adds the new one.
func (f *FrameBuffer) SendFrame(frame image.Image) error {
	select {
	case <-f.closeChan:
		return ErrBufferClosed
	default:
	}

	select {
	case f.frameChan <- frame:
		// Successfully added frame
		return nil
	default:
		// Buffer full - drop oldest frame and add the new one
		select {
		case <-f.frameChan: // Remove oldest
			// Successfully removed old frame
		default:
			// Buffer was empty (race condition)
		}

		// Now add the new frame
		select {
		case f.frameChan <- frame:
			return nil
		default:
			// Still can't add (shouldn't happen)
			return ErrFailedToAddFrameAfterDrop
		}
	}
}
