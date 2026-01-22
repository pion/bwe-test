// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package receiver

import (
	"errors"
	"fmt"
	"image"
	"io"
	"sync"
	"time"

	"github.com/pion/logging"
	"github.com/pion/mediadevices/pkg/codec"
	"github.com/pion/mediadevices/pkg/codec/vpx"
	"github.com/pion/mediadevices/pkg/prop"
)

var (
	// ErrDecoderCreationFailed is returned when VP8 decoder creation fails.
	ErrDecoderCreationFailed = errors.New("VP8 decoder creation failed")
	// ErrDecoderCloseFailed is returned when decoder close operation fails.
	ErrDecoderCloseFailed = errors.New("decoder close failed")
)

// frameFeeder implements io.Reader to feed frames to the decoder.
type frameFeeder struct {
	frameChan chan []byte
	current   []byte
	offset    int
	mu        sync.Mutex
}

func newFrameFeeder() *frameFeeder {
	return &frameFeeder{
		frameChan: make(chan []byte, 10),
	}
}

func (f *frameFeeder) Read(buffer []byte) (n int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// If we have data in current frame, read from it
	if f.current != nil && f.offset < len(f.current) {
		n = copy(buffer, f.current[f.offset:])
		f.offset += n
		if f.offset >= len(f.current) {
			f.current = nil
			f.offset = 0
		}

		return n, nil
	}

	// Otherwise, get next frame from channel
	select {
	case frame := <-f.frameChan:
		if frame == nil {
			return 0, io.EOF
		}
		f.current = frame
		f.offset = 0
		n = copy(buffer, f.current[f.offset:])
		f.offset += n
		if f.offset >= len(f.current) {
			f.current = nil
			f.offset = 0
		}

		return n, nil
	default:
		return 0, nil // No data available, don't block
	}
}

func (f *frameFeeder) feedFrame(data []byte) {
	defer func() {
		// Recover from panic if channel was closed
		_ = recover()
	}()

	select {
	case f.frameChan <- data:
	default:
		// Channel full, drop frame
	}
}

func (f *frameFeeder) close() {
	close(f.frameChan)
}

// VP8FrameProcessor handles VP8 frame processing pipeline.
type VP8FrameProcessor struct {
	decoder          codec.VideoDecoder
	feeder           *frameFeeder
	frameCounter     int
	gotFirstKeyframe bool
	trackID          string
	log              logging.LeveledLogger

	// Callback for frame processing
	frameCallback VideoWriterCallback

	done     chan struct{}
	finished chan struct{} // Signal when goroutine is finished
	mu       sync.RWMutex
}

// VideoWriterCallback is the callback function type for video writing.
type VideoWriterCallback func(
	img image.Image, width, height int, isKeyFrame bool, timestamp uint64, trackID string,
)

// NewVP8FrameProcessor creates a new VP8 frame processor with callback.
func NewVP8FrameProcessor(
	width, height int, trackID string, frameCallback VideoWriterCallback, logger logging.LeveledLogger,
) (*VP8FrameProcessor, error) {
	// Create frame feeder
	feeder := newFrameFeeder()

	// Create decoder with proper media properties
	decoder, err := vpx.NewDecoder(feeder, prop.Media{
		Video: prop.Video{
			Width:  width,
			Height: height,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDecoderCreationFailed, err)
	}

	vp8Processor := &VP8FrameProcessor{
		decoder:          decoder,
		feeder:           feeder,
		frameCounter:     0,
		gotFirstKeyframe: false,
		trackID:          trackID,
		log:              logger,
		frameCallback:    frameCallback,
		done:             make(chan struct{}),
		finished:         make(chan struct{}),
	}

	// Start goroutine to continuously read decoded frames
	go func() {
		defer close(vp8Processor.finished) // Signal when we're done
		for {
			select {
			case <-vp8Processor.done:
				return
			default:
				// Try to read a decoded frame
				img, release, err := vp8Processor.decoder.Read()
				if err != nil {
					if !errors.Is(err, io.EOF) {
						// Only sleep on non-EOF errors
						time.Sleep(10 * time.Millisecond)
					}

					continue
				}

				if img != nil {
					vp8Processor.mu.Lock()
					frameCount := vp8Processor.frameCounter
					vp8Processor.frameCounter++
					vp8Processor.mu.Unlock()

					// Get dimensions from the image
					bounds := img.Bounds()
					width := bounds.Dx()
					height := bounds.Dy()

					// Call the callback to handle frame processing
					if vp8Processor.frameCallback != nil {
						vp8Processor.frameCallback(img, width, height, false, 0, vp8Processor.trackID)
					}

					// Release the frame
					if release != nil {
						release()
					}

					vp8Processor.log.Debugf("FrameProcessor: processed frame %d for %s (%dx%d)",
						frameCount+1, vp8Processor.trackID, width, height)
				} else {
					// No frame available, small delay
					time.Sleep(1 * time.Millisecond)
				}
			}
		}
	}()

	return vp8Processor, nil
}

// Decode processes a VP8 frame by feeding it to the decoder.
func (vp *VP8FrameProcessor) Decode(frameData []byte) {
	if vp.feeder != nil && len(frameData) > 0 {
		// Make a copy since the data might be reused by caller
		frameCopy := make([]byte, len(frameData))
		copy(frameCopy, frameData)
		vp.feeder.feedFrame(frameCopy)
	}
}

// SetFirstKeyFrame marks that we got the first keyframe.
func (vp *VP8FrameProcessor) SetFirstKeyFrame() {
	vp.mu.Lock()
	defer vp.mu.Unlock()
	vp.gotFirstKeyframe = true
}

// HasFirstKeyFrame returns whether we got the first keyframe.
func (vp *VP8FrameProcessor) HasFirstKeyFrame() bool {
	vp.mu.RLock()
	defer vp.mu.RUnlock()

	return vp.gotFirstKeyframe
}

// Close stops the frame processor and cleans up resources.
func (vp *VP8FrameProcessor) Close() error {
	// Close the feeder first to signal EOF
	if vp.feeder != nil {
		vp.feeder.close()
	}

	// Give decoder time to process remaining frames
	time.Sleep(100 * time.Millisecond)

	// Stop the goroutine and wait for it to finish
	close(vp.done)
	<-vp.finished // Wait for goroutine to finish

	// Close the decoder
	if vp.decoder != nil {
		if err := vp.decoder.Close(); err != nil {
			return fmt.Errorf("%w: %w", ErrDecoderCloseFailed, err)
		}
	}

	vp.log.Infof("Closed frame processor for %s with %d frames", vp.trackID, vp.frameCounter)

	return nil
}

// GetFrameCount returns the number of processed frames.
func (vp *VP8FrameProcessor) GetFrameCount() int {
	vp.mu.RLock()
	defer vp.mu.RUnlock()

	return vp.frameCounter
}
