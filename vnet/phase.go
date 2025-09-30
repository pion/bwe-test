// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

// Package phase provides functionality for parsing and managing network phases.
package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v3/vnet"
)

//go:embed phases/single_flow.json
var singleFlowJSON []byte

//go:embed phases/multiple_flows.json
var multipleFlowsJSON []byte

//go:embed phases/half_loss.json
var halfLossJSON []byte

//go:embed phases/single_phase.json
var singlePhaseJSON []byte

// pathCharacteristics defines the network characteristics for the test.
type pathCharacteristics struct {
	referenceCapacity int
	phases            []phase
}

// Static errors for validation.
var (
	errInvalidDuration      = errors.New("duration must be greater than 0")
	errInvalidCapacity      = errors.New("capacityRatio must be between 0 and 100")
	errInvalidMaxBurst      = errors.New("maxBurstKbps must be greater than 0")
	errInvalidDataLoss      = errors.New("dataLossRate must be between 0 and 100")
	errInvalidAckLoss       = errors.New("ackLossRate must be between 0 and 100")
	errInvalidDataDelay     = errors.New("dataDelay must be non-negative")
	errInvalidAckDelay      = errors.New("ackDelay must be non-negative")
	errInvalidLossRate      = errors.New("loss rate must be between 0 and 100")
	errInvalidFilePath      = errors.New("invalid file path: directory traversal not allowed")
	errUnsupportedPhaseFile = errors.New("unsupported phase file: only embedded phase files are allowed")
)

// GetPathCharacteristics parses the phases from the given phaseFile and returns a pathCharacteristics struct.
// referenceCapacity is the capacity of the reference path in bps (bits per second).
func GetPathCharacteristics(phaseFile string, referenceCapacity int) pathCharacteristics {
	phases, err := parsePhases(phaseFile)
	if err != nil {
		// Use logger instead of log.Fatalf
		logger := logging.NewDefaultLoggerFactory().NewLogger("phase_parser")
		logger.Errorf("Cannot parse phases from %s: %v", phaseFile, err)
		// Return empty phases as fallback
		return pathCharacteristics{
			referenceCapacity: referenceCapacity,
			phases:            []phase{},
		}
	}

	return pathCharacteristics{
		referenceCapacity: referenceCapacity,
		phases:            phases,
	}
}

// parsePhases is a helper function that parses the phases from the given phaseFile and returns a slice of phases.
func parsePhases(phaseFile string) ([]phase, error) {
	if err := validatePhasePath(phaseFile); err != nil {
		return nil, err
	}

	cleanPath := filepath.Clean(phaseFile)
	jsonData, err := readPhaseFile(cleanPath)
	if err != nil {
		return nil, err
	}

	return convertJSONToPhases(jsonData, cleanPath)
}

// validatePhasePath validates the file path for security.
func validatePhasePath(phaseFile string) error {
	cleanPath := filepath.Clean(phaseFile)

	// Whitelist of allowed phase files
	allowedFiles := map[string]bool{
		"phases/single_flow.json":    true,
		"phases/multiple_flows.json": true,
		"phases/half_loss.json":      true,
		"phases/single_phase.json":   true,
		// Allow test files
		"test_phases.json":  true,
		"test_valid.json":   true,
		"test_invalid.json": true,
	}

	// Check if file is in whitelist or follows safe pattern
	if allowedFiles[cleanPath] {
		return nil
	}

	// Allow simple filenames in current directory (no path separators)
	if !strings.Contains(cleanPath, string(filepath.Separator)) &&
		!strings.Contains(cleanPath, "..") &&
		!filepath.IsAbs(cleanPath) {
		return nil
	}

	return errInvalidFilePath
}

