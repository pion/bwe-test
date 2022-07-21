package vnet

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/pion/bwe-test/logging"
	"github.com/pion/bwe-test/sender"

	"github.com/pion/bwe-test/receiver"
	"github.com/pion/transport/vnet"
	"github.com/stretchr/testify/assert"
)

type senderMode int

func (m senderMode) String() string {
	switch m {
	case simulcastSenderMode:
		return "Simulcast"
	case abrSenderMode:
		return "ABR"
	default:
		return "invalid senderMode"
	}
}

const (
	simulcastSenderMode senderMode = iota
	abrSenderMode
)

type congestionController int

func (c congestionController) String() string {
	switch c {
	case gcc:
		return "GCC"
	case nada:
		return "NADA"
	default:
		return "invalid congestion controller"
	}
}

const (
	gcc congestionController = iota
	nada
)

type feedbackMode int

const (
	TWCC feedbackMode = iota
	RFC8888
)

func (f feedbackMode) String() string {
	switch f {
	case TWCC:
		return "TWCC"
	case RFC8888:
		return "RFC8888"
	default:
		return "invalid feedback mode"
	}
}

const (
	initTargetBitrate = 700_000
	minTargetBitrate  = 100_000
	maxTargetBitrate  = 50_000_000
)

type senderReceiverPair struct {
	sender     *vnet.Net
	senderIP   string
	receiver   *vnet.Net
	receiverIP string
}

type config struct {
	mode senderMode
	cc   congestionController
	fb   feedbackMode
}

func TestVnet(t *testing.T) {
	configs := []config{
		{
			mode: simulcastSenderMode,
			cc:   gcc,
			fb:   TWCC,
		},
		{
			mode: abrSenderMode,
			cc:   gcc,
			fb:   TWCC,
		},
		{
			mode: simulcastSenderMode,
			cc:   nada,
			fb:   TWCC,
		},
		{
			mode: abrSenderMode,
			cc:   nada,
			fb:   TWCC,
		},
		{
			mode: simulcastSenderMode,
			cc:   gcc,
			fb:   RFC8888,
		},
		{
			mode: abrSenderMode,
			cc:   gcc,
			fb:   RFC8888,
		},
		{
			mode: simulcastSenderMode,
			cc:   nada,
			fb:   RFC8888,
		},
		{
			mode: abrSenderMode,
			cc:   nada,
			fb:   RFC8888,
		},
	}
	for _, config := range configs {
		VnetRunner(t, config)
	}
}

