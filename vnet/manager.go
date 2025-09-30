// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

// Package main implements virtual network functionality for bandwidth estimation tests.
package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v3/vnet"
)

var errCleanupFailed = errors.New("cleanup failed")

var errNoIPAvailiable = errors.New("no IP available")

// RouterWithConfig combines a vnet Router with its configuration and IP tracking.
type RouterWithConfig struct {
	*vnet.RouterConfig
	*vnet.Router
	usedIPs map[string]bool
}

func (r *RouterWithConfig) getIPMapping() (private, public string, err error) {
	if len(r.usedIPs) >= len(r.StaticIPs) {
		return "", "", errNoIPAvailiable
	}
	ip := r.StaticIPs[0]
	for i := 1; i < len(r.StaticIPs); i++ {
		if _, ok := r.usedIPs[ip]; !ok {
			break
		}
		ip = r.StaticIPs[i]
	}
	mapping := strings.Split(ip, "/")
	public = mapping[0]
	private = mapping[1]

	return private, public, err
}

// NetworkManager manages the virtual network topology for bandwidth estimation tests.
type NetworkManager struct {
	wan         *vnet.Router
	leftRouter  *RouterWithConfig
	leftTBF     *vnet.TokenBucketFilter
	rightRouter *RouterWithConfig
	rightTBF    *vnet.TokenBucketFilter
}

const (
	initCapacity = 1 * vnet.MBit
	initMaxBurst = 80 * vnet.KBit
)

// NewManager creates a new NetworkManager with default configuration.
func NewManager() (*NetworkManager, error) {
	wan, err := vnet.NewRouter(&vnet.RouterConfig{
		CIDR:          "0.0.0.0/0",
		LoggerFactory: logging.NewDefaultLoggerFactory(),
	})
	if err != nil {
		return nil, err
	}

	leftRouter, leftTBF, err := newLeftNet()
	if err != nil {
		return nil, err
	}

	err = wan.AddNet(leftTBF)
	if err != nil {
		return nil, err
	}
	err = wan.AddChildRouter(leftRouter.Router)
	if err != nil {
		return nil, err
	}

	rightRouter, rightTBF, err := newRightNet()
	if err != nil {
		return nil, err
	}

	err = wan.AddNet(rightTBF)
	if err != nil {
		return nil, err
	}
	err = wan.AddChildRouter(rightRouter.Router)
	if err != nil {
		return nil, err
	}

	manager := &NetworkManager{
		wan:         wan,
		leftRouter:  leftRouter,
		leftTBF:     leftTBF,
		rightRouter: rightRouter,
		rightTBF:    rightTBF,
	}

	if err := wan.Start(); err != nil {
		return nil, err
	}

	return manager, nil
}

// Close stops the network manager and cleans up resources.
func (m *NetworkManager) Close() error {
	var errs []error

	if err := m.wan.Stop(); err != nil {
		errs = append(errs, err)
	}
	if err := m.leftRouter.Stop(); err != nil {
		errs = append(errs, err)
	}
	if err := m.rightRouter.Stop(); err != nil {
		errs = append(errs, err)
	}
	if err := m.leftTBF.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := m.rightTBF.Close(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %v", errCleanupFailed, errs)
	}

	return nil
}

func (m *NetworkManager) GetLeftNet() (*vnet.Net, string, error) {
	privateIP, publicIP, err := m.leftRouter.getIPMapping()
	if err != nil {
		return nil, "", err
	}

	net, err := vnet.NewNet(&vnet.NetConfig{
		StaticIPs: []string{privateIP},
		StaticIP:  "",
	})
	if err != nil {
		return nil, "", err
	}

	err = m.leftRouter.AddNet(net)
	if err != nil {
		return nil, "", err
	}

	return net, publicIP, nil
}

func (m *NetworkManager) GetRightNet() (*vnet.Net, string, error) {
	privateIP, publicIP, err := m.rightRouter.getIPMapping()
	if err != nil {
		return nil, "", err
	}

	net, err := vnet.NewNet(&vnet.NetConfig{
		StaticIPs: []string{privateIP},
		StaticIP:  "",
	})
	if err != nil {
		return nil, "", err
	}

	err = m.rightRouter.AddNet(net)
	if err != nil {
		return nil, "", err
	}

	return net, publicIP, nil
}

