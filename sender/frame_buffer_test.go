//go:build !js

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

	// Mark as initialized so we take the non-blocking fast path.
	fb.SetInitialized()

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

func TestFrameBuffer_ReadInitializedAfterClose(t *testing.T) {
	fb := NewFrameBuffer(640, 480)
	fb.SetInitialized()

	// Close, then read — the closeChan precedence check should win over
	// the default branch in the non-blocking select.
	err := fb.Close()
	require.NoError(t, err)

	img, release, err := fb.Read()
	assert.Nil(t, img)
	assert.NotNil(t, release)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBufferClosed)
}

func TestFrameBuffer_ReadUninitializedWithFrame(t *testing.T) {
	fb := NewFrameBuffer(640, 480)
	defer func() { _ = fb.Close() }()

	// Don't call SetInitialized — this exercises the blocking path.
	// Send a frame before reading so the blocking select picks it up.
	testImg := image.NewRGBA(image.Rect(0, 0, 640, 480))
	err := fb.SendFrame(testImg)
	require.NoError(t, err)

	img, release, err := fb.Read()
	require.NoError(t, err)
	require.NotNil(t, img)
	assert.Equal(t, testImg, img)
	release()
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

func TestFrameBuffer_SendWithCaptureTs_RoundTrips(t *testing.T) {
	fb := NewFrameBuffer(640, 480)
	defer func() { _ = fb.Close() }()
	fb.SetInitialized()

	testImg := image.NewRGBA(image.Rect(0, 0, 640, 480))

	evicted, err := fb.SendFrameWithCaptureTS(testImg, 111)
	require.NoError(t, err)
	require.False(t, evicted)
	evicted, err = fb.SendFrameWithCaptureTS(testImg, 222)
	require.NoError(t, err)
	require.False(t, evicted)

	// Each Read advances LastCaptureTSUs to the timestamp of the frame
	// that was just returned.
	_, release, err := fb.Read()
	require.NoError(t, err)
	release()
	assert.Equal(t, int64(111), fb.LastCaptureTSUs())

	_, release, err = fb.Read()
	require.NoError(t, err)
	release()
	assert.Equal(t, int64(222), fb.LastCaptureTSUs())
}

func TestFrameBuffer_LegacySendFrame_RecordsZeroCaptureTs(t *testing.T) {
	fb := NewFrameBuffer(640, 480)
	defer func() { _ = fb.Close() }()
	fb.SetInitialized()

	testImg := image.NewRGBA(image.Rect(0, 0, 640, 480))

	// Seed a non-zero value to confirm Read overwrites it on legacy send.
	_, err := fb.SendFrameWithCaptureTS(testImg, 999)
	require.NoError(t, err)
	_, release, err := fb.Read()
	require.NoError(t, err)
	release()
	require.Equal(t, int64(999), fb.LastCaptureTSUs())

	require.NoError(t, fb.SendFrame(testImg))
	_, release, err = fb.Read()
	require.NoError(t, err)
	release()
	assert.Equal(t, int64(0), fb.LastCaptureTSUs(),
		"legacy SendFrame should result in zero LastCaptureTSUs")
}

func TestFrameBuffer_BlackFrame_PreservesCaptureTS(t *testing.T) {
	fb := NewFrameBuffer(640, 480)
	defer func() { _ = fb.Close() }()

	// Seed a value via the buffered path while still uninitialized so it
	// is consumed by the encoder-init Read first, then a subsequent Read
	// hits the black-frame timeout and must NOT clobber the recorded
	// value — codecs with lookahead may emit an encoded output that
	// corresponds to this still-buffered real frame after the timeout
	// Read happens, and the callback for that emission must see 555.
	testImg := image.NewRGBA(image.Rect(0, 0, 640, 480))
	_, err := fb.SendFrameWithCaptureTS(testImg, 555)
	require.NoError(t, err)

	// First Read pulls the buffered frame and records 555.
	_, release, err := fb.Read()
	require.NoError(t, err)
	release()
	require.Equal(t, int64(555), fb.LastCaptureTSUs())

	// Second Read drops into the 100ms timeout path and returns a black
	// frame; LastCaptureTSUs must still report 555.
	_, release, err = fb.Read()
	require.NoError(t, err)
	release()
	assert.Equal(t, int64(555), fb.LastCaptureTSUs(),
		"black-frame Read should preserve LastCaptureTSUs from the previous real frame")
}

func TestFrameBuffer_BlackFrame_FirstReadReturnsZero(t *testing.T) {
	fb := NewFrameBuffer(640, 480)
	defer func() { _ = fb.Close() }()

	// A fresh buffer that has never seen a real frame must report 0
	// from a black-frame Read — the atomic's zero value is the sentinel
	// for "no real frame ever consumed".
	_, release, err := fb.Read()
	require.NoError(t, err)
	release()
	assert.Equal(t, int64(0), fb.LastCaptureTSUs(),
		"first black-frame Read on a fresh buffer should return 0")
}

func TestFrameBuffer_LastDequeueWallUs_AdvancesOnRead(t *testing.T) {
	fb := NewFrameBuffer(640, 480)
	defer func() { _ = fb.Close() }()
	fb.SetInitialized()

	assert.Zero(t, fb.LastDequeueWallUs(), "fresh buffer should report zero")

	testImg := image.NewRGBA(image.Rect(0, 0, 640, 480))
	_, err := fb.SendFrameWithCaptureTS(testImg, 111)
	require.NoError(t, err)

	before := time.Now().UnixMicro()
	_, release, err := fb.Read()
	require.NoError(t, err)
	release()
	after := time.Now().UnixMicro()

	got := fb.LastDequeueWallUs()
	assert.GreaterOrEqual(t, got, before, "dequeue stamp must be >= pre-Read wall clock")
	assert.LessOrEqual(t, got, after, "dequeue stamp must be <= post-Read wall clock")
}

func TestFrameBuffer_LastDequeueWallUs_PreservedOnBlackFrame(t *testing.T) {
	fb := NewFrameBuffer(640, 480)
	defer func() { _ = fb.Close() }()

	testImg := image.NewRGBA(image.Rect(0, 0, 640, 480))
	_, err := fb.SendFrameWithCaptureTS(testImg, 555)
	require.NoError(t, err)

	// First Read consumes the buffered frame and stamps a real dequeue.
	_, release, err := fb.Read()
	require.NoError(t, err)
	release()
	stampAfterReal := fb.LastDequeueWallUs()
	require.Positive(t, stampAfterReal)

	// Second Read hits the 100ms timeout (uninitialized + empty) and
	// returns a black frame; LastDequeueWallUs must NOT be clobbered.
	_, release, err = fb.Read()
	require.NoError(t, err)
	release()
	assert.Equal(t, stampAfterReal, fb.LastDequeueWallUs(),
		"black-frame Read should preserve LastDequeueWallUs")
}

func TestFrameBuffer_SendWithCaptureTs_OverflowReportsEviction(t *testing.T) {
	fb := NewFrameBuffer(640, 480)
	defer func() { _ = fb.Close() }()

	// Buffer capacity is 2. The first 2 sends fit; the 3rd must evict the
	// oldest entry so downstream SLO accounting can see the overload.
	testImg := image.NewRGBA(image.Rect(0, 0, 640, 480))
	for i := range 2 {
		evicted, err := fb.SendFrameWithCaptureTS(testImg, int64(i+1))
		require.NoError(t, err)
		require.False(t, evicted, "send %d should not evict before capacity", i)
	}
	evicted, err := fb.SendFrameWithCaptureTS(testImg, 3)
	require.NoError(t, err)
	assert.True(t, evicted, "send beyond capacity should report eviction")
}
