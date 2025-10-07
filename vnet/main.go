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
	flag.Parse()

	loggerFactory, err := getLoggerFactory(*logLevel)
	if err != nil {
		log.Fatalf("get logger factory: %v", err)
	}

	testCases := []struct {
		name              string
		senderMode        senderMode
		flowMode          flowMode
		videoFile         []string
		phaseFile         string
		referenceCapacity int
	}{
		{
			name:       "TestVnetRunnerABR/VariableAvailableCapacitySingleFlow",
			senderMode: abrSenderMode,
			flowMode:   singleFlowMode,
		},
		{
			name:       "TestVnetRunnerABR/VariableAvailableCapacityMultipleFlows",
			senderMode: abrSenderMode,
			flowMode:   multipleFlowsMode,
		},
		{
			name:       "TestVnetRunnerSimulcast/VariableAvailableCapacitySingleFlow",
			senderMode: simulcastSenderMode,
			flowMode:   singleFlowMode,
		},
		{
			name:       "TestVnetRunnerSimulcast/VariableAvailableCapacityMultipleFlows",
			senderMode: simulcastSenderMode,
			flowMode:   multipleFlowsMode,
		},
		{
			name:              "TestVnetRunnerDualVideoTracks/VariableAvailableCapacitySingleFlow",
			senderMode:        videoFileEncoderMode,
			flowMode:          singleFlowMode,
			videoFile:         []string{"../sample_videos_0/", "../sample_videos_1/"},
			phaseFile:         "phases/single_flow.json",
			referenceCapacity: 1 * vnet.MBit,
		},
	}

	logger := loggerFactory.NewLogger("bwe_test_runner")
	for _, t := range testCases {
		runner := Runner{
			loggerFactory:       loggerFactory,
			logger:              logger,
			name:                t.name,
			senderMode:          t.senderMode,
			flowMode:            t.flowMode,
			videoFile:           t.videoFile,
			pathCharacteristics: GetPathCharacteristics(t.phaseFile, t.referenceCapacity),
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
	loggerFactory       *logging.DefaultLoggerFactory
	logger              logging.LeveledLogger
	name                string
	senderMode          senderMode
	flowMode            flowMode
	videoFile           []string
	pathCharacteristics pathCharacteristics
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

	if r.senderMode == videoFileEncoderMode {
		return r.runVideoFileSingleFlow(nm, dataDir)
	}

	return r.runStandardSingleFlow(nm, dataDir)
}

func (r *Runner) runVideoFileSingleFlow(nm *NetworkManager, dataDir string) error {
	videoFiles := r.convertToVideoFileInfo()
	flow, err := NewSimpleFlow(r.loggerFactory, nm, 0, r.senderMode, dataDir, videoFiles...)
	if err != nil {
		return fmt.Errorf("setup simple flow: %w", err)
	}

	defer r.closeFlow(flow)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r.startSender(ctx, flow)

	// Allow some time for connection setup before starting the network simulation
	time.Sleep(2 * time.Second)

	r.runNetworkSimulation(nm)

	// Allow some time for any buffered frames to be processed
	r.logger.Infof("Bandwidth test complete, allowing time for final frame processing...")
	time.Sleep(2 * time.Second)

	return nil
}

func (r *Runner) runStandardSingleFlow(nm *NetworkManager, dataDir string) error {
	flow, err := NewSimpleFlow(r.loggerFactory, nm, 0, r.senderMode, dataDir)
	if err != nil {
		return fmt.Errorf("setup simple flow: %w", err)
	}

	defer r.closeFlow(flow)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r.startSender(ctx, flow)

	r.setSingleFlowPathCharacteristics()
	r.runNetworkSimulation(nm)

	return nil
}

func (r *Runner) convertToVideoFileInfo() []VideoFileInfo {
	var videoFiles []VideoFileInfo
	for i, filePath := range r.videoFile {
		videoFiles = append(videoFiles, VideoFileInfo{
			FilePath: filePath,
			TrackID:  fmt.Sprintf("video-track-%d", i+1),
			Width:    640,
			Height:   480,
		})
	}

	return videoFiles
}

func (r *Runner) closeFlow(flow Flow) {
	err := flow.Close()
	if err != nil {
		r.logger.Errorf("flow close: %v", err)
	}
}

func (r *Runner) startSender(ctx context.Context, flow Flow) {
	go func() {
		err := flow.sender.sender.Start(ctx)
		if err != nil {
			r.logger.Errorf("sender start: %v", err)
		}
	}()
}

func (r *Runner) setSingleFlowPathCharacteristics() {
	r.pathCharacteristics = pathCharacteristics{
		referenceCapacity: 1 * vnet.MBit,
		phases: []phase{
			{
				duration:      40 * time.Second,
				capacityRatio: 1.0,
				maxBurst:      160 * vnet.KBit,
			},
			{
				duration:      20 * time.Second,
				capacityRatio: 2.5,
				maxBurst:      160 * vnet.KBit,
			},
			{
				duration:      20 * time.Second,
				capacityRatio: 0.6,
				maxBurst:      160 * vnet.KBit,
			},
			{
				duration:      20 * time.Second,
				capacityRatio: 1.0,
				maxBurst:      160 * vnet.KBit,
			},
		},
	}
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

	if r.senderMode == videoFileEncoderMode {
		return r.runVideoFileMultipleFlows(nm, dataDir)
	}

	return r.runStandardMultipleFlows(nm, dataDir)
}

func (r *Runner) runVideoFileMultipleFlows(nm *NetworkManager, dataDir string) error {
	videoFiles := r.convertToVideoFileInfo()

	for i := range 2 {
		flow, err := NewSimpleFlow(r.loggerFactory, nm, i, r.senderMode, dataDir, videoFiles...)
		if err != nil {
			return fmt.Errorf("setup simple flow %d: %w", i, err)
		}

		defer r.closeFlow(flow)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		r.startSender(ctx, flow)
	}

	r.runNetworkSimulation(nm)

	return nil
}

func (r *Runner) runStandardMultipleFlows(nm *NetworkManager, dataDir string) error {
	for i := range 2 {
		flow, err := NewSimpleFlow(r.loggerFactory, nm, i, r.senderMode, dataDir)
		if err != nil {
			return fmt.Errorf("setup simple flow %d: %w", i, err)
		}

		defer r.closeFlow(flow)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		r.startSender(ctx, flow)
	}

	r.setMultipleFlowsPathCharacteristics()
	r.runNetworkSimulation(nm)

	return nil
}

func (r *Runner) setMultipleFlowsPathCharacteristics() {
	r.pathCharacteristics = pathCharacteristics{
		referenceCapacity: 1 * vnet.MBit,
		phases: []phase{
			{
				duration:      25 * time.Second,
				capacityRatio: 2.0,
				maxBurst:      160 * vnet.KBit,
			},
			{
				duration:      25 * time.Second,
				capacityRatio: 1.0,
				maxBurst:      160 * vnet.KBit,
			},
			{
				duration:      25 * time.Second,
				capacityRatio: 1.75,
				maxBurst:      160 * vnet.KBit,
			},
			{
				duration:      25 * time.Second,
				capacityRatio: 0.5,
				maxBurst:      160 * vnet.KBit,
			},
			{
				duration:      25 * time.Second,
				capacityRatio: 1.0,
				maxBurst:      160 * vnet.KBit,
			},
		},
	}
}

func (r *Runner) runNetworkSimulation(nm *NetworkManager) {
	for _, phase := range r.pathCharacteristics.phases {
		r.logger.Infof("enter next phase: %v", phase)
		nm.SetCapacity(
			int(float64(r.pathCharacteristics.referenceCapacity)*phase.capacityRatio),
			phase.maxBurst,
		)
		nm.SetDataDelay(phase.dataDelay)
		nm.SetAckDelay(phase.ackDelay)
		nm.SetDataLossRate(phase.dataLossRate)
		nm.SetAckLossRate(phase.ackLossRate)
		time.Sleep(phase.duration)
	}
}
