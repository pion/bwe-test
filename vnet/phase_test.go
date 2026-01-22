// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pion/transport/v3/vnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetPathCharacteristics_EmptyPhaseFile(t *testing.T) {
	// Test with empty phase file
	result := GetPathCharacteristics("", 1000000)
	assert.Equal(t, 0, result.referenceCapacity, "Reference capacity should be 0 for empty phase file")
	assert.Empty(t, result.phases, "Phases should be empty for empty phase file")
}

func TestGetPathCharacteristics_NonExistentFile(t *testing.T) {
	// Test with non-existent file
	result := GetPathCharacteristics("non_existent_file.json", 1000000)
	assert.Equal(t, 0, result.referenceCapacity, "Reference capacity should be 0 for non-existent file")
	assert.Empty(t, result.phases, "Phases should be empty for non-existent file")
}

func TestGetPathCharacteristics_ValidFile(t *testing.T) {
	// Create a temporary JSON file for testing
	tempDir := t.TempDir()
	phaseFile := filepath.Join(tempDir, "test_phases.json")

	// Create valid phase data
	phases := []phaseJSON{
		{
			DurationSeconds: 10,
			CapacityRatio:   50.0,
			MaxBurstKbps:    100,
			DataLossRate:    5,
			AckLossRate:     2,
			DataDelayMs:     50,
			AckDelayMs:      25,
		},
		{
			DurationSeconds: 20,
			CapacityRatio:   75.0,
			MaxBurstKbps:    150,
			DataLossRate:    3,
			AckLossRate:     1,
			DataDelayMs:     30,
			AckDelayMs:      15,
		},
	}

	// Write JSON file
	jsonData, err := json.Marshal(phases)
	require.NoError(t, err, "Should marshal phases to JSON")

	err = os.WriteFile(phaseFile, jsonData, 0o600)
	require.NoError(t, err, "Should write phase file")

	// Test GetPathCharacteristics
	referenceCapacity := 2000000
	result := GetPathCharacteristics(phaseFile, referenceCapacity)

	assert.Equal(t, referenceCapacity, result.referenceCapacity, "Reference capacity should match input")
	assert.Len(t, result.phases, 2, "Should have 2 phases")

	// Verify first phase
	phase1 := result.phases[0]
	assert.Equal(t, 10*time.Second, phase1.duration, "Phase 1 duration should be 10 seconds")
	assert.Equal(t, 50.0, phase1.capacityRatio, "Phase 1 capacity ratio should be 50.0")
	assert.Equal(t, 100*vnet.KBit, phase1.maxBurst, "Phase 1 max burst should be 100 KBit")
	assert.Equal(t, 5, phase1.dataLossRate, "Phase 1 data loss rate should be 5")
	assert.Equal(t, 2, phase1.ackLossRate, "Phase 1 ack loss rate should be 2")
	assert.Equal(t, 50*time.Millisecond, phase1.dataDelay, "Phase 1 data delay should be 50ms")
	assert.Equal(t, 25*time.Millisecond, phase1.ackDelay, "Phase 1 ack delay should be 25ms")

	// Verify second phase
	phase2 := result.phases[1]
	assert.Equal(t, 20*time.Second, phase2.duration, "Phase 2 duration should be 20 seconds")
	assert.Equal(t, 75.0, phase2.capacityRatio, "Phase 2 capacity ratio should be 75.0")
	assert.Equal(t, 150*vnet.KBit, phase2.maxBurst, "Phase 2 max burst should be 150 KBit")
	assert.Equal(t, 3, phase2.dataLossRate, "Phase 2 data loss rate should be 3")
	assert.Equal(t, 1, phase2.ackLossRate, "Phase 2 ack loss rate should be 1")
	assert.Equal(t, 30*time.Millisecond, phase2.dataDelay, "Phase 2 data delay should be 30ms")
	assert.Equal(t, 15*time.Millisecond, phase2.ackDelay, "Phase 2 ack delay should be 15ms")
}

