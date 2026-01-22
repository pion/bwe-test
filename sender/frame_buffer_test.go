//go:build !js
// +build !js

// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"image"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFrameBuffer(t *testing.T) {
	fb := NewFrameBuffer(1280, 720)
	require.NotNil(t, fb)

	assert.Equal(t, "frame-buffer", fb.ID())

	// Test that it implements VideoSource interface
	var _ VideoSource = fb
}

func TestFrameBuffer_SendAndRead(t *testing.T) {
	fb := NewFrameBuffer(640, 480)
	defer func() { _ = fb.Close() }()

	// Create a simple test image
	testImg := image.NewRGBA(image.Rect(0, 0, 640, 480))

	// Send a frame
	err := fb.SendFrame(testImg)
	require.NoError(t, err)

	// Mark as initialized so we don't get black frames
	fb.SetInitialized()

	// Read the frame back
	img, release, err := fb.Read()
	require.NoError(t, err)
	require.NotNil(t, img)
	require.NotNil(t, release)

	// Release the frame
	release()
}

func TestFrameBuffer_ReadTimeout(t *testing.T) {
	fb := NewFrameBuffer(640, 480)
	defer func() { _ = fb.Close() }()

	// Don't mark as initialized, so we should get a black frame on timeout
	img, release, err := fb.Read()
	require.NoError(t, err)
	require.NotNil(t, img)
	require.NotNil(t, release)

	// Should be a YCbCr image (black frame)
	_, ok := img.(*image.YCbCr)
	assert.True(t, ok, "Expected YCbCr image for black frame")

	release()
}

func TestFrameBuffer_ReadAfterClose(t *testing.T) {
	fb := NewFrameBuffer(640, 480)

	// Close the buffer
	err := fb.Close()
	require.NoError(t, err)

	// Try to read after close
	img, release, err := fb.Read()
	assert.Nil(t, img)
	assert.NotNil(t, release) // Release function should still be provided
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBufferClosed)
}

func TestFrameBuffer_SendAfterClose(t *testing.T) {
	fb := NewFrameBuffer(640, 480)

	// Close the buffer
	err := fb.Close()
	require.NoError(t, err)

	// Try to send after close
	testImg := image.NewRGBA(image.Rect(0, 0, 640, 480))
	err = fb.SendFrame(testImg)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBufferClosed)
}

func TestFrameBuffer_BufferFull(t *testing.T) {
	fb := NewFrameBuffer(640, 480)
	defer func() { _ = fb.Close() }()

	// Fill the buffer beyond capacity (buffer size is 8)
	testImg := image.NewRGBA(image.Rect(0, 0, 640, 480))

	// Send more frames than buffer capacity
	for range 10 {
		err := fb.SendFrame(testImg)
		// Should not error - it should drop oldest frames
		assert.NoError(t, err)
	}
}

func TestFrameBuffer_ReadWithoutFrames(t *testing.T) {
	fb := NewFrameBuffer(640, 480)
	defer func() { _ = fb.Close() }()

	// Mark as initialized so we don't get black frames
	fb.SetInitialized()

	// Try to read without sending any frames
	// This should timeout and return ErrNoFrameAvailable
	img, release, err := fb.Read()
	assert.Nil(t, img)
	assert.NotNil(t, release)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoFrameAvailable)
}

func TestFrameBuffer_MultipleClose(t *testing.T) {
	fb := NewFrameBuffer(640, 480)

	// Close multiple times should not panic
	err1 := fb.Close()
	err2 := fb.Close()

	assert.NoError(t, err1)
	assert.NoError(t, err2)
}

func TestGetBlackFrame(t *testing.T) {
	// Test that getBlackFrame returns a valid black frame
	blackFrame := getBlackFrame(640, 480)
	require.NotNil(t, blackFrame)

	assert.Equal(t, 640, blackFrame.Bounds().Dx())
	assert.Equal(t, 480, blackFrame.Bounds().Dy())

	// Test that multiple calls return the same instance (singleton)
	blackFrame2 := getBlackFrame(640, 480)
	assert.Equal(t, blackFrame, blackFrame2, "getBlackFrame should return the same instance")
}

func TestFrameBufferStaticErrors(t *testing.T) {
	// Test that all frame buffer static errors are properly defined
	assert.NotNil(t, ErrBufferClosed)
	assert.NotNil(t, ErrNoFrameAvailable)
	assert.NotNil(t, ErrFailedToAddFrameAfterDrop)

	// Test error messages
	assert.Contains(t, ErrBufferClosed.Error(), "closed")
	assert.Contains(t, ErrNoFrameAvailable.Error(), "no frame")
	assert.Contains(t, ErrFailedToAddFrameAfterDrop.Error(), "failed to add")
}

func TestFrameBuffer_ConcurrentAccess(t *testing.T) {
	fb := NewFrameBuffer(640, 480)
	defer func() { _ = fb.Close() }()

	fb.SetInitialized()

	// Test concurrent sends and reads
	done := make(chan bool)

	// Sender goroutine
	go func() {
		defer func() { done <- true }()
		testImg := image.NewRGBA(image.Rect(0, 0, 640, 480))
		for range 5 {
			_ = fb.SendFrame(testImg)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Reader goroutine
	go func() {
		defer func() { done <- true }()
		for range 3 {
			img, release, err := fb.Read()
			if err == nil && img != nil {
				release()
			}
			time.Sleep(15 * time.Millisecond)
		}
	}()

	// Wait for both goroutines to complete
	<-done
	<-done

	// Test should complete without deadlock or panic
}
