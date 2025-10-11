//go:build !js
// +build !js

// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package main provides an example of integrating real-time frame pushing with BWE testing using RTCSender.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pion/bwe-test/sender"
	"github.com/pion/logging"
	"github.com/pion/mediadevices/pkg/codec/vpx"
	"github.com/pion/webrtc/v4"
)

/*
This example demonstrates how to integrate real-time frame pushing with pion/bwe-test using RTCSender.

Usage:
1. Start the receiver:
   $ go run ./cmd/simple-receiver/

2. Run this example:
   $ go run ./examples/realtime_encoder_integration.go

This shows the same API that would be called from C++ code via CGO for real-time video streaming.
*/

func main() {
	// Set up logging
	loggerFactory := logging.NewDefaultLoggerFactory()
	logger := loggerFactory.NewLogger("real_time_integration")

	// Create a context that can be canceled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Define encoder parameters (similar to your RTC module)
	width := 640
	height := 480
	initialBitrate := 500000 // 500 kbps

	// ================== BWE TEST INTEGRATION ==================

	// Create RTCSender with BWE capabilities
	rtcSender, err := sender.NewRTCSender(
		sender.DefaultInterceptors(),
		sender.GCC(initialBitrate), // Initial bitrate 500 kbps
		sender.SetLoggerFactory(loggerFactory),
	)
	if err != nil {
		logger.Error(err.Error())

		return
	}

	// Create VP8 encoder parameters
	vpxParams, err := vpx.NewVP8Params()
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to create VP8 params: %v", err))

		return
	}
	vpxParams.BitRate = initialBitrate

	// Add a video track for real-time encoding
	trackInfo := sender.VideoTrackInfo{
		TrackID:        "realtime-video",
		Width:          width,
		Height:         height,
		EncoderBuilder: &vpxParams,
	}

	if err := rtcSender.AddVideoTrack(trackInfo); err != nil {
		logger.Error(fmt.Sprintf("Failed to add video track: %v", err))

		return
	}

	// Set up the peer connection
	if err := rtcSender.SetupPeerConnection(); err != nil {
		logger.Error(err.Error())

		return
	}

	// ================== DIRECT FRAME FEEDING ==================
	startFrameFeeding(ctx, rtcSender, width, height, logger)

	// ================== SIGNALING ==================
	startSignaling(rtcSender, cancel, logger)

	// ================== HANDLING SHUTDOWN ==================
	handleShutdown(cancel)

	// Start the sender
	logger.Info("Starting BWE test...")
	if err := rtcSender.Start(ctx); err != nil {
		logger.Error(err.Error())
	}

	logger.Info("BWE test completed")
}

// startFrameFeeding starts a goroutine to generate and feed test frames.
func startFrameFeeding(
	ctx context.Context,
	rtcSender *sender.RTCSender,
	width, height int,
	logger logging.LeveledLogger,
) {
	go func() {
		// Feed frames at 30fps
		ticker := time.NewTicker(33 * time.Millisecond)
		defer ticker.Stop()

		frameCounter := 0
		for {
			select {
			case <-ticker.C:
				// Create a slightly different frame each time
				testImg := createAnimatedTestImage(width, height, frameCounter)

				// This is the same API that would be called from your C++ code via CGO
				if err := rtcSender.SendFrame("realtime-video", testImg); err != nil {
					logger.Error(fmt.Sprintf("Error pushing frame: %v", err))
				}
				frameCounter++
			case <-ctx.Done():
				return
			}
		}
	}()
}

// startSignaling starts the signaling process.
func startSignaling(rtcSender *sender.RTCSender, cancel context.CancelFunc, logger logging.LeveledLogger) {
	go func() {
		// Create WebRTC offer
		offer, err := rtcSender.CreateOffer()
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to create offer: %v", err))
			cancel()

			return
		}

		// Send offer to receiver via HTTP
		offerJSON, err := json.Marshal(offer)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to marshal offer: %v", err))
			cancel()

			return
		}

		// Create request with context
		url := "http://127.0.0.1:8081/sdp"
		req, err := http.NewRequestWithContext(context.Background(), "POST", url, bytes.NewBuffer(offerJSON))
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to create request: %v", err))
			cancel()

			return
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to send offer: %v", err))
			cancel()

			return
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				logger.Error(fmt.Sprintf("Failed to close response body: %v", err))
			}
		}()

		// Read answer from receiver
		var answer webrtc.SessionDescription
		if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
			logger.Error(fmt.Sprintf("Failed to decode answer: %v", err))
			cancel()

			return
		}

		// Set remote description
		if err := rtcSender.AcceptAnswer(&answer); err != nil {
			logger.Error(fmt.Sprintf("Failed to accept answer: %v", err))
			cancel()

			return
		}

		logger.Info("Successfully completed WebRTC signaling")
	}()
}

// handleShutdown handles graceful shutdown signals.
func handleShutdown(cancel context.CancelFunc) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		cancel()
	}()
}

// createAnimatedTestImage creates a test pattern that changes over time.
func createAnimatedTestImage(width, height, frame int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Create an animated pattern that changes with frame number
	offset := frame % 255

	for yPos := 0; yPos < height; yPos++ {
		for xPos := 0; xPos < width; xPos++ {
			// Safe conversion to avoid overflow warnings
			rVal := ((xPos + offset) * 255) / width
			gVal := ((yPos + offset) * 255) / height
			bVal := (128 + offset) % 255

			// Ensure values are within uint8 range
			if rVal > 255 {
				rVal = 255
			}
			if gVal > 255 {
				gVal = 255
			}
			if bVal > 255 {
				bVal = 255
			}

			//nolint:gosec // Values are bounds-checked above
			r := uint8(rVal)
			//nolint:gosec // Values are bounds-checked above
			g := uint8(gVal)
			//nolint:gosec // Values are bounds-checked above
			b := uint8(bVal)
			a := uint8(255)

			img.Set(xPos, yPos, color.RGBA{R: r, G: g, B: b, A: a})
		}
	}

	return img
}
