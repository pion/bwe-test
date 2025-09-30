// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

// Package main implements virtual network functionality for bandwidth estimation tests.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v3/vnet"
)

// senderMode defines the type of sender to use in the test.
type senderMode int

const (
	simulcastSenderMode senderMode = iota
	abrSenderMode
	videoFileEncoderMode // New mode for video file encoder
)

// flowMode defines whether to use a single flow or multiple flows in the test.
type flowMode int

const (
	singleFlowMode flowMode = iota
	multipleFlowsMode
)

func main() {
	logLevel := flag.String("log", "info", "Log level")
	videoFiles := flag.String("videos", "", "Comma-separated list of video file paths (e.g. 'video1.mp4,video2.mp4')")
	flag.Parse()

	loggerFactory, err := getLoggerFactory(*logLevel)
	if err != nil {
		log.Fatalf("get logger factory: %v", err)
	}

	testCases := []struct {
		name              string
		senderMode        senderMode
		flowMode          flowMode
		videoFile         string
		phaseFile         string
		referenceCapacity int
		trackCount        int
	}{
		// {
		// 	name:       "TestVnetRunnerABR/VariableAvailableCapacitySingleFlow",
		// 	senderMode: abrSenderMode,
		// 	flowMode:   singleFlowMode,
		// },
		// {
		// 	name:       "TestVnetRunnerABR/VariableAvailableCapacityMultipleFlows",
		// 	senderMode: abrSenderMode,
		// 	flowMode:   multipleFlowsMode,
		// },
		// Single video track test using hardcoded video files
		// {
		// 	name:              "TestVnetRunnerSingleVideoTrack/VariableAvailableCapacitySingleFlow",
		// 	senderMode:        videoFileEncoderMode,
		// 	flowMode:          singleFlowMode,
		// 	videoFile:         "", // Not used since we hardcode the files in flow.go
		// 	phaseFile:         "phases/single_flow.json",
		// 	referenceCapacity: 1 * vnet.MBit,
		// 	trackCount:        1,
		// },
		// Dual video track test using configurable video files
		{
			name:              "TestVnetRunnerDualVideoTracks/VariableAvailableCapacitySingleFlow",
			senderMode:        videoFileEncoderMode,
			flowMode:          singleFlowMode,
			videoFile:         *videoFiles, // Use command line parameter (comma-separated list)
			phaseFile:         "phases/single_flow.json",
			referenceCapacity: 2 * vnet.MBit,
			trackCount:        2,
		},
		// {
		// 	name:       "TestVnetRunnerSimulcast/VariableAvailableCapacitySingleFlow",
		// 	senderMode: simulcastSenderMode,
		// 	flowMode:   singleFlowMode,
		// },
		// {
		// 	name:       "TestVnetRunnerSimulcast/VariableAvailableCapacityMultipleFlows",
		// 	senderMode: simulcastSenderMode,
		// 	flowMode:   multipleFlowsMode,
		// },
	}

	logger := loggerFactory.NewLogger("bwe_test_runner")
	for _, t := range testCases {
		runner := Runner{
			loggerFactory: loggerFactory,
			logger:        logger,
			name:          t.name,
			senderMode:    t.senderMode,
			flowMode:      t.flowMode,
			videoFile:     t.videoFile,
			trackCount:    t.trackCount,
		}

		err := runner.Run()
		if err != nil {
			logger.Errorf("runner: %v", err)
		}
	}
}

var errUnknownLogLevel = errors.New("unknown log level")

func getLoggerFactory(logLevel string) (*logging.DefaultLoggerFactory, error) {
	logLevels := map[string]logging.LogLevel{
		"disable": logging.LogLevelDisabled,
		"error":   logging.LogLevelError,
		"warn":    logging.LogLevelWarn,
		"info":    logging.LogLevelInfo,
		"debug":   logging.LogLevelDebug,
		"trace":   logging.LogLevelTrace,
	}

	level, ok := logLevels[strings.ToLower(logLevel)]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errUnknownLogLevel, logLevel)
	}

	loggerFactory := &logging.DefaultLoggerFactory{
		Writer:          os.Stdout,
		DefaultLogLevel: level,
		ScopeLevels:     make(map[string]logging.LogLevel),
	}

	return loggerFactory, nil
}

// Runner manages the execution of bandwidth estimation tests.
type Runner struct {
	loggerFactory *logging.DefaultLoggerFactory
	logger        logging.LeveledLogger
	name          string
	senderMode    senderMode
	flowMode      flowMode
	videoFile     string
	trackCount    int
	// Note: pathCharacteristics removed to maintain struct comparability
	// Phase configuration is loaded dynamically via GetPathCharacteristics
}

var errUnknownFlowMode = errors.New("unknown flow mode")

// Run executes the test based on the configured flow mode.
func (r *Runner) Run() error {
	switch r.flowMode {
	case singleFlowMode:
		err := r.runVariableAvailableCapacitySingleFlow()
		if err != nil {
			return fmt.Errorf("run variable available capacity single flow: %w", err)
		}
	case multipleFlowsMode:
		err := r.runVariableAvailableCapacityMultipleFlows()
		if err != nil {
			return fmt.Errorf("run variable available capacity multiple flows: %w", err)
		}
	default:
		return fmt.Errorf("%w: %v", errUnknownFlowMode, r.flowMode)
	}

	return nil
}