func TestParsePhases_InvalidFilePath(t *testing.T) {
	// Test with invalid file path (directory traversal attempt)
	phases, err := parsePhases("../../../etc/passwd")
	assert.Error(t, err, "Should error on invalid file path")
	assert.Nil(t, phases, "Should return nil phases on error")
	assert.Contains(t, err.Error(), "invalid phase file path", "Error should mention invalid path")
}

func TestParsePhases_NonExistentFile(t *testing.T) {
	// Test with non-existent file
	phases, err := parsePhases("non_existent_file.json")
	assert.Error(t, err, "Should error on non-existent file")
	assert.Nil(t, phases, "Should return nil phases on error")
}

func TestParsePhases_InvalidJSON(t *testing.T) {
	// Create a temporary file with invalid JSON
	tempDir := t.TempDir()
	phaseFile := filepath.Join(tempDir, "invalid.json")

	err := os.WriteFile(phaseFile, []byte("{ invalid json }"), 0o600)
	require.NoError(t, err, "Should write invalid JSON file")

	phases, err := parsePhases(phaseFile)
	assert.Error(t, err, "Should error on invalid JSON")
	assert.Nil(t, phases, "Should return nil phases on error")
}

func TestParsePhases_EmptyJSON(t *testing.T) {
	// Create a temporary file with empty JSON array
	tempDir := t.TempDir()
	phaseFile := filepath.Join(tempDir, "empty.json")

	err := os.WriteFile(phaseFile, []byte("[]"), 0o600)
	require.NoError(t, err, "Should write empty JSON file")

	phases, err := parsePhases(phaseFile)
	require.NoError(t, err, "Should not error on empty JSON array")
	assert.Empty(t, phases, "Should return empty phases array")
}

func TestPhaseJSON_Validate_ValidData(t *testing.T) {
	pj := phaseJSON{
		DurationSeconds: 10,
		CapacityRatio:   50.0,
		MaxBurstKbps:    100,
		DataLossRate:    5,
		AckLossRate:     2,
		DataDelayMs:     50,
		AckDelayMs:      25,
	}

	err := pj.validate()
	assert.NoError(t, err, "Valid phaseJSON should not error")
}

func TestPhaseJSON_Validate_InvalidDuration(t *testing.T) {
	testCases := []struct {
		name     string
		duration int
		expected error
	}{
		{"Zero duration", 0, errDurationMustBePositive},
		{"Negative duration", -5, errDurationMustBePositive},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pj := phaseJSON{
				DurationSeconds: tc.duration,
				CapacityRatio:   50.0,
				MaxBurstKbps:    100,
				DataLossRate:    5,
				AckLossRate:     2,
				DataDelayMs:     50,
				AckDelayMs:      25,
			}

			err := pj.validate()
			assert.Error(t, err, "Should error on invalid duration")
			assert.Equal(t, tc.expected, err, "Should return correct error")
		})
	}
}

func TestPhaseJSON_Validate_InvalidCapacityRatio(t *testing.T) {
	testCases := []struct {
		name          string
		capacityRatio float64
		expected      error
	}{
		{"Zero capacity ratio", 0, errCapacityRatioInvalid},
		{"Negative capacity ratio", -10.0, errCapacityRatioInvalid},
		{"Over 100 capacity ratio", 150.0, errCapacityRatioInvalid},
		{"Exactly 100 capacity ratio", 100.0, nil}, // 100 should be valid
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pj := phaseJSON{
				DurationSeconds: 10,
				CapacityRatio:   tc.capacityRatio,
				MaxBurstKbps:    100,
				DataLossRate:    5,
				AckLossRate:     2,
				DataDelayMs:     50,
				AckDelayMs:      25,
			}

			err := pj.validate()
			if tc.expected == nil {
				assert.NoError(t, err, "Should not error on valid capacity ratio")
			} else {
				assert.Error(t, err, "Should error on invalid capacity ratio")
				assert.Equal(t, tc.expected, err, "Should return correct error")
			}
		})
	}
}

