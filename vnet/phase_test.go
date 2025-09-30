// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package main

import (
	"os"
	"testing"
	"time"

	"github.com/pion/transport/v3/vnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetPathCharacteristics(t *testing.T) {
	// Create a test phase file in the current directory
	testFile := "test_phases.json"

	testJSON := `[
		{
			"durationSeconds": 30,
			"capacityRatio": 1.5,
			"maxBurstKbps": 200,
			"dataLossRate": 5,
			"ackLossRate": 2,
			"dataDelayMs": 100,
			"ackDelayMs": 50
		}
	]`

	err := os.WriteFile(testFile, []byte(testJSON), 0o600)
	require.NoError(t, err)
	defer func() {
		_ = os.Remove(testFile) // Best effort cleanup
	}()

	// Test successful parsing
	pathChars := GetPathCharacteristics(testFile, 2*vnet.MBit)

	assert.Equal(t, 2*vnet.MBit, pathChars.referenceCapacity)
	assert.Len(t, pathChars.phases, 1)

	if len(pathChars.phases) > 0 {
		p := pathChars.phases[0]
		assert.Equal(t, 30*time.Second, p.duration)
		assert.Equal(t, 1.5, p.capacityRatio)
		assert.Equal(t, 200*vnet.KBit, p.maxBurst)
		assert.Equal(t, 5, p.dataLossRate)
		assert.Equal(t, 2, p.ackLossRate)
		assert.Equal(t, 100*time.Millisecond, p.dataDelay)
		assert.Equal(t, 50*time.Millisecond, p.ackDelay)
	}
}

func TestGetPathCharacteristics_InvalidFile(t *testing.T) {
	// Test with non-existent file
	pathChars := GetPathCharacteristics("nonexistent.json", 1*vnet.MBit)

	// Should return empty phases as fallback
	assert.Equal(t, 1*vnet.MBit, pathChars.referenceCapacity)
	assert.Len(t, pathChars.phases, 0)
}

func TestParsePhases_ValidJSON(t *testing.T) {
	testFile := "test_valid.json"

	testJSON := `[
		{
			"durationSeconds": 10,
			"capacityRatio": 2.0,
			"maxBurstKbps": 160
		},
		{
			"durationSeconds": 20,
			"capacityRatio": 0.5,
			"maxBurstKbps": 80,
			"dataLossRate": 10,
			"ackLossRate": 5,
			"dataDelayMs": 200,
			"ackDelayMs": 100
		}
	]`

	err := os.WriteFile(testFile, []byte(testJSON), 0o600)
	require.NoError(t, err)
	defer func() {
		_ = os.Remove(testFile) // Best effort cleanup
	}()

	phases, err := parsePhases(testFile)
	require.NoError(t, err)
	assert.Len(t, phases, 2)

	// Test first phase (minimal fields)
	assert.Equal(t, 10*time.Second, phases[0].duration)
	assert.Equal(t, 2.0, phases[0].capacityRatio)
	assert.Equal(t, 160*vnet.KBit, phases[0].maxBurst)
	assert.Equal(t, 0, phases[0].dataLossRate)
	assert.Equal(t, 0, phases[0].ackLossRate)
	assert.Equal(t, time.Duration(0), phases[0].dataDelay)
	assert.Equal(t, time.Duration(0), phases[0].ackDelay)

	// Test second phase (all fields)
	assert.Equal(t, 20*time.Second, phases[1].duration)
	assert.Equal(t, 0.5, phases[1].capacityRatio)
	assert.Equal(t, 80*vnet.KBit, phases[1].maxBurst)
	assert.Equal(t, 10, phases[1].dataLossRate)
	assert.Equal(t, 5, phases[1].ackLossRate)
	assert.Equal(t, 200*time.Millisecond, phases[1].dataDelay)
	assert.Equal(t, 100*time.Millisecond, phases[1].ackDelay)
}

func TestParsePhases_InvalidPath(t *testing.T) {
	testCases := []struct {
		name string
		path string
	}{
		{"directory traversal", "../malicious.json"},
		{"complex traversal", "phases/../../../etc/passwd"},
		{"invalid subdirectory", "invalid/path.json"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parsePhases(tc.path)
			assert.ErrorIs(t, err, errInvalidFilePath)
		})
	}
}

func TestParsePhases_InvalidJSON(t *testing.T) {
	testFile := "test_invalid.json"

	// Invalid JSON
	err := os.WriteFile(testFile, []byte("invalid json"), 0o600)
	require.NoError(t, err)
	defer func() {
		_ = os.Remove(testFile) // Best effort cleanup
	}()

	_, err = parsePhases(testFile)
	assert.Error(t, err)
}