func VnetRunner(t *testing.T, c config) {
	t.Run(fmt.Sprintf("%v/VariableAvailableCapacitySingleFlow/%v/%v", c.mode, c.cc, c.fb), func(t *testing.T) {
		nm, err := NewManager()
		assert.NoError(t, err)

		err = os.MkdirAll(fmt.Sprintf("data/%v", t.Name()), os.ModePerm)
		assert.NoError(t, err)

		leftVnet, publicIPLeft, err := nm.GetLeftNet()
		assert.NoError(t, err)
		rightVnet, publicIPRight, err := nm.GetRightNet()
		assert.NoError(t, err)
		srp := senderReceiverPair{
			sender:     leftVnet,
			senderIP:   publicIPLeft,
			receiver:   rightVnet,
			receiverIP: publicIPRight,
		}
		s, r, teardown := setupSimpleFlow(t, 0, srp, c)
		defer teardown()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var wg sync.WaitGroup

		wg.Add(1)
		go func() {
			defer wg.Done()
			err = s.Start(ctx)
			assert.NoError(t, err)
		}()
		defer func() {
			err = r.Close()
			assert.NoError(t, err)
		}()

		c := pathCharacteristics{
			referenceCapacity: 1 * vnet.MBit,
			phases: []phase{
				{
					duration:      40 * time.Second,
					capacityRatio: 1.0,
					maxBurst:      160 * vnet.KBit,
				},
				{
					duration:      20 * time.Second,
					capacityRatio: 2.5,
					maxBurst:      160 * vnet.KBit,
				},
				{
					duration:      20 * time.Second,
					capacityRatio: 0.6,
					maxBurst:      160 * vnet.KBit,
				},
				{
					duration:      20 * time.Second,
					capacityRatio: 1.0,
					maxBurst:      160 * vnet.KBit,
				},
			},
		}

		lastRateCh := make(chan int)

		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(time.Second)
			//bytesSent := uint64(0)
			bytesReceived := uint64(0)
			lastLog := time.Now()
			currentLimit := 0
			for {
				select {
				case <-ctx.Done():
					return
				case currentLimit = <-lastRateCh:
				case now := <-ticker.C:

					target := s.GetTargetBitrate()
					statsMap := s.GetStats()

					assert.GreaterOrEqual(t, target, minTargetBitrate)
					assert.LessOrEqual(t, target, maxTargetBitrate)
					assert.LessOrEqualf(t, target, int(1.3*float64(currentLimit)), "target rate (%v) too large for limit (%v)", target, currentLimit)

					assert.Equal(t, 1, len(statsMap))
					// Only works if there's only 1 track:

					dx := time.Since(lastLog)
					for _, stats := range statsMap {

						assert.LessOrEqualf(t, stats.FractionLost, 0.15, "receiver experienced high loss (%v)", stats.FractionLost)

						//dy := 8 * (stats.OutboundRTPStreamStats.BytesSent - bytesSent)
						//rate := int(float64(dy) / dx.Seconds())

						//assert.InDeltaf(t, target, rate, float64(target)*0.3, "actual send rate (%v) deviates too much from target (%v)", rate, target)
						//fmt.Printf("[sender] ts:%v, rtt:%v, target:%v, actual:%v\n", now.UnixMilli(), stats.RemoteInboundRTPStreamStats.RoundTripTime, target, int(rate))

						lastLog = now
						//bytesSent = stats.OutboundRTPStreamStats.BytesSent
					}

					statsMap = r.GetStats()
					for _, stats := range statsMap {

						dy := 8 * (stats.BytesReceived - bytesReceived)
						rate := int(float64(dy) / dx.Seconds())

						assert.LessOrEqualf(t, target, int(1.3*float64(rate)), "target (%v) deviates too much actual receive rate (1.3 * %v = %v)", target, rate, int(1.3*float64(rate)))
						//fmt.Printf("[receiver] ts:%v, rate:%v\n", now.UnixMilli(), int(rate))

						bytesReceived = stats.BytesReceived
					}
				}
			}
		}()

		capacityLogger, err := logging.GetLogFile(fmt.Sprintf("data/%v/capacity.log", t.Name()))
		assert.NoError(t, err)
		defer func() {
			assert.NoError(t, capacityLogger.Close())
		}()

		var now time.Time
		var rate int
		for _, phase := range c.phases {
			now = time.Now()
			rate = int(float64(c.referenceCapacity) * phase.capacityRatio)
			nm.SetCapacity(
				rate,
				phase.maxBurst,
			)
			fmt.Fprintf(capacityLogger, "%v, %v\n", now.UnixMilli(), rate)
			lastRateCh <- rate
			time.Sleep(phase.duration)
		}
		fmt.Fprintf(capacityLogger, "%v, %v\n", now.UnixMilli(), rate)
	})

	t.Run(fmt.Sprintf("%v/VariableAvailableCapacityMultipleFlows/%v/%v", c.mode, c.cc, c.fb), func(t *testing.T) {
		nm, err := NewManager()
		assert.NoError(t, err)

		err = os.MkdirAll(fmt.Sprintf("data/%v", t.Name()), os.ModePerm)
		assert.NoError(t, err)

		for i := 0; i < 2; i++ {
			leftVnet, publicIPLeft, err := nm.GetLeftNet()
			assert.NoError(t, err)
			rightVnet, publicIPRight, err := nm.GetRightNet()
			assert.NoError(t, err)
			srp := senderReceiverPair{
				sender:     leftVnet,
				senderIP:   publicIPLeft,
				receiver:   rightVnet,
				receiverIP: publicIPRight,
			}
			s, r, teardown := setupSimpleFlow(t, i, srp, config{
				mode: c.mode,
				cc:   c.cc,
			})
			defer teardown()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				err = s.Start(ctx)
				assert.NoError(t, err)
			}()

			defer func() {
				err = r.Close()
				assert.NoError(t, err)
			}()
		}

		c := pathCharacteristics{
			referenceCapacity: 1 * vnet.MBit,
			phases: []phase{
				{
					duration:      25 * time.Second,
					capacityRatio: 2.0,
					maxBurst:      160 * vnet.KBit,
				},

				{
					duration:      25 * time.Second,
					capacityRatio: 1.0,
					maxBurst:      160 * vnet.KBit,
				},
				{
					duration:      25 * time.Second,
					capacityRatio: 1.75,
					maxBurst:      160 * vnet.KBit,
				},
				{
					duration:      25 * time.Second,
					capacityRatio: 0.5,
					maxBurst:      160 * vnet.KBit,
				},
				{
					duration:      25 * time.Second,
					capacityRatio: 1.0,
					maxBurst:      160 * vnet.KBit,
				},
			},
		}

		capacityLogger, err := logging.GetLogFile(fmt.Sprintf("data/%v/capacity.log", t.Name()))
		assert.NoError(t, err)
		defer func() {
			assert.NoError(t, capacityLogger.Close())
		}()

		var now time.Time
		var rate int
		for _, phase := range c.phases {
			now = time.Now()
			rate = int(float64(c.referenceCapacity) * phase.capacityRatio)
			nm.SetCapacity(
				rate,
				phase.maxBurst,
			)
			fmt.Fprintf(capacityLogger, "%v, %v\n", now.UnixMilli(), rate)
			time.Sleep(phase.duration)
		}
		fmt.Fprintf(capacityLogger, "%v, %v\n", now.UnixMilli(), rate)
	})

	t.Run(fmt.Sprintf("%v/CongestedFeedbackLinkWithBidirectionalMediaFlows/%v/%v", c.mode, c.cc, c.fb), func(t *testing.T) {
	})
}

