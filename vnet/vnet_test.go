// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package main

import (
	"testing"
	"testing/synctest"

	"github.com/pion/logging"
	"github.com/stretchr/testify/assert"
)

func TestVnet(t *testing.T) {
	lf := logging.NewDefaultLoggerFactory()
	logger := lf.NewLogger("bwe_vnet_synctest")

	testCases := []struct {
		name       string
		senderMode senderMode
		flowMode   flowMode
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
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				t.Helper()
				runner := Runner{
					loggerFactory: lf,
					logger:        logger,
					name:          tc.name,
					senderMode:    tc.senderMode,
					flowMode:      tc.flowMode,
				}
				err := runner.Run()
				assert.NoError(t, err)
				synctest.Wait()
			})
		})
	}
}
