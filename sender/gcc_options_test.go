// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js

package sender

import (
	"testing"

	"github.com/pion/interceptor/pkg/gcc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Options passed to GCC must reach the estimator. Without the pass-through there
// is no way to configure anything the two bitrate arguments do not cover.
func TestNewGCCFactoryAppliesExtraOptions(t *testing.T) {
	var applied bool
	marker := func(*gcc.SendSideBWE) error {
		applied = true

		return nil
	}

	est, err := newGCCFactory(500_000, 1_500_000, marker)()
	require.NoError(t, err)
	require.NotNil(t, est)
	defer func() { assert.NoError(t, est.Close()) }()

	assert.True(t, applied, "an option passed to newGCCFactory must reach NewSendSideBWE")
}

// extra is appended after the bitrate options, so a caller can override them.
func TestExtraOptionsOverrideTheBitrateArguments(t *testing.T) {
	est, err := newGCCFactory(500_000, 1_500_000, gcc.SendSideBWEInitialBitrate(900_000))()
	require.NoError(t, err)
	require.NotNil(t, est)
	defer func() { assert.NoError(t, est.Close()) }()

	assert.Equal(t, 900_000, est.GetTargetBitrate(),
		"the later option must win over the initialBitrate argument")
}

// The variadic addition must not break existing two-argument callers.
func TestGCCRemainsCallableWithTwoArguments(t *testing.T) {
	s, err := NewRTCSender(GCC(500_000, 1_500_000))
	require.NoError(t, err)
	assert.NotNil(t, s)
}
