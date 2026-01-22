// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package main

import (
	"testing"
	"time"

	"github.com/pion/transport/v3/vnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager(t *testing.T) {
	manager, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")
	assert.NotNil(t, manager, "NewManager() should return non-nil manager")
	assert.NotNil(t, manager.leftNetComponents, "leftNetComponents should be initialized")
	assert.NotNil(t, manager.rightNetComponents, "rightNetComponents should be initialized")
}

func TestNetworkManager_GetLeftNet(t *testing.T) {
	manager, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	net, publicIP, err := manager.GetLeftNet()
	require.NoError(t, err, "GetLeftNet() should not error")
	assert.NotNil(t, net, "GetLeftNet() should return non-nil net")
	assert.NotEmpty(t, publicIP, "GetLeftNet() should return non-empty public IP")
	assert.Equal(t, "10.0.1.1", publicIP, "GetLeftNet() should return correct public IP")
}

func TestNetworkManager_GetRightNet(t *testing.T) {
	manager, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	net, publicIP, err := manager.GetRightNet()
	require.NoError(t, err, "GetRightNet() should not error")
	assert.NotNil(t, net, "GetRightNet() should return non-nil net")
	assert.NotEmpty(t, publicIP, "GetRightNet() should return non-empty public IP")
	assert.Equal(t, "10.0.2.1", publicIP, "GetRightNet() should return correct public IP")
}

func TestNetworkManager_GetLeftNet_MultipleCalls(t *testing.T) {
	manager, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// First call should succeed
	net1, _, err := manager.GetLeftNet()
	require.NoError(t, err, "First GetLeftNet() should not error")
	assert.NotNil(t, net1, "First GetLeftNet() should return non-nil net")

	// Second call should also succeed since IPs are not marked as used in current implementation
	net2, _, err := manager.GetLeftNet()
	require.NoError(t, err, "Second GetLeftNet() should not error")
	assert.NotNil(t, net2, "Second GetLeftNet() should return non-nil net")
}

func TestNetworkManager_GetRightNet_MultipleCalls(t *testing.T) {
	manager, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// First call should succeed
	net1, _, err := manager.GetRightNet()
	require.NoError(t, err, "First GetRightNet() should not error")
	assert.NotNil(t, net1, "First GetRightNet() should return non-nil net")

	// Second call should also succeed since IPs are not marked as used in current implementation
	net2, _, err := manager.GetRightNet()
	require.NoError(t, err, "Second GetRightNet() should not error")
	assert.NotNil(t, net2, "Second GetRightNet() should return non-nil net")
}

func TestNetworkManager_SetCapacity(t *testing.T) {
	manager, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Test setting capacity for both sides
	capacity := int(2 * vnet.MBit)
	maxBurst := int(100 * vnet.KBit)

	manager.SetCapacity(capacity, maxBurst)

	// Verify that both sides have the capacity set
	// We can't directly test the TBF values, but we can ensure the method doesn't panic
	assert.NotNil(t, manager.leftNetComponents.tbf, "Left TBF should be initialized")
	assert.NotNil(t, manager.rightNetComponents.tbf, "Right TBF should be initialized")
}

func TestNetworkManager_SetLeftCapacity(t *testing.T) {
	manager, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	capacity := int(1.5 * vnet.MBit)
	maxBurst := int(50 * vnet.KBit)

	manager.SetLeftCapacity(capacity, maxBurst)

	assert.NotNil(t, manager.leftNetComponents.tbf, "Left TBF should be initialized")
}

func TestNetworkManager_SetRightCapacity(t *testing.T) {
	manager, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	capacity := int(3 * vnet.MBit)
	maxBurst := int(150 * vnet.KBit)

	manager.SetRightCapacity(capacity, maxBurst)

	assert.NotNil(t, manager.rightNetComponents.tbf, "Right TBF should be initialized")
}

func TestNetworkManager_SetAckLossRate(t *testing.T) {
	manager, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	lossRate := 5
	manager.SetAckLossRate(lossRate)

	assert.NotNil(t, manager.leftNetComponents.lossFilter, "Left loss filter should be initialized")
}

func TestNetworkManager_SetDataLossRate(t *testing.T) {
	manager, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	lossRate := 10
	manager.SetDataLossRate(lossRate)

	assert.NotNil(t, manager.rightNetComponents.lossFilter, "Right loss filter should be initialized")
}

func TestNetworkManager_SetDataDelay(t *testing.T) {
	manager, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	delay := 50 * time.Millisecond
	manager.SetDataDelay(delay)

	assert.NotNil(t, manager.rightNetComponents.delayFilter, "Right delay filter should be initialized")
}

func TestNetworkManager_SetAckDelay(t *testing.T) {
	manager, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	delay := 25 * time.Millisecond
	manager.SetAckDelay(delay)

	assert.NotNil(t, manager.leftNetComponents.delayFilter, "Left delay filter should be initialized")
}

func TestRouterWithConfig_getIPMapping(t *testing.T) {
	routerConfig := &vnet.RouterConfig{
		StaticIPs: []string{
			"10.0.1.1/10.0.1.101",
			"10.0.1.2/10.0.1.102",
		},
	}

	routerWithConfig := &RouterWithConfig{
		RouterConfig: routerConfig,
		usedIPs:      make(map[string]bool),
	}

	// First call should return the first IP
	private, public, err := routerWithConfig.getIPMapping()
	require.NoError(t, err, "getIPMapping() should not error")
	assert.Equal(t, "10.0.1.101", private, "Should return correct private IP")
	assert.Equal(t, "10.0.1.1", public, "Should return correct public IP")

	// Mark the first IP as used
	routerWithConfig.usedIPs["10.0.1.1/10.0.1.101"] = true

	// Second call should return the second IP
	private, public, err = routerWithConfig.getIPMapping()
	require.NoError(t, err, "getIPMapping() should not error")
	assert.Equal(t, "10.0.1.102", private, "Should return correct private IP")
	assert.Equal(t, "10.0.1.2", public, "Should return correct public IP")

	// Mark the second IP as used
	routerWithConfig.usedIPs["10.0.1.2/10.0.1.102"] = true

	// Third call should return error
	_, _, err = routerWithConfig.getIPMapping()
	assert.Error(t, err, "getIPMapping() should error when no IPs available")
	assert.Equal(t, errNoIPAvailiable, err, "Should return errNoIPAvailiable")
}