type pathCharacteristics struct {
	referenceCapacity int
	phases            []phase
}

type phase struct {
	duration      time.Duration
	capacityRatio float64
	maxBurst      int
}

func setupSimpleFlow(t *testing.T, id int, srp senderReceiverPair, c config) (*sender.Sender, *receiver.Receiver, func()) {
	senderOutboundRTPLogger, err := logging.GetLogFile(fmt.Sprintf("data/%v/%v_sender_outbound_rtp.log", t.Name(), id))
	assert.NoError(t, err)
	senderInboundRTCPLogger, err := logging.GetLogFile(fmt.Sprintf("data/%v/%v_sender_inbound_rtcp.log", t.Name(), id))
	assert.NoError(t, err)
	senderOutboundRTCPLogger, err := logging.GetLogFile(fmt.Sprintf("data/%v/%v_sender_outbound_rtcp.log", t.Name(), id))
	assert.NoError(t, err)
	ccLogger, err := logging.GetLogFile(fmt.Sprintf("data/%v/%v_cc.log", t.Name(), id))
	assert.NoError(t, err)

	var s *sender.Sender
	switch c.mode {
	case abrSenderMode:
		options := []sender.Option{
			sender.SetVnet(srp.sender, []string{srp.senderIP}),
			sender.PacketLogWriter(senderOutboundRTPLogger, senderOutboundRTCPLogger, senderInboundRTCPLogger),
			sender.RTCPReports(),
		}
		if c.cc == gcc {
			options = append(options, sender.GCC(initTargetBitrate, minTargetBitrate, maxTargetBitrate, false))
		}
		if c.cc == nada {
			options = append(options, sender.NADA())
		}
		options = append(options, sender.CCLogWriter(ccLogger))
		s, err = sender.New(
			sender.NewStatisticalEncoderSource(),
			options...,
		)
		assert.NoError(t, err)
	case simulcastSenderMode:
		options := []sender.Option{
			sender.SetVnet(srp.sender, []string{srp.senderIP}),
			sender.PacketLogWriter(senderOutboundRTPLogger, senderOutboundRTCPLogger, senderInboundRTCPLogger),
			sender.RTCPReports(),
		}
		if c.cc == gcc {
			options = append(options, sender.GCC(initTargetBitrate, minTargetBitrate, maxTargetBitrate, false))
		}
		if c.cc == nada {
			options = append(options, sender.NADA())
		}
		options = append(options, sender.CCLogWriter(ccLogger))
		s, err = sender.New(
			sender.NewSimulcastFilesSource(),
			options...,
		)
		assert.NoError(t, err)
	default:
		assert.Fail(t, "invalid sender mode", c.mode)
	}

	receiverInboundRTPLogger, err := logging.GetLogFile(fmt.Sprintf("data/%v/%v_receiver_inbound_rtp.log", t.Name(), id))
	assert.NoError(t, err)
	receiverOutboundRTCPLogger, err := logging.GetLogFile(fmt.Sprintf("data/%v/%v_receiver_outbound_rtcp.log", t.Name(), id))
	assert.NoError(t, err)
	receiverInboundRTCPLogger, err := logging.GetLogFile(fmt.Sprintf("data/%v/%v_receiver_inbound_rtcp.log", t.Name(), id))
	assert.NoError(t, err)

	options := []receiver.Option{
		receiver.SetVnet(srp.receiver, []string{srp.receiverIP}),
		receiver.PacketLogWriter(receiverInboundRTPLogger, receiverOutboundRTCPLogger, receiverInboundRTCPLogger),
		receiver.RTCPReports(),
	}
	if c.fb == TWCC {
		options = append(options, receiver.TWCC())
	}
	if c.fb == RFC8888 {
		options = append(options, receiver.RFC8888())
	}
	r, err := receiver.New(options...)
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

	return s, r, func() {
		assert.NoError(t, senderOutboundRTPLogger.Close())
		assert.NoError(t, senderInboundRTCPLogger.Close())
		assert.NoError(t, ccLogger.Close())
		assert.NoError(t, receiverInboundRTPLogger.Close())
		assert.NoError(t, receiverOutboundRTCPLogger.Close())
	}
}
