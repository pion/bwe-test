// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

// Package main implements virtual network functionality for bandwidth estimation tests.
package main

import (
	"errors"
	"strings"

	"github.com/pion/logging"
	"github.com/pion/transport/v3/vnet"
)

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

	return
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

func (m *NetworkManager) Close() error {
	m.leftTBF.Close()
	m.rightTBF.Close()
	m.wan.Stop()
	return nil
}

// GetLeftNet creates and returns a new Net on the left side of the network topology.
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

// GetRightNet creates and returns a new Net on the right side of the network topology.
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
	m.leftTBF.Set(vnet.TBFRate(capacity), vnet.TBFMaxBurst(maxBurst))
	m.rightTBF.Set(vnet.TBFRate(capacity), vnet.TBFMaxBurst(maxBurst))
}

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