func TestPhaseJSON_Validate_InvalidMaxBurst(t *testing.T) {
	testCases := []struct {
		name         string
		maxBurstKbps int
		expected     error
	}{
		{"Zero max burst", 0, errMaxBurstMustBePositive},
		{"Negative max burst", -10, errMaxBurstMustBePositive},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pj := phaseJSON{
				DurationSeconds: 10,
				CapacityRatio:   50.0,
				MaxBurstKbps:    tc.maxBurstKbps,
				DataLossRate:    5,
				AckLossRate:     2,
				DataDelayMs:     50,
				AckDelayMs:      25,
			}

			err := pj.validate()
			assert.Error(t, err, "Should error on invalid max burst")
			assert.Equal(t, tc.expected, err, "Should return correct error")
		})
	}
}

func TestPhaseJSON_Validate_InvalidDataLossRate(t *testing.T) {
	testCases := []struct {
		name         string
		dataLossRate int
		expected     error
	}{
		{"Negative data loss rate", -5, errDataLossRateInvalid},
		{"Over 100 data loss rate", 150, errDataLossRateInvalid},
		{"Zero data loss rate", 0, nil},  // 0 should be valid
		{"100 data loss rate", 100, nil}, // 100 should be valid
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pj := phaseJSON{
				DurationSeconds: 10,
				CapacityRatio:   50.0,
				MaxBurstKbps:    100,
				DataLossRate:    tc.dataLossRate,
				AckLossRate:     2,
				DataDelayMs:     50,
				AckDelayMs:      25,
			}

			err := pj.validate()
			if tc.expected == nil {
				assert.NoError(t, err, "Should not error on valid data loss rate")
			} else {
				assert.Error(t, err, "Should error on invalid data loss rate")
				assert.Equal(t, tc.expected, err, "Should return correct error")
			}
		})
	}
}

func TestPhaseJSON_Validate_InvalidAckLossRate(t *testing.T) {
	testCases := []struct {
		name        string
		ackLossRate int
		expected    error
	}{
		{"Negative ack loss rate", -5, errAckLossRateInvalid},
		{"Over 100 ack loss rate", 150, errAckLossRateInvalid},
		{"Zero ack loss rate", 0, nil},  // 0 should be valid
		{"100 ack loss rate", 100, nil}, // 100 should be valid
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pj := phaseJSON{
				DurationSeconds: 10,
				CapacityRatio:   50.0,
				MaxBurstKbps:    100,
				DataLossRate:    5,
				AckLossRate:     tc.ackLossRate,
				DataDelayMs:     50,
				AckDelayMs:      25,
			}

			err := pj.validate()
			if tc.expected == nil {
				assert.NoError(t, err, "Should not error on valid ack loss rate")
			} else {
				assert.Error(t, err, "Should error on invalid ack loss rate")
				assert.Equal(t, tc.expected, err, "Should return correct error")
			}
		})
	}
}

func TestPhaseJSON_Validate_InvalidDataDelay(t *testing.T) {
	testCases := []struct {
		name        string
		dataDelayMs int
		expected    error
	}{
		{"Negative data delay", -10, errDataDelayMustBeNonNegative},
		{"Zero data delay", 0, nil}, // 0 should be valid
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pj := phaseJSON{
				DurationSeconds: 10,
				CapacityRatio:   50.0,
				MaxBurstKbps:    100,
				DataLossRate:    5,
				AckLossRate:     2,
				DataDelayMs:     tc.dataDelayMs,
				AckDelayMs:      25,
			}

			err := pj.validate()
			if tc.expected == nil {
				assert.NoError(t, err, "Should not error on valid data delay")
			} else {
				assert.Error(t, err, "Should error on invalid data delay")
				assert.Equal(t, tc.expected, err, "Should return correct error")
			}
		})
	}
}