func (r *Runner) runVariableAvailableCapacitySingleFlow() error {
	nm, err := NewManager()
	if err != nil {
		return fmt.Errorf("new manager: %w", err)
	}

	dataDir := fmt.Sprintf("data/%v", r.name)
	err = os.MkdirAll(dataDir, 0o750)
	if err != nil {
		return fmt.Errorf("mkdir data: %w", err)
	}

	flow, err := NewSimpleFlow(r.loggerFactory, nm, 0, r.senderMode, dataDir)
	if err != nil {
		return fmt.Errorf("setup simple flow: %w", err)
	}

	defer func(flow Flow) {
		err = flow.Close()
		if err != nil {
			r.logger.Errorf("flow close: %v", err)
		}
	}(flow)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		err = flow.sender.sender.Start(ctx)
		if err != nil {
			r.logger.Errorf("sender start: %v", err)
		}
	}()

	// Allow some time for connection setup before starting the network simulation
	time.Sleep(2 * time.Second)

	// Load phase characteristics for this test
	pathChars := GetPathCharacteristics("phases/single_flow.json", 1*vnet.MBit)
	r.runNetworkSimulation(nm, pathChars)

	// Allow some time for any buffered frames to be processed
	r.logger.Infof("Bandwidth test complete, allowing time for final frame processing...")
	time.Sleep(2 * time.Second)

	return nil
}

func (r *Runner) runVariableAvailableCapacityMultipleFlows() error {
	nm, err := NewManager()
	if err != nil {
		return fmt.Errorf("new manager: %w", err)
	}

	dataDir := fmt.Sprintf("data/%v", r.name)
	err = os.MkdirAll(dataDir, 0o750)
	if err != nil {
		return fmt.Errorf("mkdir data: %w", err)
	}

	for i := 0; i < 2; i++ {
		flow, err := NewSimpleFlow(r.loggerFactory, nm, i, r.senderMode, dataDir)
		defer func(flow Flow) {
			err = flow.Close()
			if err != nil {
				r.logger.Errorf("flow close: %v", err)
			}
		}(flow)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			err = flow.sender.sender.Start(ctx)
			if err != nil {
				r.logger.Errorf("sender start: %v", err)
			}
		}()
	}

	// Load phase characteristics for this test
	pathChars := GetPathCharacteristics("phases/single_flow.json", 1*vnet.MBit)
	r.runNetworkSimulation(nm, pathChars)

	return nil
}

func (r *Runner) runNetworkSimulation(nm *NetworkManager, pathChars pathCharacteristics) {
	for _, phase := range pathChars.phases {
		r.logger.Infof("enter next phase: %v", phase)
		nm.SetCapacity(
			int(float64(pathChars.referenceCapacity)*phase.capacityRatio),
			phase.maxBurst,
		)
		nm.SetDataDelay(phase.dataDelay)
		nm.SetAckDelay(phase.ackDelay)
		nm.SetDataLossRate(phase.dataLossRate)
		nm.SetAckLossRate(phase.ackLossRate)
		time.Sleep(phase.duration)
	}
}

// RunnerOption is a function that configures a Runner.
type RunnerOption func(*RunnerSettings)

// RunnerSettings holds configuration for Runner.
type RunnerSettings struct {
	phaseFile         string
	referenceCapacity int
	videoFile         string
}

// WithPhaseFile sets the phase configuration file.
func WithPhaseFile(phaseFile string) RunnerOption {
	return func(settings *RunnerSettings) {
		settings.phaseFile = phaseFile
	}
}

// WithReferenceCapacity sets the reference capacity.
func WithReferenceCapacity(capacity int) RunnerOption {
	return func(settings *RunnerSettings) {
		settings.referenceCapacity = capacity
	}
}

// WithVideoFile sets the video file.
func WithVideoFile(videoFile string) RunnerOption {
	return func(settings *RunnerSettings) {
		settings.videoFile = videoFile
	}
}

// NewRunnerWithOptions creates a new Runner with options.
func NewRunnerWithOptions(
	loggerFactory *logging.DefaultLoggerFactory,
	logger logging.LeveledLogger,
	name string,
	senderMode senderMode,
	flowMode flowMode,
	options ...RunnerOption,
) *Runner {
	// Apply default settings
	settings := RunnerSettings{
		phaseFile:         "phases/single_flow.json",
		referenceCapacity: 1 * vnet.MBit,
	}

	// Apply options
	for _, option := range options {
		option(&settings)
	}

	return &Runner{
		loggerFactory: loggerFactory,
		logger:        logger,
		name:          name,
		senderMode:    senderMode,
		flowMode:      flowMode,
		videoFile:     settings.videoFile,
		trackCount:    0, // Default track count
		// pathCharacteristics removed to maintain comparability
	}
}