func TestRouterWithConfig_getIPMapping_EmptyStaticIPs(t *testing.T) {
	routerConfig := &vnet.RouterConfig{
		StaticIPs: []string{},
	}

	routerWithConfig := &RouterWithConfig{
		RouterConfig: routerConfig,
		usedIPs:      make(map[string]bool),
	}

	_, _, err := routerWithConfig.getIPMapping()
	assert.Error(t, err, "getIPMapping() should error with empty StaticIPs")
	assert.Equal(t, errNoIPAvailiable, err, "Should return errNoIPAvailiable")
}

func TestRouterWithConfig_getIPMapping_InvalidIPFormat(t *testing.T) {
	routerConfig := &vnet.RouterConfig{
		StaticIPs: []string{
			"invalid-ip-format",
		},
	}

	routerWithConfig := &RouterWithConfig{
		RouterConfig: routerConfig,
		usedIPs:      make(map[string]bool),
	}

	// This should panic due to index out of range when splitting the invalid format
	// We test that it panics as expected
	assert.Panics(t, func() {
		_, _, _ = routerWithConfig.getIPMapping()
	}, "getIPMapping() should panic with invalid IP format")
}

func TestNewLeftNet(t *testing.T) {
	components, err := newLeftNet()
	require.NoError(t, err, "newLeftNet() should not error")
	assert.NotNil(t, components, "newLeftNet() should return non-nil components")
	assert.NotNil(t, components.routerWithConfig, "routerWithConfig should be initialized")
	assert.NotNil(t, components.tbf, "TBF should be initialized")
	assert.NotNil(t, components.lossFilter, "lossFilter should be initialized")
	assert.NotNil(t, components.delayFilter, "delayFilter should be initialized")
}

func TestNewRightNet(t *testing.T) {
	components, err := newRightNet()
	require.NoError(t, err, "newRightNet() should not error")
	assert.NotNil(t, components, "newRightNet() should return non-nil components")
	assert.NotNil(t, components.routerWithConfig, "routerWithConfig should be initialized")
	assert.NotNil(t, components.tbf, "TBF should be initialized")
	assert.NotNil(t, components.lossFilter, "lossFilter should be initialized")
	assert.NotNil(t, components.delayFilter, "delayFilter should be initialized")
}

func TestNetworkComponents_Initialization(t *testing.T) {
	manager, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Test left network components
	leftComponents := manager.leftNetComponents
	assert.NotNil(t, leftComponents.routerWithConfig, "Left routerWithConfig should be initialized")
	assert.NotNil(t, leftComponents.tbf, "Left TBF should be initialized")
	assert.NotNil(t, leftComponents.lossFilter, "Left lossFilter should be initialized")
	assert.NotNil(t, leftComponents.delayFilter, "Left delayFilter should be initialized")

	// Test right network components
	rightComponents := manager.rightNetComponents
	assert.NotNil(t, rightComponents.routerWithConfig, "Right routerWithConfig should be initialized")
	assert.NotNil(t, rightComponents.tbf, "Right TBF should be initialized")
	assert.NotNil(t, rightComponents.lossFilter, "Right lossFilter should be initialized")
	assert.NotNil(t, rightComponents.delayFilter, "Right delayFilter should be initialized")
}

func TestNetworkManager_Integration(t *testing.T) {
	manager, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Test complete workflow
	capacity := int(2 * vnet.MBit)
	maxBurst := int(100 * vnet.KBit)
	lossRate := 5
	delay := 50 * time.Millisecond

	// Set network parameters
	manager.SetCapacity(capacity, maxBurst)
	manager.SetAckLossRate(lossRate)
	manager.SetDataLossRate(lossRate)
	manager.SetAckDelay(delay)
	manager.SetDataDelay(delay)

	// Get networks
	leftNet, leftIP, err := manager.GetLeftNet()
	require.NoError(t, err, "GetLeftNet() should not error")
	assert.NotNil(t, leftNet, "Left net should not be nil")
	assert.NotEmpty(t, leftIP, "Left IP should not be empty")

	rightNet, rightIP, err := manager.GetRightNet()
	require.NoError(t, err, "GetRightNet() should not error")
	assert.NotNil(t, rightNet, "Right net should not be nil")
	assert.NotEmpty(t, rightIP, "Right IP should not be empty")

	// Verify IPs are different
	assert.NotEqual(t, leftIP, rightIP, "Left and right IPs should be different")
}

func TestConstants(t *testing.T) {
	// Test that constants are properly defined
	assert.Equal(t, 1*vnet.MBit, initCapacity, "initCapacity should be 1 MBit")
	assert.Equal(t, 80*vnet.KBit, initMaxBurst, "initMaxBurst should be 80 KBit")
}

func TestManagerErrorConstants(t *testing.T) {
	// Test that error constants are properly defined
	assert.NotNil(t, errNoIPAvailiable, "errNoIPAvailiable should be defined")
	assert.Equal(t, "no IP available", errNoIPAvailiable.Error(), "errNoIPAvailiable should have correct message")
}