func TestPhaseJSON_Validate_InvalidAckDelay(t *testing.T) {
	testCases := []struct {
		name       string
		ackDelayMs int
		expected   error
	}{
		{"Negative ack delay", -10, errAckDelayMustBeNonNegative},
		{"Zero ack delay", 0, nil}, // 0 should be valid
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pj := phaseJSON{
				DurationSeconds: 10,
				CapacityRatio:   50.0,
				MaxBurstKbps:    100,
				DataLossRate:    5,
				AckLossRate:     2,
				DataDelayMs:     50,
				AckDelayMs:      tc.ackDelayMs,
			}

			err := pj.validate()
			if tc.expected == nil {
				assert.NoError(t, err, "Should not error on valid ack delay")
			} else {
				assert.Error(t, err, "Should error on invalid ack delay")
				assert.Equal(t, tc.expected, err, "Should return correct error")
			}
		})
	}
}

func TestPhaseJSON_ConvertToPhase(t *testing.T) {
	pj := phaseJSON{
		DurationSeconds: 15,
		CapacityRatio:   75.5,
		MaxBurstKbps:    200,
		DataLossRate:    8,
		AckLossRate:     3,
		DataDelayMs:     75,
		AckDelayMs:      40,
	}

	convertedPhase := pj.convertToPhase()

	assert.Equal(t, 15*time.Second, convertedPhase.duration, "Duration should be converted correctly")
	assert.Equal(t, 75.5, convertedPhase.capacityRatio, "Capacity ratio should be preserved")
	assert.Equal(t, 200*vnet.KBit, convertedPhase.maxBurst, "Max burst should be converted to bits")
	assert.Equal(t, 8, convertedPhase.dataLossRate, "Data loss rate should be preserved")
	assert.Equal(t, 3, convertedPhase.ackLossRate, "Ack loss rate should be preserved")
	assert.Equal(t, 75*time.Millisecond, convertedPhase.dataDelay, "Data delay should be converted to duration")
	assert.Equal(t, 40*time.Millisecond, convertedPhase.ackDelay, "Ack delay should be converted to duration")
}

func TestPhaseJSON_ToPhase_ValidData(t *testing.T) {
	pj := phaseJSON{
		DurationSeconds: 10,
		CapacityRatio:   50.0,
		MaxBurstKbps:    100,
		DataLossRate:    5,
		AckLossRate:     2,
		DataDelayMs:     50,
		AckDelayMs:      25,
	}

	validatedPhase, err := pj.toPhase()
	require.NoError(t, err, "toPhase() should not error on valid data")
	assert.Equal(t, 10*time.Second, validatedPhase.duration, "Duration should be converted correctly")
	assert.Equal(t, 50.0, validatedPhase.capacityRatio, "Capacity ratio should be preserved")
	assert.Equal(t, 100*vnet.KBit, validatedPhase.maxBurst, "Max burst should be converted to bits")
	assert.Equal(t, 5, validatedPhase.dataLossRate, "Data loss rate should be preserved")
	assert.Equal(t, 2, validatedPhase.ackLossRate, "Ack loss rate should be preserved")
	assert.Equal(t, 50*time.Millisecond, validatedPhase.dataDelay, "Data delay should be converted to duration")
	assert.Equal(t, 25*time.Millisecond, validatedPhase.ackDelay, "Ack delay should be converted to duration")
}

func TestPhaseJSON_ToPhase_InvalidData(t *testing.T) {
	pj := phaseJSON{
		DurationSeconds: -5, // Invalid duration
		CapacityRatio:   50.0,
		MaxBurstKbps:    100,
		DataLossRate:    5,
		AckLossRate:     2,
		DataDelayMs:     50,
		AckDelayMs:      25,
	}

	phaseResult, err := pj.toPhase()
	assert.Error(t, err, "toPhase() should error on invalid data")
	assert.Equal(t, errDurationMustBePositive, err, "Should return correct error")
	assert.Equal(t, phase{}, phaseResult, "Should return zero value phase on error")
}

