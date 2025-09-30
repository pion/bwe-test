// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package main

import (
	"testing"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v3/vnet"
	"github.com/stretchr/testify/assert"
)

func TestRunnerOptions(t *testing.T) {
	testCases := []struct {
		name     string
		options  []RunnerOption
		expected RunnerSettings
	}{
		{
			name:    "default settings",
			options: nil,
			expected: RunnerSettings{
				phaseFile:         "phases/single_flow.json",
				referenceCapacity: 1 * vnet.MBit,
			},
		},
		{
			name: "with phase file",
			options: []RunnerOption{
				WithPhaseFile("phases/custom.json"),
			},
			expected: RunnerSettings{
				phaseFile:         "phases/custom.json",
				referenceCapacity: 1 * vnet.MBit,
			},
		},
		{
			name: "with reference capacity",
			options: []RunnerOption{
				WithReferenceCapacity(2 * vnet.MBit),
			},
			expected: RunnerSettings{
				phaseFile:         "phases/single_flow.json",
				referenceCapacity: 2 * vnet.MBit,
			},
		},
		{
			name: "with video file",
			options: []RunnerOption{
				WithVideoFile("test_video.mp4"),
			},
			expected: RunnerSettings{
				phaseFile:         "phases/single_flow.json",
				referenceCapacity: 1 * vnet.MBit,
				videoFile:         "test_video.mp4",
			},
		},
		{
			name: "multiple options",
			options: []RunnerOption{
				WithPhaseFile("phases/multiple_flows.json"),
				WithReferenceCapacity(3 * vnet.MBit),
				WithVideoFile("multi_test.mp4"),
			},
			expected: RunnerSettings{
				phaseFile:         "phases/multiple_flows.json",
				referenceCapacity: 3 * vnet.MBit,
				videoFile:         "multi_test.mp4",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test that options are applied correctly
			settings := RunnerSettings{
				phaseFile:         "phases/single_flow.json",
				referenceCapacity: 1 * vnet.MBit,
			}

			for _, option := range tc.options {
				option(&settings)
			}

			assert.Equal(t, tc.expected, settings)
		})
	}
}

func TestNewRunnerWithOptions(t *testing.T) {
	loggerFactory := logging.NewDefaultLoggerFactory()
	logger := loggerFactory.NewLogger("test")

	runner := NewRunnerWithOptions(
		loggerFactory,
		logger,
		"TestRunner",
		abrSenderMode,
		singleFlowMode,
		WithPhaseFile("phases/single_flow.json"),
		WithReferenceCapacity(2*vnet.MBit),
		WithVideoFile("test.mp4"),
	)

	assert.NotNil(t, runner)
	assert.Equal(t, loggerFactory, runner.loggerFactory)
	assert.Equal(t, logger, runner.logger)
	assert.Equal(t, "TestRunner", runner.name)
	assert.Equal(t, abrSenderMode, runner.senderMode)
	assert.Equal(t, singleFlowMode, runner.flowMode)
	assert.Equal(t, "test.mp4", runner.videoFile)
	assert.Equal(t, 0, runner.trackCount)
}

func TestRunnerSettings_Comparability(t *testing.T) {
	// Test that RunnerSettings is comparable
	s1 := RunnerSettings{
		phaseFile:         "phases/test.json",
		referenceCapacity: 2 * vnet.MBit,
		videoFile:         "video.mp4",
	}

	s2 := RunnerSettings{
		phaseFile:         "phases/test.json",
		referenceCapacity: 2 * vnet.MBit,
		videoFile:         "video.mp4",
	}

	s3 := RunnerSettings{
		phaseFile:         "phases/other.json",
		referenceCapacity: 1 * vnet.MBit,
		videoFile:         "other.mp4",
	}

	// Test equality
	assert.True(t, s1 == s2)
	assert.False(t, s1 == s3)
}

func TestRunner_Comparability(t *testing.T) {
	// Test that Runner struct is comparable (critical for API compatibility)
	loggerFactory := logging.NewDefaultLoggerFactory()
	logger := loggerFactory.NewLogger("test")

	r1 := Runner{
		loggerFactory: loggerFactory,
		logger:        logger,
		name:          "test",
		senderMode:    abrSenderMode,
		flowMode:      singleFlowMode,
		videoFile:     "test.mp4",
		trackCount:    1,
	}

	r2 := Runner{
		loggerFactory: loggerFactory,
		logger:        logger,
		name:          "test",
		senderMode:    abrSenderMode,
		flowMode:      singleFlowMode,
		videoFile:     "test.mp4",
		trackCount:    1,
	}

	r3 := Runner{
		loggerFactory: loggerFactory,
		logger:        logger,
		name:          "different",
		senderMode:    simulcastSenderMode,
		flowMode:      multipleFlowsMode,
		videoFile:     "other.mp4",
		trackCount:    2,
	}

	// Test equality - this verifies struct comparability
	assert.True(t, r1 == r2)
	assert.False(t, r1 == r3)
}

func TestSenderMode_Values(t *testing.T) {
	// Test that sender modes have expected values
	assert.Equal(t, senderMode(0), simulcastSenderMode)
	assert.Equal(t, senderMode(1), abrSenderMode)
	assert.Equal(t, senderMode(2), videoFileEncoderMode)
}

func TestFlowMode_Values(t *testing.T) {
	// Test that flow modes have expected values
	assert.Equal(t, flowMode(0), singleFlowMode)
	assert.Equal(t, flowMode(1), multipleFlowsMode)
}

func TestPathCharacteristics_Structure(t *testing.T) {
	// Test pathCharacteristics struct
	phases := []phase{
		{
			duration:      10 * time.Second,
			capacityRatio: 1.0,
			maxBurst:      160 * vnet.KBit,
		},
		{
			duration:      20 * time.Second,
			capacityRatio: 2.0,
			maxBurst:      320 * vnet.KBit,
			dataLossRate:  5,
			ackLossRate:   2,
			dataDelay:     100 * time.Millisecond,
			ackDelay:      50 * time.Millisecond,
		},
	}

	pathChars := pathCharacteristics{
		referenceCapacity: 2 * vnet.MBit,
		phases:            phases,
	}

	assert.Equal(t, 2*vnet.MBit, pathChars.referenceCapacity)
	assert.Len(t, pathChars.phases, 2)
	assert.Equal(t, phases[0], pathChars.phases[0])
	assert.Equal(t, phases[1], pathChars.phases[1])
}

func TestConstants(t *testing.T) {
	// Test that constants have expected values
	assert.Equal(t, 1*vnet.MBit, int(initCapacity))
	assert.Equal(t, 80*vnet.KBit, int(initMaxBurst))
}

func TestErrorVariables(t *testing.T) {
	// Test that error variables are properly defined
	assert.NotNil(t, errUnknownFlowMode)
	assert.Contains(t, errUnknownFlowMode.Error(), "unknown flow mode")
}