// SetCapacity sets the capacity and maximum burst size for both sides of the network.
func (m *NetworkManager) SetCapacity(capacity, maxBurst int) {
	m.SetLeftCapacity(capacity, maxBurst)
	m.SetRightCapacity(capacity, maxBurst)
}

func (m *NetworkManager) SetLeftCapacity(capacity, maxBurst int) {
	m.leftTBF.Set(vnet.TBFRate(capacity), vnet.TBFMaxBurst(maxBurst))
}

func (m *NetworkManager) SetRightCapacity(capacity, maxBurst int) {
	m.rightTBF.Set(vnet.TBFRate(capacity), vnet.TBFMaxBurst(maxBurst))
}

// SetAckLossRate sets the acknowledgment loss rate.
// Note: This is simplified for standard pion/transport compatibility.
// The original implementation used custom Nuro extensions that aren't available.
func (m *NetworkManager) SetAckLossRate(lossRate int) {
	// Implementation placeholder for standard pion/transport when loss filter API is available.
	// For now, this is a no-op as the standard version doesn't support dynamic loss rate changes.
	_ = lossRate
}

// SetDataLossRate sets the data loss rate.
// Note: This is simplified for standard pion/transport compatibility.
func (m *NetworkManager) SetDataLossRate(lossRate int) {
	// Implementation placeholder for standard pion/transport when loss filter API is available.
	// For now, this is a no-op as the standard version doesn't support dynamic loss rate changes.
	_ = lossRate
}

// SetDataDelay sets the data delay.
// Note: This is simplified for standard pion/transport compatibility.
func (m *NetworkManager) SetDataDelay(delay time.Duration) {
	// Implementation placeholder for standard pion/transport when delay filter API is available.
	// For now, this is a no-op as the standard version doesn't support dynamic delay changes.
	_ = delay
}

// SetAckDelay sets the acknowledgment delay.
// Note: This is simplified for standard pion/transport compatibility.
func (m *NetworkManager) SetAckDelay(delay time.Duration) {
	// Implementation placeholder for standard pion/transport when delay filter API is available.
	// For now, this is a no-op as the standard version doesn't support dynamic delay changes.
	_ = delay
}

// newLeftNet creates and returns a new Net on the left side of the network topology.
func newLeftNet() (*RouterWithConfig, *vnet.TokenBucketFilter, error) {
	routerConfig := &vnet.RouterConfig{
		CIDR: "10.0.1.0/24",
		StaticIPs: []string{
			"10.0.1.1/10.0.1.101",
		},
		LoggerFactory: logging.NewDefaultLoggerFactory(),
		NATType: &vnet.NATType{
			Mode: vnet.NATModeNAT1To1,
		},
	}

	router, err := vnet.NewRouter(routerConfig)
	if err != nil {
		return nil, nil, err
	}

	// Create a simple TBF without loss/delay filters for now
	// This maintains compatibility with standard pion/transport
	tbf, err := vnet.NewTokenBucketFilter(
		router,
		vnet.TBFRate(initCapacity),
		vnet.TBFMaxBurst(initMaxBurst),
	)
	if err != nil {
		return nil, nil, err
	}

	routerWithConfig := &RouterWithConfig{
		Router:       router,
		RouterConfig: routerConfig,
	}

	return routerWithConfig, tbf, nil
}

// newRightNet creates and returns a new Net on the right side of the network topology.
func newRightNet() (*RouterWithConfig, *vnet.TokenBucketFilter, error) {
	routerConfig := &vnet.RouterConfig{
		CIDR: "10.0.2.0/24",
		StaticIPs: []string{
			"10.0.2.1/10.0.2.101",
		},
		LoggerFactory: logging.NewDefaultLoggerFactory(),
		NATType: &vnet.NATType{
			Mode: vnet.NATModeNAT1To1,
		},
	}

	router, err := vnet.NewRouter(routerConfig)
	if err != nil {
		return nil, nil, err
	}

	// Create a simple TBF without loss/delay filters for now
	// This maintains compatibility with standard pion/transport
	tbf, err := vnet.NewTokenBucketFilter(
		router,
		vnet.TBFRate(initCapacity),
		vnet.TBFMaxBurst(initMaxBurst),
	)
	if err != nil {
		return nil, nil, err
	}

	routerWithConfig := &RouterWithConfig{
		Router:       router,
		RouterConfig: routerConfig,
	}

	return routerWithConfig, tbf, nil
}

