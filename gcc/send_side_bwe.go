// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package gcc

import (
	"time"

	"github.com/pion/logging"
)

type Option func(*SendSideController) error

func Logger(l logging.LeveledLogger) Option {
	return func(ssc *SendSideController) error {
		ssc.log = l
		ssc.drc.log = l
		return nil
	}
}

type SendSideController struct {
	log          logging.LeveledLogger
	dre          *deliveryRateEstimator
	lbc          *lossRateController
	drc          *delayRateController
	rate         int
	highestAcked uint64
}

func NewSendSideController(initialRate, minRate, maxRate int, opts ...Option) (*SendSideController, error) {
	ssc := &SendSideController{
		log:  logging.NewDefaultLoggerFactory().NewLogger("bwe_send_side_controller"),
		dre:  newDeliveryRateEstimator(time.Second),
		lbc:  newLossRateController(initialRate, minRate, maxRate),
		drc:  newDelayRateController(initialRate, logging.NewDefaultLoggerFactory().NewLogger("bwe_delay_rate_controller")),
		rate: initialRate,
	}
	for _, opt := range opts {
		if err := opt(ssc); err != nil {
			return nil, err
		}
	}
	return ssc, nil
}

func (c *SendSideController) OnAcks(arrival time.Time, rtt time.Duration, acks []Acknowledgment) int {
	if len(acks) == 0 {
		return c.rate
	}

	for _, ack := range acks {
		if ack.SeqNr < c.highestAcked {
			continue
		}
		if ack.Arrived {
			if ack.SeqNr > c.highestAcked {
				c.highestAcked = ack.SeqNr
			}
			c.lbc.onPacketAcked()
			if !ack.Arrival.IsZero() {
				c.dre.onPacketAcked(ack.Arrival, int(ack.Size))
				c.drc.onPacketAcked(ack)
			}
		} else {
			c.lbc.onPacketLost()
		}
	}

	delivered := c.dre.getRate()
	lossTarget := c.lbc.update(delivered)
	delayTarget := c.drc.update(arrival, delivered, rtt)
	c.rate = min(lossTarget, delayTarget)
	c.log.Tracef("rtt=%v, delivered=%v, lossTarget=%v, delayTarget=%v, target=%v", rtt.Nanoseconds(), delivered, lossTarget, delayTarget, c.rate)
	return c.rate
}
