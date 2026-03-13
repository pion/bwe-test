// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

// Package main implements virtual network functionality for bandwidth estimation tests.
package main

import (
	"errors"
	"strings"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v4/vnet"
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
	leftNetComponents  *NetworkComponents
	rightNetComponents *NetworkComponents
}

type NetworkComponents struct {
	routerWithConfig *RouterWithConfig
	tbfQueue         *vnet.TBFQueue
	queue            *vnet.Queue
	lossFilter       *vnet.LossFilter
	delayFilter      *vnet.DelayFilter
}

const (
	// Bit rate constants (removed from transport/v4).
	KBit = 1000
	MBit = 1000 * KBit

	initCapacity = 1 * MBit
	initMaxBurst = 80 * KBit
)

// NewManager creates a new NetworkManager with default configuration.
func NewManager() (*NetworkManager, error) {
	wan, err := vnet.NewRouter(&vnet.RouterConfig{
		CIDR:          "0.0.0.0/0",
		LoggerFactory: logging.NewDefaultLoggerFactory(),
		Name:          "wan",
	})
	if err != nil {
		return nil, err
	}

	leftNetComponents, err := newLeftNet()
	if err != nil {
		return nil, err
	}

	err = wan.AddNet(leftNetComponents.delayFilter)
	if err != nil {
		return nil, err
	}
	err = wan.AddChildRouter(leftNetComponents.routerWithConfig.Router)
	if err != nil {
		return nil, err
	}

	rightNetComponents, err := newRightNet()
	if err != nil {
		return nil, err
	}

	err = wan.AddNet(rightNetComponents.delayFilter)
	if err != nil {
		return nil, err
	}
	err = wan.AddChildRouter(rightNetComponents.routerWithConfig.Router)
	if err != nil {
		return nil, err
	}

	manager := &NetworkManager{
		leftNetComponents:  leftNetComponents,
		rightNetComponents: rightNetComponents,
	}

	if err := wan.Start(); err != nil {
		return nil, err
	}

	return manager, nil
}

func (m *NetworkManager) GetLeftNet() (*vnet.Net, string, error) {
	privateIP, publicIP, err := m.leftNetComponents.routerWithConfig.getIPMapping()
	if err != nil {
		return nil, "", err
	}

	net, err := vnet.NewNet(&vnet.NetConfig{
		StaticIPs: []string{privateIP},
	})
	if err != nil {
		return nil, "", err
	}

	err = m.leftNetComponents.routerWithConfig.AddNet(net)
	if err != nil {
		return nil, "", err
	}

	return net, publicIP, nil
}

func (m *NetworkManager) GetRightNet() (*vnet.Net, string, error) {
	privateIP, publicIP, err := m.rightNetComponents.routerWithConfig.getIPMapping()
	if err != nil {
		return nil, "", err
	}

	net, err := vnet.NewNet(&vnet.NetConfig{
		StaticIPs: []string{privateIP},
	})
	if err != nil {
		return nil, "", err
	}

	err = m.rightNetComponents.routerWithConfig.AddNet(net)
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
	m.leftNetComponents.tbfQueue.SetRate(capacity)
	m.leftNetComponents.tbfQueue.SetBurst(maxBurst)
}

func (m *NetworkManager) SetRightCapacity(capacity, maxBurst int) {
	m.rightNetComponents.tbfQueue.SetRate(capacity)
	m.rightNetComponents.tbfQueue.SetBurst(maxBurst)
}

func (m *NetworkManager) SetAckLossRate(lossRate int) {
	_ = m.leftNetComponents.lossFilter.SetLossRate(lossRate, true)
}

func (m *NetworkManager) SetDataLossRate(lossRate int) {
	_ = m.rightNetComponents.lossFilter.SetLossRate(lossRate, false)
}

func (m *NetworkManager) SetDataDelay(delay time.Duration) {
	m.rightNetComponents.delayFilter.SetDelay(delay)
}

func (m *NetworkManager) SetAckDelay(delay time.Duration) {
	m.leftNetComponents.delayFilter.SetDelay(delay)
}

// newLeftNet creates and returns a new Net on the left side of the network topology.
// Packets that enter the leftNet pass through AckDelayFilter -> AckLossFilter -> LeftTBF -> leftNet.
func newLeftNet() (*NetworkComponents, error) {
	routerConfig := &vnet.RouterConfig{
		CIDR: "10.0.1.0/24",
		StaticIPs: []string{
			"10.0.1.1/10.0.1.101",
		},
		LoggerFactory: logging.NewDefaultLoggerFactory(),
		NATType: &vnet.NATType{
			Mode: vnet.NATModeNAT1To1,
		},
		MinDelay: 0 * time.Millisecond,
		Name:     "left",
	}

	router, err := vnet.NewRouter(routerConfig)
	if err != nil {
		return nil, err
	}

	tbfQueue := vnet.NewTBFQueue(initCapacity, initMaxBurst, 1024*1024)
	queue, err := vnet.NewQueue(router, tbfQueue)
	if err != nil {
		return nil, err
	}

	lossFilter, err := vnet.NewLossFilter(queue, 0)
	if err != nil {
		return nil, err
	}

	delayFilter, err := vnet.NewDelayFilter(lossFilter)
	if err != nil {
		return nil, err
	}

	routerWithConfig := &RouterWithConfig{
		Router:       router,
		RouterConfig: routerConfig,
	}

	return &NetworkComponents{
		routerWithConfig: routerWithConfig,
		tbfQueue:         tbfQueue,
		queue:            queue,
		lossFilter:       lossFilter,
		delayFilter:      delayFilter,
	}, nil
}

// newRightNet creates and returns a new Net on the right side of the network topology.
// Packets that enter the rightNet pass through DataDelayFilter -> DataLossFilter -> RightTBF -> rightNet.
func newRightNet() (*NetworkComponents, error) {
	routerConfig := &vnet.RouterConfig{
		CIDR: "10.0.2.0/24",
		StaticIPs: []string{
			"10.0.2.1/10.0.2.101",
		},
		LoggerFactory: logging.NewDefaultLoggerFactory(),
		NATType: &vnet.NATType{
			Mode: vnet.NATModeNAT1To1,
		},
		Name: "right",
	}

	router, err := vnet.NewRouter(routerConfig)
	if err != nil {
		return nil, err
	}

	tbfQueue := vnet.NewTBFQueue(initCapacity, initMaxBurst, 1024*1024)
	queue, err := vnet.NewQueue(router, tbfQueue)
	if err != nil {
		return nil, err
	}

	lossFilter, err := vnet.NewLossFilter(queue, 0)
	if err != nil {
		return nil, err
	}

	delayFilter, err := vnet.NewDelayFilter(lossFilter)
	if err != nil {
		return nil, err
	}

	routerWithConfig := &RouterWithConfig{
		Router:       router,
		RouterConfig: routerConfig,
	}

	return &NetworkComponents{
		routerWithConfig: routerWithConfig,
		tbfQueue:         tbfQueue,
		queue:            queue,
		lossFilter:       lossFilter,
		delayFilter:      delayFilter,
	}, nil
}
