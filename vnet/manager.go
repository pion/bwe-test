package vnet

import (
	"fmt"
	"strings"

	"github.com/pion/logging"
	"github.com/pion/transport/vnet"
)

type RouterWithConfig struct {
	*vnet.RouterConfig
	*vnet.Router
	usedIPs map[string]bool
}

func (r *RouterWithConfig) getIPMapping() (private, public string, err error) {
	if len(r.usedIPs) >= len(r.StaticIPs) {
		return "", "", fmt.Errorf("no IP available")
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

type NetworkManager struct {
	leftRouter  *RouterWithConfig
	leftTBF     *vnet.TokenBucketFilter
	rightRouter *RouterWithConfig
	rightTBF    *vnet.TokenBucketFilter
}

const (
	initCapacity = 1 * vnet.MBit
	initMaxBurst = 80 * vnet.KBit
)

func NewManager() (*NetworkManager, error) {
	wan, err := vnet.NewRouter(&vnet.RouterConfig{
		CIDR:          "0.0.0.0/0",
		LoggerFactory: logging.NewDefaultLoggerFactory(),
	})
	if err != nil {
		return nil, err
	}

	leftRouterConfig := &vnet.RouterConfig{
		CIDR: "10.0.1.0/24",
		StaticIPs: []string{
			"10.0.1.1/10.0.1.101",
		},
		LoggerFactory: logging.NewDefaultLoggerFactory(),
		NATType: &vnet.NATType{
			Mode: vnet.NATModeNAT1To1,
		},
	}
	leftRouter, err := vnet.NewRouter(leftRouterConfig)
	if err != nil {
		return nil, err
	}

	leftTBF, err := vnet.NewTokenBucketFilter(
		leftRouter,
		vnet.TBFRate(initCapacity),
		vnet.TBFMaxBurst(initMaxBurst),
	)
	if err != nil {
		return nil, err
	}
	err = wan.AddNet(leftTBF)
	if err != nil {
		return nil, err
	}
	err = wan.AddChildRouter(leftRouter)
	if err != nil {
		return nil, err
	}

	rightRouterConfig := &vnet.RouterConfig{
		CIDR: "10.0.2.0/24",
		StaticIPs: []string{
			"10.0.2.1/10.0.2.101",
		},
		LoggerFactory: logging.NewDefaultLoggerFactory(),
		NATType: &vnet.NATType{
			Mode: vnet.NATModeNAT1To1,
		},
	}
	rightRouter, err := vnet.NewRouter(rightRouterConfig)
	if err != nil {
		return nil, err
	}
	rightTBF, err := vnet.NewTokenBucketFilter(rightRouter, vnet.TBFRate(initCapacity), vnet.TBFMaxBurst(initMaxBurst))
	if err != nil {
		return nil, err
	}
	err = wan.AddNet(rightTBF)
	if err != nil {
		return nil, err
	}
	err = wan.AddChildRouter(rightRouter)
	if err != nil {
		return nil, err
	}

	manager := &NetworkManager{
		leftRouter: &RouterWithConfig{
			Router:       leftRouter,
			RouterConfig: leftRouterConfig,
		},
		leftTBF: leftTBF,
		rightRouter: &RouterWithConfig{
			Router:       rightRouter,
			RouterConfig: rightRouterConfig,
		},
		rightTBF: rightTBF,
	}

	if err := wan.Start(); err != nil {
		return nil, err
	}

	return manager, nil
}

func (m *NetworkManager) GetLeftNet() (*vnet.Net, string, error) {
	privateIP, publicIP, err := m.leftRouter.getIPMapping()
	if err != nil {
		return nil, "", err
	}
	net := vnet.NewNet(&vnet.NetConfig{
		StaticIPs: []string{privateIP},
		StaticIP:  "",
	})
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
	net := vnet.NewNet(&vnet.NetConfig{
		StaticIPs: []string{privateIP},
		StaticIP:  "",
	})
	err = m.rightRouter.AddNet(net)
	if err != nil {
		return nil, "", err
	}
	return net, publicIP, nil
}

func (m *NetworkManager) SetCapacity(capacity, maxBurst int) {
	m.leftTBF.Set(vnet.TBFRate(capacity), vnet.TBFMaxBurst(maxBurst))
	m.rightTBF.Set(vnet.TBFRate(capacity), vnet.TBFMaxBurst(maxBurst))
}