// NetworkManagerOption is a function that configures a NetworkManager.
type NetworkManagerOption func(*NetworkManagerSettings)

// NetworkManagerSettings holds configuration for NetworkManager.
type NetworkManagerSettings struct {
	dataLossRate  int
	ackLossRate   int
	dataDelay     time.Duration
	ackDelay      time.Duration
	leftCapacity  int
	rightCapacity int
	maxBurst      int
}

// WithDataLossRate sets the data loss rate (client -> server).
func WithDataLossRate(lossRate int) NetworkManagerOption {
	return func(settings *NetworkManagerSettings) {
		settings.dataLossRate = lossRate
	}
}

// WithAckLossRate sets the ack loss rate (server -> client).
func WithAckLossRate(lossRate int) NetworkManagerOption {
	return func(settings *NetworkManagerSettings) {
		settings.ackLossRate = lossRate
	}
}

// WithDataDelay sets the data delay (client -> server).
func WithDataDelay(delay time.Duration) NetworkManagerOption {
	return func(settings *NetworkManagerSettings) {
		settings.dataDelay = delay
	}
}

// WithAckDelay sets the ack delay (server -> client).
func WithAckDelay(delay time.Duration) NetworkManagerOption {
	return func(settings *NetworkManagerSettings) {
		settings.ackDelay = delay
	}
}

// WithLeftCapacity sets the left side capacity.
func WithLeftCapacity(capacity int) NetworkManagerOption {
	return func(settings *NetworkManagerSettings) {
		settings.leftCapacity = capacity
	}
}

// WithRightCapacity sets the right side capacity.
func WithRightCapacity(capacity int) NetworkManagerOption {
	return func(settings *NetworkManagerSettings) {
		settings.rightCapacity = capacity
	}
}

// WithMaxBurst sets the maximum burst size.
func WithMaxBurst(maxBurst int) NetworkManagerOption {
	return func(settings *NetworkManagerSettings) {
		settings.maxBurst = maxBurst
	}
}

// NewManagerWithOptions creates a new NetworkManager with options.
// This is the new preferred way to create a NetworkManager with custom settings.
func NewManagerWithOptions(options ...NetworkManagerOption) (*NetworkManager, error) {
	// Apply default settings
	settings := NetworkManagerSettings{
		leftCapacity:  int(initCapacity),
		rightCapacity: int(initCapacity),
		maxBurst:      int(initMaxBurst),
	}

	// Apply options
	for _, option := range options {
		option(&settings)
	}

	// Create manager with existing NewManager function
	manager, err := NewManager()
	if err != nil {
		return nil, err
	}

	// Apply the configured settings using helper function
	applyManagerSettings(manager, settings)

	return manager, nil
}

// applyManagerSettings applies the settings to a NetworkManager.
func applyManagerSettings(manager *NetworkManager, settings NetworkManagerSettings) {
	if settings.leftCapacity != int(initCapacity) || settings.maxBurst != int(initMaxBurst) {
		manager.SetLeftCapacity(settings.leftCapacity, settings.maxBurst)
	}
	if settings.rightCapacity != int(initCapacity) || settings.maxBurst != int(initMaxBurst) {
		manager.SetRightCapacity(settings.rightCapacity, settings.maxBurst)
	}
	if settings.dataLossRate > 0 {
		manager.SetDataLossRate(settings.dataLossRate)
	}
	if settings.ackLossRate > 0 {
		manager.SetAckLossRate(settings.ackLossRate)
	}
	if settings.dataDelay > 0 {
		manager.SetDataDelay(settings.dataDelay)
	}
	if settings.ackDelay > 0 {
		manager.SetAckDelay(settings.ackDelay)
	}
}
