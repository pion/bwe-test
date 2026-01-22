// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

// Package phase provides functionality for parsing and managing network phases.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pion/transport/v3/vnet"
)

// pathCharacteristics defines the network characteristics for the test.
type pathCharacteristics struct {
	referenceCapacity int
	phases            []phase
}

var (
	errDurationMustBePositive     = errors.New("duration must be greater than 0")
	errCapacityRatioInvalid       = errors.New("capacityRatio must be between 0 and 100")
	errMaxBurstMustBePositive     = errors.New("maxBurstKbps must be greater than 0")
	errDataLossRateInvalid        = errors.New("dataLossRate must be between 0 and 100")
	errAckLossRateInvalid         = errors.New("ackLossRate must be between 0 and 100")
	errDataDelayMustBeNonNegative = errors.New("dataDelay must be non-negative")
	errAckDelayMustBeNonNegative  = errors.New("ackDelay must be non-negative")
	errInvalidPhaseFilePath       = errors.New("invalid phase file path")
)

// GetPathCharacteristics parses the phases from the given phaseFile and returns a pathCharacteristics struct.
func GetPathCharacteristics(phaseFile string, referenceCapacity int) pathCharacteristics {
	if phaseFile == "" {
		return pathCharacteristics{}
	}
	phases, err := parsePhases(phaseFile)
	if err != nil {
		// Return empty pathCharacteristics on error instead of fatal exit
		return pathCharacteristics{}
	}

	return pathCharacteristics{
		// capacity of the reference path in bps (bits per second)
		referenceCapacity: referenceCapacity,
		phases:            phases,
	}
}

// helper function that parses the phases from the given phaseFile.
// Return: a slice of phases.
func parsePhases(phaseFile string) ([]phase, error) {
	// Validate file path to prevent directory traversal
	if !isValidPhaseFile(phaseFile) {
		return nil, fmt.Errorf("%w: %s", errInvalidPhaseFilePath, phaseFile)
	}

	jsonFile, err := os.Open(phaseFile) //nolint:gosec // File path validated by isValidPhaseFile
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = jsonFile.Close()
	}()

	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		return nil, err
	}

	var jsonPhases []phaseJSON
	if err := json.Unmarshal(byteValue, &jsonPhases); err != nil {
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

	// Log parsing success (removed fmt.Printf for linting compliance)

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
	if err := pj.validate(); err != nil {
		return phase{}, err
	}

	return pj.convertToPhase(), nil
}

func (pj phaseJSON) validate() error {
	if err := pj.validateBasicFields(); err != nil {
		return err
	}
	if err := pj.validateLossRates(); err != nil {
		return err
	}
	if err := pj.validateDelays(); err != nil {
		return err
	}

	return nil
}

func (pj phaseJSON) validateBasicFields() error {
	if pj.DurationSeconds <= 0 {
		return errDurationMustBePositive
	}
	if pj.CapacityRatio <= 0 || pj.CapacityRatio > 100 {
		return errCapacityRatioInvalid
	}
	if pj.MaxBurstKbps <= 0 {
		return errMaxBurstMustBePositive
	}

	return nil
}

func (pj phaseJSON) validateLossRates() error {
	if pj.DataLossRate < 0 || pj.DataLossRate > 100 {
		return errDataLossRateInvalid
	}
	if pj.AckLossRate < 0 || pj.AckLossRate > 100 {
		return errAckLossRateInvalid
	}

	return nil
}

func (pj phaseJSON) validateDelays() error {
	if pj.DataDelayMs < 0 {
		return errDataDelayMustBeNonNegative
	}
	if pj.AckDelayMs < 0 {
		return errAckDelayMustBeNonNegative
	}

	return nil
}

func (pj phaseJSON) convertToPhase() phase {
	return phase{
		duration:      time.Duration(pj.DurationSeconds) * time.Second,
		capacityRatio: pj.CapacityRatio,
		maxBurst:      pj.MaxBurstKbps * vnet.KBit,
		dataLossRate:  pj.DataLossRate,
		ackLossRate:   pj.AckLossRate,
		dataDelay:     time.Duration(pj.DataDelayMs) * time.Millisecond,
		ackDelay:      time.Duration(pj.AckDelayMs) * time.Millisecond,
	}
}

// isValidPhaseFile validates that the file path is safe and within allowed directories.
func isValidPhaseFile(phaseFile string) bool {
	// Clean the path to resolve any .. or . components
	cleanPath := filepath.Clean(phaseFile)

	// Check for directory traversal attempts
	if strings.Contains(cleanPath, "..") {
		return false
	}

	// Only allow files in the current directory or subdirectories
	// This prevents access to system files or files outside the project
	if filepath.IsAbs(cleanPath) && !strings.HasPrefix(cleanPath, "/tmp/") {
		return false
	}

	// Must be a .json file
	return strings.HasSuffix(cleanPath, ".json")
}
