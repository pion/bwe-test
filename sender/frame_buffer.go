// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"errors"
	"image"
	"sync"
)

// Static errors for err113 compliance.
var (
	ErrBufferClosed              = errors.New("buffer closed")
	ErrNoFrameAvailable          = errors.New("no frame available")
	ErrFailedToAddFrameAfterDrop = errors.New("failed to add frame after dropping oldest")
)

// FrameBuffer is a simple in-memory frame buffer that implements VideoSource
// It can be used as a virtual video driver for testing or programmatic frame injection.
type FrameBuffer struct {
	frameChan chan image.Image
	closeChan chan struct{}
	closeOnce sync.Once
	width     int
	height    int
	id        string
}

// NewFrameBuffer creates a new frame buffer with the specified dimensions.
func NewFrameBuffer(width, height int) *FrameBuffer {
	return &FrameBuffer{
		frameChan: make(chan image.Image, 8),
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

// Read returns the next frame from the buffer if one is available.
// Returns ErrNoFrameAvailable immediately if the buffer is empty.
func (f *FrameBuffer) Read() (image.Image, func(), error) {
	select {
	case img := <-f.frameChan:
		return img, func() {}, nil
	case <-f.closeChan:
		return nil, func() {}, ErrBufferClosed
	default:
		return nil, func() {}, ErrNoFrameAvailable
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
		return nil
	default:
		// Buffer full - drop oldest frame and add the new one
		select {
		case <-f.frameChan: // Remove oldest
		default:
			// Buffer was empty (race condition)
		}

		select {
		case f.frameChan <- frame:
			return nil
		default:
			return ErrFailedToAddFrameAfterDrop
		}
	}
}