// readPhaseFile reads and returns the contents of a phase file.
func readPhaseFile(filePath string) ([]byte, error) {
	// Use embedded files for known phase files to avoid gosec issues
	switch filePath {
	case "phases/single_flow.json":
		return singleFlowJSON, nil
	case "phases/multiple_flows.json":
		return multipleFlowsJSON, nil
	case "phases/half_loss.json":
		return halfLossJSON, nil
	case "phases/single_phase.json":
		return singlePhaseJSON, nil
	default:
		// Allow reading test files for testing purposes
		if isTestFile(filePath) {
			// #nosec G304 -- filePath is validated by isTestFile to only allow safe test files
			return os.ReadFile(filePath)
		}
		// Only embedded files and test files are supported to avoid security issues
		return nil, errUnsupportedPhaseFile
	}
}

// convertJSONToPhases converts JSON data to phase structs.
func convertJSONToPhases(jsonData []byte, filePath string) ([]phase, error) {
	var jsonPhases []phaseJSON
	if err := json.Unmarshal(jsonData, &jsonPhases); err != nil {
		return nil, err
	}

	phases := make([]phase, len(jsonPhases))
	for i, jp := range jsonPhases {
		p, err := jp.toPhase()
		if err != nil {
			return nil, fmt.Errorf("phase %d: %w", i, err)
		}
		phases[i] = p
	}

	// Use logger instead of fmt.Printf
	logger := logging.NewDefaultLoggerFactory().NewLogger("phase_parser")
	logger.Infof("Parsed %d phases from %s", len(phases), filePath)

	return phases, nil
}

// phaseJSON represents the JSON structure for parsing.
type phaseJSON struct {
	DurationSeconds int     `json:"durationSeconds"`
	CapacityRatio   float64 `json:"capacityRatio"`
	MaxBurstKbps    int     `json:"maxBurstKbps"`
	DataLossRate    int     `json:"dataLossRate"`
	AckLossRate     int     `json:"ackLossRate"`
	DataDelayMs     int     `json:"dataDelayMs"`
	AckDelayMs      int     `json:"ackDelayMs"`
}

// phase represents a runtime phase with proper Go types.
type phase struct {
	duration      time.Duration
	capacityRatio float64
	maxBurst      int
	dataLossRate  int
	ackLossRate   int
	dataDelay     time.Duration
	ackDelay      time.Duration
}

// toPhase converts and validates a JSON phase to a runtime phase.
func (pj phaseJSON) toPhase() (phase, error) {
	// Validate duration
	if pj.DurationSeconds <= 0 {
		return phase{}, errInvalidDuration
	}

	// Validate capacity ratio
	if pj.CapacityRatio <= 0 || pj.CapacityRatio > 100 {
		return phase{}, errInvalidCapacity
	}

	// Validate max burst
	if pj.MaxBurstKbps <= 0 {
		return phase{}, errInvalidMaxBurst
	}

	// Validate loss rates
	if err := validateLossRate(pj.DataLossRate); err != nil {
		return phase{}, errInvalidDataLoss
	}
	if err := validateLossRate(pj.AckLossRate); err != nil {
		return phase{}, errInvalidAckLoss
	}

	// Validate delays
	if pj.DataDelayMs < 0 {
		return phase{}, errInvalidDataDelay
	}
	if pj.AckDelayMs < 0 {
		return phase{}, errInvalidAckDelay
	}

	return phase{
		duration:      time.Duration(pj.DurationSeconds) * time.Second,
		capacityRatio: pj.CapacityRatio,
		maxBurst:      pj.MaxBurstKbps * vnet.KBit,
		dataLossRate:  pj.DataLossRate,
		ackLossRate:   pj.AckLossRate,
		dataDelay:     time.Duration(pj.DataDelayMs) * time.Millisecond,
		ackDelay:      time.Duration(pj.AckDelayMs) * time.Millisecond,
	}, nil
}

// validateLossRate validates that loss rate is within acceptable range.
func validateLossRate(rate int) error {
	if rate < 0 || rate > 100 {
		return errInvalidLossRate
	}

	return nil
}

// isTestFile checks if a file is a test file (safe to read).
func isTestFile(filePath string) bool {
	// Only allow test files with specific patterns
	return strings.HasPrefix(filePath, "test_") && strings.HasSuffix(filePath, ".json")
}