func TestIsValidPhaseFile(t *testing.T) {
	testCases := []struct {
		name     string
		filePath string
		expected bool
	}{
		{"Valid JSON file", "test.json", true},
		{"Valid JSON file with path", "phases/test.json", true},
		{"Valid JSON file with subdirectory", "phases/subdir/test.json", true},
		{"Invalid file extension", "test.txt", false},
		{"Invalid file extension", "test", false},
		{"Directory traversal attempt", "../../../etc/passwd", false},
		{"Directory traversal with dots", "../test.json", false},
		{"Absolute path outside tmp", "/etc/passwd", false},
		{"Absolute path in tmp", "/tmp/test.json", true},
		{"Empty path", "", false},
		{"Current directory", "./test.json", true},
		{"Parent directory traversal", "../test.json", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := isValidPhaseFile(tc.filePath)
			assert.Equal(t, tc.expected, result, "isValidPhaseFile() should return correct result for %s", tc.filePath)
		})
	}
}

func TestParsePhases_InvalidPhaseInArray(t *testing.T) {
	// Create a temporary JSON file with one valid and one invalid phase
	tempDir := t.TempDir()
	phaseFile := filepath.Join(tempDir, "mixed_phases.json")

	// Create JSON with mixed valid and invalid phases
	phases := []map[string]any{
		{
			"durationSeconds": 10,
			"capacityRatio":   50.0,
			"maxBurstKbps":    100,
			"dataLossRate":    5,
			"ackLossRate":     2,
			"dataDelayMs":     50,
			"ackDelayMs":      25,
		},
		{
			"durationSeconds": -5, // Invalid duration
			"capacityRatio":   50.0,
			"maxBurstKbps":    100,
			"dataLossRate":    5,
			"ackLossRate":     2,
			"dataDelayMs":     50,
			"ackDelayMs":      25,
		},
	}

	// Write JSON file
	jsonData, err := json.Marshal(phases)
	require.NoError(t, err, "Should marshal phases to JSON")

	err = os.WriteFile(phaseFile, jsonData, 0o600)
	require.NoError(t, err, "Should write phase file")

	// Test parsePhases
	phasesResult, err := parsePhases(phaseFile)
	assert.Error(t, err, "Should error on invalid phase in array")
	assert.Nil(t, phasesResult, "Should return nil phases on error")
	assert.Contains(t, err.Error(), "phase 1", "Error should mention which phase failed")
}

func TestPhaseErrorConstants(t *testing.T) {
	// Test that all error constants are properly defined
	assert.NotNil(t, errDurationMustBePositive, "errDurationMustBePositive should be defined")
	assert.NotNil(t, errCapacityRatioInvalid, "errCapacityRatioInvalid should be defined")
	assert.NotNil(t, errMaxBurstMustBePositive, "errMaxBurstMustBePositive should be defined")
	assert.NotNil(t, errDataLossRateInvalid, "errDataLossRateInvalid should be defined")
	assert.NotNil(t, errAckLossRateInvalid, "errAckLossRateInvalid should be defined")
	assert.NotNil(t, errDataDelayMustBeNonNegative, "errDataDelayMustBeNonNegative should be defined")
	assert.NotNil(t, errAckDelayMustBeNonNegative, "errAckDelayMustBeNonNegative should be defined")

	// Test error messages
	assert.Contains(t, errDurationMustBePositive.Error(), "duration", "Error message should mention duration")
	assert.Contains(t, errCapacityRatioInvalid.Error(), "capacityRatio", "Error message should mention capacityRatio")
	assert.Contains(t, errMaxBurstMustBePositive.Error(), "maxBurstKbps", "Error message should mention maxBurstKbps")
	assert.Contains(t, errDataLossRateInvalid.Error(), "dataLossRate", "Error message should mention dataLossRate")
	assert.Contains(t, errAckLossRateInvalid.Error(), "ackLossRate", "Error message should mention ackLossRate")
	assert.Contains(t, errDataDelayMustBeNonNegative.Error(), "dataDelay", "Error message should mention dataDelay")
	assert.Contains(t, errAckDelayMustBeNonNegative.Error(), "ackDelay", "Error message should mention ackDelay")
}
