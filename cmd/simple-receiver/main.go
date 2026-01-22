//go:build !js
// +build !js

// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package main provides a simple HTTP receiver for testing the realtime encoder integration example.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/pion/bwe-test/receiver"
	"github.com/pion/logging"
	"github.com/pion/webrtc/v4"
)

func main() {
	// Set up logging
	loggerFactory := logging.NewDefaultLoggerFactory()
	logger := loggerFactory.NewLogger("simple_receiver")

	logger.Info("Starting simple HTTP receiver on :8081")

	// Create receiver with output path matching the script's expected structure
	outputPath := "vnet/data/TestVnetRunnerDualVideoTracks/VariableAvailableCapacitySingleFlow/received_video/received_0"

	// Ensure the directory exists
	outputDir := filepath.Dir(outputPath + "_dummy") // Get directory part

	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		log.Fatalf("Failed to create output directory %s: %v", outputDir, err)
	}

	logger.Info(fmt.Sprintf("Ensured output directory exists: %s", outputDir))

	recv, err := receiver.NewReceiver(
		receiver.SetLoggerFactory(loggerFactory),
		receiver.SaveVideo(outputPath),
	)
	if err != nil {
		log.Fatalf("Failed to create receiver: %v", err)
	}

	// Set up HTTP handler for SDP exchange
	http.HandleFunc("/sdp", func(writer http.ResponseWriter, request *http.Request) {
		logger.Info("Received SDP offer from sender")

		if request.Method != http.MethodPost {
			http.Error(writer, "Method not allowed", http.StatusMethodNotAllowed)

			return
		}

		// Read the offer from the request
		var offer webrtc.SessionDescription
		if err := json.NewDecoder(request.Body).Decode(&offer); err != nil {
			logger.Error(fmt.Sprintf("Failed to decode offer: %v", err))
			http.Error(writer, "Bad request", http.StatusBadRequest)

			return
		}

		// Set up peer connection
		if err := recv.SetupPeerConnection(); err != nil {
			logger.Error(fmt.Sprintf("Failed to setup peer connection: %v", err))
			http.Error(writer, "Internal server error", http.StatusInternalServerError)

			return
		}

		// Process the offer and create answer
		answer, err := recv.AcceptOffer(&offer)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to accept offer: %v", err))
			http.Error(writer, "Internal server error", http.StatusInternalServerError)

			return
		}

		// Send the answer back
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(answer); err != nil {
			logger.Error(fmt.Sprintf("Failed to encode answer: %v", err))

			return
		}

		logger.Info("Sent WebRTC answer to sender")
		logger.Info("Receiver is now ready to receive media")
	})

	// Start HTTP server
	server := &http.Server{
		Addr:              ":8081",
		Handler:           nil,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("HTTP receiver listening on :8081/sdp")
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Handle shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	<-sigs
	logger.Info("Shutting down receiver")

	if err := server.Close(); err != nil {
		logger.Error(fmt.Sprintf("Error closing server: %v", err))
	}
}
