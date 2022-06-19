package vnet

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/pion/bwe-test/logging"
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

	err = os.MkdirAll(fmt.Sprintf("data/%v", t.Name()), os.ModePerm)
	assert.NoError(t, err)

	leftVnet, publicIPLeft, err := nm.GetLeftNet()
	assert.NoError(t, err)
	rightVnet, publicIPRight, err := nm.GetRightNet()
	assert.NoError(t, err)

	senderRTPLogger, err := logging.GetLogFile(fmt.Sprintf("data/%v/sender_rtp.log", t.Name()))
	assert.NoError(t, err)
	defer func() { assert.NoError(t, senderRTPLogger.Close()) }()
	senderRTCPLogger, err := logging.GetLogFile(fmt.Sprintf("data/%v/sender_rtcp.log", t.Name()))
	assert.NoError(t, err)
	defer func() { assert.NoError(t, senderRTCPLogger.Close()) }()
	ccLogger, err := logging.GetLogFile(fmt.Sprintf("data/%v/cc.log", t.Name()))
	assert.NoError(t, err)
	defer func() { assert.NoError(t, ccLogger.Close()) }()

	var s *sender.Sender
	switch mode {
	case abrSenderMode:
		s, err = sender.NewSender(
			sender.NewStatisticalEncoderSource(),
			sender.SetVnet(leftVnet, []string{publicIPLeft}),
			sender.PacketLogWriter(senderRTPLogger, senderRTCPLogger),
			sender.GCC(100_000),
			sender.CCLogWriter(ccLogger),
		)
		assert.NoError(t, err)
	case simulcastSenderMode:
		s, err = sender.NewSender(
			sender.NewSimulcastFilesSource(),
			sender.SetVnet(leftVnet, []string{publicIPLeft}),
			sender.PacketLogWriter(senderRTPLogger, senderRTCPLogger),
			sender.GCC(100_000),
			sender.CCLogWriter(ccLogger),
		)
		assert.NoError(t, err)
	default:
		assert.Fail(t, "invalid sender mode", mode)
	}

	receiverRTPLogger, err := logging.GetLogFile(fmt.Sprintf("data/%v/receiver_rtp.log", t.Name()))
	assert.NoError(t, err)
	defer func() { assert.NoError(t, receiverRTPLogger.Close()) }()
	receiverRTCPLogger, err := logging.GetLogFile(fmt.Sprintf("data/%v/receiver_rtcp.log", t.Name()))
	assert.NoError(t, err)
	defer func() { assert.NoError(t, receiverRTCPLogger.Close()) }()

	r, err := receiver.NewReceiver(
		receiver.SetVnet(rightVnet, []string{publicIPRight}),
		receiver.PacketLogWriter(receiverRTPLogger, receiverRTCPLogger),
		receiver.DefaultInterceptors(),
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err = s.Start(ctx)
		assert.NoError(t, err)
	}()

	referenceCapacity := 1 * vnet.MBit
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
}
