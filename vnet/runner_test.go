package vnet

import (
	"fmt"
	"testing"
	"time"

	"github.com/pion/bwe-test/abr"
	"github.com/pion/bwe-test/receiver"
	"github.com/pion/bwe-test/simulcast"
	"github.com/pion/transport/vnet"
	"github.com/pion/webrtc/v3"
	"github.com/stretchr/testify/assert"
)

func TestVnetRunnerSimulcast(t *testing.T) {
	sender, err := simulcast.NewSender()
	assert.NoError(t, err)
	receiver := receiver.NewReceiver()
	VnetRunner(t, sender, receiver)
}

func TestVnetRunnerABR(t *testing.T) {
	sender, err := abr.NewSender()
	assert.NoError(t, err)
	receiver := receiver.NewReceiver()
	VnetRunner(t, sender, receiver)
}

type Sender interface {
	SetVnet(*vnet.Net, []string)
	SetupPeerConnection() error
	CreateOffer() (*webrtc.SessionDescription, error)
	AcceptAnswer(*webrtc.SessionDescription) error
	Start() error
	Close() error
}

type Receiver interface {
	SetVnet(*vnet.Net, []string)
	SetupPeerConnection() error
	AcceptOffer(*webrtc.SessionDescription) (*webrtc.SessionDescription, error)
	Close() error
}

func VnetRunner(t *testing.T, sender Sender, receiver Receiver) {
	nm, err := NewManager()
	assert.NoError(t, err)

	leftVnet, publicIPLeft, err := nm.GetLeftNet()
	assert.NoError(t, err)
	sender.SetVnet(leftVnet, []string{publicIPLeft})

	rightVnet, publicIPRight, err := nm.GetRightNet()
	assert.NoError(t, err)
	receiver.SetVnet(rightVnet, []string{publicIPRight})

	err = sender.SetupPeerConnection()
	assert.NoError(t, err)

	offer, err := sender.CreateOffer()
	assert.NoError(t, err)

	err = receiver.SetupPeerConnection()
	assert.NoError(t, err)

	answer, err := receiver.AcceptOffer(offer)
	assert.NoError(t, err)

	err = sender.AcceptAnswer(answer)
	assert.NoError(t, err)

	go func() {
		err = sender.Start()
		assert.NoError(t, err)
	}()

	referenceCapacity := 1 * vnet.MBit
	referenceMaxBurst := 80 * vnet.KBit
	phases := []struct {
		d             time.Duration
		capacityRatio float64
	}{
		{
			d:             40 * time.Second,
			capacityRatio: 1.0,
		},
		{
			d:             20 * time.Second,
			capacityRatio: 2.5,
		},
		{
			d:             20 * time.Second,
			capacityRatio: 0.6,
		},
		{
			d:             20 * time.Second,
			capacityRatio: 1.0,
		},
	}
	for _, phase := range phases {
		fmt.Printf("enter next phase: %v\n", phase)
		nm.SetCapacity(
			int(float64(referenceCapacity)*phase.capacityRatio),
			int(float64(referenceMaxBurst)*phase.capacityRatio),
		)
		time.Sleep(phase.d)
	}

	err = receiver.Close()
	assert.NoError(t, err)

	err = sender.Close()
	assert.NoError(t, err)
}