func TestPhaseJSON_ToPhase_Valid(t *testing.T) {
	pj := phaseJSON{
		DurationSeconds: 15,
		CapacityRatio:   1.2,
		MaxBurstKbps:    120,
		DataLossRate:    3,
		AckLossRate:     1,
		DataDelayMs:     75,
		AckDelayMs:      25,
	}

	p, err := pj.toPhase()
	require.NoError(t, err)

	assert.Equal(t, 15*time.Second, p.duration)
	assert.Equal(t, 1.2, p.capacityRatio)
	assert.Equal(t, 120*vnet.KBit, p.maxBurst)
	assert.Equal(t, 3, p.dataLossRate)
	assert.Equal(t, 1, p.ackLossRate)
	assert.Equal(t, 75*time.Millisecond, p.dataDelay)
	assert.Equal(t, 25*time.Millisecond, p.ackDelay)
}

func TestPhaseJSON_ToPhase_ValidationErrors(t *testing.T) {
	testCases := []struct {
		name        string
		phase       phaseJSON
		expectedErr error
	}{
		{
			name: "invalid duration",
			phase: phaseJSON{
				DurationSeconds: 0,
				CapacityRatio:   1.0,
				MaxBurstKbps:    160,
			},
			expectedErr: errInvalidDuration,
		},
		{
			name: "invalid capacity ratio - too low",
			phase: phaseJSON{
				DurationSeconds: 10,
				CapacityRatio:   0,
				MaxBurstKbps:    160,
			},
			expectedErr: errInvalidCapacity,
		},
		{
			name: "invalid capacity ratio - too high",
			phase: phaseJSON{
				DurationSeconds: 10,
				CapacityRatio:   101,
				MaxBurstKbps:    160,
			},
			expectedErr: errInvalidCapacity,
		},
		{
			name: "invalid max burst",
			phase: phaseJSON{
				DurationSeconds: 10,
				CapacityRatio:   1.0,
				MaxBurstKbps:    0,
			},
			expectedErr: errInvalidMaxBurst,
		},
		{
			name: "invalid data loss rate - negative",
			phase: phaseJSON{
				DurationSeconds: 10,
				CapacityRatio:   1.0,
				MaxBurstKbps:    160,
				DataLossRate:    -1,
			},
			expectedErr: errInvalidDataLoss,
		},
		{
			name: "invalid data loss rate - too high",
			phase: phaseJSON{
				DurationSeconds: 10,
				CapacityRatio:   1.0,
				MaxBurstKbps:    160,
				DataLossRate:    101,
			},
			expectedErr: errInvalidDataLoss,
		},
		{
			name: "invalid ack loss rate - negative",
			phase: phaseJSON{
				DurationSeconds: 10,
				CapacityRatio:   1.0,
				MaxBurstKbps:    160,
				AckLossRate:     -1,
			},
			expectedErr: errInvalidAckLoss,
		},
		{
			name: "invalid ack loss rate - too high",
			phase: phaseJSON{
				DurationSeconds: 10,
				CapacityRatio:   1.0,
				MaxBurstKbps:    160,
				AckLossRate:     101,
			},
			expectedErr: errInvalidAckLoss,
		},
		{
			name: "invalid data delay - negative",
			phase: phaseJSON{
				DurationSeconds: 10,
				CapacityRatio:   1.0,
				MaxBurstKbps:    160,
				DataDelayMs:     -1,
			},
			expectedErr: errInvalidDataDelay,
		},
		{
			name: "invalid ack delay - negative",
			phase: phaseJSON{
				DurationSeconds: 10,
				CapacityRatio:   1.0,
				MaxBurstKbps:    160,
				AckDelayMs:      -1,
			},
			expectedErr: errInvalidAckDelay,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.phase.toPhase()
			assert.ErrorIs(t, err, tc.expectedErr)
		})
	}
}

func TestValidateLossRate(t *testing.T) {
	testCases := []struct {
		name        string
		rate        int
		expectError bool
	}{
		{"valid rate 0", 0, false},
		{"valid rate 50", 50, false},
		{"valid rate 100", 100, false},
		{"invalid rate negative", -1, true},
		{"invalid rate too high", 101, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateLossRate(tc.rate)
			if tc.expectError {
				assert.ErrorIs(t, err, errInvalidLossRate)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPhaseStruct_Comparability(t *testing.T) {
	// Test that phase struct is comparable
	p1 := phase{
		duration:      10 * time.Second,
		capacityRatio: 1.0,
		maxBurst:      160 * vnet.KBit,
		dataLossRate:  5,
		ackLossRate:   2,
		dataDelay:     100 * time.Millisecond,
		ackDelay:      50 * time.Millisecond,
	}

	p2 := phase{
		duration:      10 * time.Second,
		capacityRatio: 1.0,
		maxBurst:      160 * vnet.KBit,
		dataLossRate:  5,
		ackLossRate:   2,
		dataDelay:     100 * time.Millisecond,
		ackDelay:      50 * time.Millisecond,
	}

	p3 := phase{
		duration:      20 * time.Second,
		capacityRatio: 2.0,
		maxBurst:      320 * vnet.KBit,
	}

	// Test equality
	assert.True(t, p1 == p2)
	assert.False(t, p1 == p3)
}
