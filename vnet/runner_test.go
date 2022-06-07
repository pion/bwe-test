package vnet

import (
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/pion/bwe-test/sender"

	"github.com/pion/bwe-test/receiver"
	"github.com/pion/transport/vnet"
	"github.com/stretchr/testify/assert"
)

type senderMode int

const (
	simulcastSenderMode senderMode = iota
	abrSenderMode
)

func TestVnetRunnerSimulcast(t *testing.T) {
	VnetRunner(t, simulcastSenderMode)
}

func TestVnetRunnerABR(t *testing.T) {
	VnetRunner(t, abrSenderMode)
}

func VnetRunner(t *testing.T, mode senderMode) {
	nm, err := NewManager()
	assert.NoError(t, err)

	leftVnet, publicIPLeft, err := nm.GetLeftNet()
	assert.NoError(t, err)
	rightVnet, publicIPRight, err := nm.GetRightNet()
	assert.NoError(t, err)

	var s *sender.Sender
	switch mode {
	case abrSenderMode:
		s, err = sender.NewSender(
			sender.NewStatisticalEncoderSource(),
			sender.SetVnet(leftVnet, []string{publicIPLeft}),
			sender.RTPLogWriter(io.Discard),
		)
		assert.NoError(t, err)
	case simulcastSenderMode:
		s, err = sender.NewSender(
			sender.NewSimulcastFilesSource(),
			sender.SetVnet(leftVnet, []string{publicIPLeft}),
			sender.RTPLogWriter(io.Discard),
		)
		assert.NoError(t, err)
	default:
		assert.Fail(t, "invalid sender mode", mode)
	}

	r, err := receiver.NewReceiver(
		receiver.Vnet(rightVnet, []string{publicIPRight}),
	)
	assert.NoError(t, err)

	err = s.SetupPeerConnection()
	assert.NoError(t, err)

	offer, err := s.CreateOffer()
	assert.NoError(t, err)

	err = r.SetupPeerConnection()
	assert.NoError(t, err)

	answer, err := r.AcceptOffer(offer)
	assert.NoError(t, err)

	err = s.AcceptAnswer(answer)
	assert.NoError(t, err)

	go func() {
		err = s.Start()
		assert.NoError(t, err)
	}()

	referenceCapacity := 2 * vnet.MBit
	referenceMaxBurst := 160 * vnet.KBit
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

	err = r.Close()
	assert.NoError(t, err)

	err = s.Close()
	assert.NoError(t, err)
}
