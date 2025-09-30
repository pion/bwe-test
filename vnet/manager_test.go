// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package main

import (
	"testing"
	"time"

	"github.com/pion/transport/v3/vnet"
	"github.com/stretchr/testify/assert"
)

func TestNetworkManagerOptions(t *testing.T) {
	testCases := []struct {
		name     string
		options  []NetworkManagerOption
		expected NetworkManagerSettings
	}{
		{
			name:    "default settings",
			options: nil,
			expected: NetworkManagerSettings{
				leftCapacity:  int(initCapacity),
				rightCapacity: int(initCapacity),
				maxBurst:      int(initMaxBurst),
			},
		},
		{
			name: "with data loss rate",
			options: []NetworkManagerOption{
				WithDataLossRate(10),
			},
			expected: NetworkManagerSettings{
				leftCapacity:  int(initCapacity),
				rightCapacity: int(initCapacity),
				maxBurst:      int(initMaxBurst),
				dataLossRate:  10,
			},
		},
		{
			name: "with ack loss rate",
			options: []NetworkManagerOption{
				WithAckLossRate(5),
			},
			expected: NetworkManagerSettings{
				leftCapacity:  int(initCapacity),
				rightCapacity: int(initCapacity),
				maxBurst:      int(initMaxBurst),
				ackLossRate:   5,
			},
		},
		{
			name: "with data delay",
			options: []NetworkManagerOption{
				WithDataDelay(100 * time.Millisecond),
			},
			expected: NetworkManagerSettings{
				leftCapacity:  int(initCapacity),
				rightCapacity: int(initCapacity),
				maxBurst:      int(initMaxBurst),
				dataDelay:     100 * time.Millisecond,
			},
		},
		{
			name: "with ack delay",
			options: []NetworkManagerOption{
				WithAckDelay(50 * time.Millisecond),
			},
			expected: NetworkManagerSettings{
				leftCapacity:  int(initCapacity),
				rightCapacity: int(initCapacity),
				maxBurst:      int(initMaxBurst),
				ackDelay:      50 * time.Millisecond,
			},
		},
		{
			name: "with left capacity",
			options: []NetworkManagerOption{
				WithLeftCapacity(2 * vnet.MBit),
			},
			expected: NetworkManagerSettings{
				leftCapacity:  2 * vnet.MBit,
				rightCapacity: int(initCapacity),
				maxBurst:      int(initMaxBurst),
			},
		},
		{
			name: "with right capacity",
			options: []NetworkManagerOption{
				WithRightCapacity(3 * vnet.MBit),
			},
			expected: NetworkManagerSettings{
				leftCapacity:  int(initCapacity),
				rightCapacity: 3 * vnet.MBit,
				maxBurst:      int(initMaxBurst),
			},
		},
		{
			name: "with max burst",
			options: []NetworkManagerOption{
				WithMaxBurst(200 * vnet.KBit),
			},
			expected: NetworkManagerSettings{
				leftCapacity:  int(initCapacity),
				rightCapacity: int(initCapacity),
				maxBurst:      200 * vnet.KBit,
			},
		},
		{
			name: "multiple options",
			options: []NetworkManagerOption{
				WithDataLossRate(15),
				WithAckLossRate(8),
				WithDataDelay(150 * time.Millisecond),
				WithAckDelay(75 * time.Millisecond),
				WithLeftCapacity(4 * vnet.MBit),
				WithRightCapacity(5 * vnet.MBit),
				WithMaxBurst(300 * vnet.KBit),
			},
			expected: NetworkManagerSettings{
				dataLossRate:  15,
				ackLossRate:   8,
				dataDelay:     150 * time.Millisecond,
				ackDelay:      75 * time.Millisecond,
				leftCapacity:  4 * vnet.MBit,
				rightCapacity: 5 * vnet.MBit,
				maxBurst:      300 * vnet.KBit,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test that options are applied correctly
			settings := NetworkManagerSettings{
				leftCapacity:  int(initCapacity),
				rightCapacity: int(initCapacity),
				maxBurst:      int(initMaxBurst),
			}

			for _, option := range tc.options {
				option(&settings)
			}

			assert.Equal(t, tc.expected, settings)
		})
	}
}

func TestNetworkManagerSettings_Comparability(t *testing.T) {
	// Test that NetworkManagerSettings is comparable
	s1 := NetworkManagerSettings{
		dataLossRate:  5,
		ackLossRate:   2,
		dataDelay:     100 * time.Millisecond,
		ackDelay:      50 * time.Millisecond,
		leftCapacity:  1 * vnet.MBit,
		rightCapacity: 1 * vnet.MBit,
		maxBurst:      80 * vnet.KBit,
	}

	s2 := NetworkManagerSettings{
		dataLossRate:  5,
		ackLossRate:   2,
		dataDelay:     100 * time.Millisecond,
		ackDelay:      50 * time.Millisecond,
		leftCapacity:  1 * vnet.MBit,
		rightCapacity: 1 * vnet.MBit,
		maxBurst:      80 * vnet.KBit,
	}

	s3 := NetworkManagerSettings{
		dataLossRate: 10,
	}

	// Test equality
	assert.True(t, s1 == s2)
	assert.False(t, s1 == s3)
}

func TestNetworkManager_SetMethods(t *testing.T) {
	// Note: These tests verify the methods exist and can be called
	// The actual network impairment functionality is not implemented
	// for standard pion/transport compatibility

	nm := &NetworkManager{}

	// Test that all set methods can be called without panicking
	assert.NotPanics(t, func() {
		nm.SetAckLossRate(5)
		nm.SetDataLossRate(10)
		nm.SetDataDelay(100 * time.Millisecond)
		nm.SetAckDelay(50 * time.Millisecond)
	})
}

func TestNetworkManager_Comparability(t *testing.T) {
	// Test that NetworkManager struct is comparable
	nm1 := NetworkManager{}
	nm2 := NetworkManager{}

	// This should compile and work since NetworkManager should be comparable
	assert.True(t, nm1 == nm2)

	// Test with different values
	nm3 := NetworkManager{wan: nil, leftRouter: nil}
	assert.True(t, nm1 == nm3) // Both have nil fields
}
