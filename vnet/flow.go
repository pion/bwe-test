// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"io"

	plogging "github.com/pion/logging"

	"github.com/pion/bwe-test/logging"
	"github.com/pion/bwe-test/receiver"
	"github.com/pion/bwe-test/sender"
)

type Flow struct {
	sender             *sender.Sender
	receiver           *receiver.Receiver
	senderRTPLogger    io.WriteCloser
	senderRTCPLogger   io.WriteCloser
	ccLogger           io.WriteCloser
	receiverRTPLogger  io.WriteCloser
	receiverRTCPLogger io.WriteCloser
}

func NewSimpleFlow(
	loggerFactory plogging.LoggerFactory,
	nm *NetworkManager,
	id int,
	senderMode senderMode,
	dataDir string,
) (Flow, error) {
	leftVnet, publicIPLeft, err := nm.GetLeftNet()
	if err != nil {
		return Flow{}, fmt.Errorf("get left net: %w", err)
	}

	rightVnet, publicIPRight, err := nm.GetRightNet()
	if err != nil {
		return Flow{}, fmt.Errorf("get right net: %w", err)
	}

	senderRTPLogger, err := logging.GetLogFile(fmt.Sprintf("%v/%v_sender_rtp.log", dataDir, id))
	if err != nil {
		return Flow{}, fmt.Errorf("get sender rtp log file: %w", err)
	}

	senderRTCPLogger, err := logging.GetLogFile(fmt.Sprintf("%v/%v_sender_rtcp.log", dataDir, id))
	if err != nil {
		return Flow{}, fmt.Errorf("get sender rtcp log file: %w", err)
	}

	ccLogger, err := logging.GetLogFile(fmt.Sprintf("%v/%v_cc.log", dataDir, id))
	if err != nil {
		return Flow{}, fmt.Errorf("get cc log file: %w", err)
	}

	var s *sender.Sender
	switch senderMode {
	case abrSenderMode:
		s, err = sender.NewSender(
			sender.NewStatisticalEncoderSource(),
			sender.SetVnet(leftVnet, []string{publicIPLeft}),
			sender.PacketLogWriter(senderRTPLogger, senderRTCPLogger),
			sender.GCC(100_000),
			sender.CCLogWriter(ccLogger),
			sender.SetLoggerFactory(loggerFactory),
		)
		if err != nil {
			return Flow{}, fmt.Errorf("new abr sender: %w", err)
		}
	case simulcastSenderMode:
		s, err = sender.NewSender(
			sender.NewSimulcastFilesSource(),
			sender.SetVnet(leftVnet, []string{publicIPLeft}),
			sender.PacketLogWriter(senderRTPLogger, senderRTCPLogger),
			sender.GCC(100_000),
			sender.CCLogWriter(ccLogger),
			sender.SetLoggerFactory(loggerFactory),
		)
		if err != nil {
			return Flow{}, fmt.Errorf("new simulcast sender: %w", err)
		}
	default:
		return Flow{}, fmt.Errorf("invalid sender mode: %v", senderMode)
	}

	receiverRTPLogger, err := logging.GetLogFile(fmt.Sprintf("%v/%v_receiver_rtp.log", dataDir, id))
	if err != nil {
		return Flow{}, fmt.Errorf("get receiver rtp log file: %w", err)
	}

	receiverRTCPLogger, err := logging.GetLogFile(fmt.Sprintf("%v/%v_receiver_rtcp.log", dataDir, id))
	if err != nil {
		return Flow{}, fmt.Errorf("get receiver rtcp log file: %w", err)
	}

	rc, err := receiver.NewReceiver(
		receiver.SetVnet(rightVnet, []string{publicIPRight}),
		receiver.PacketLogWriter(receiverRTPLogger, receiverRTCPLogger),
		receiver.DefaultInterceptors(),
	)
	if err != nil {
		return Flow{}, fmt.Errorf("new receiver: %w", err)
	}

	err = s.SetupPeerConnection()
	if err != nil {
		return Flow{}, fmt.Errorf("sender setup peer connection: %w", err)
	}

	offer, err := s.CreateOffer()
	if err != nil {
		return Flow{}, fmt.Errorf("sender create offer: %w", err)
	}

	err = rc.SetupPeerConnection()
	if err != nil {
		return Flow{}, fmt.Errorf("receiver setup peer connection: %w", err)
	}

	answer, err := rc.AcceptOffer(offer)
	if err != nil {
		return Flow{}, fmt.Errorf("receiver accept offer: %w", err)
	}

	err = s.AcceptAnswer(answer)
	if err != nil {
		return Flow{}, fmt.Errorf("sender accept answer: %w", err)
	}

	return Flow{
		sender:             s,
		receiver:           rc,
		senderRTPLogger:    senderRTPLogger,
		senderRTCPLogger:   senderRTCPLogger,
		ccLogger:           ccLogger,
		receiverRTPLogger:  receiverRTPLogger,
		receiverRTCPLogger: receiverRTCPLogger,
	}, nil
}

func (f Flow) Close() error {
	var errs []error
	err := f.receiver.Close()
	if err != nil {
		errs = append(errs, fmt.Errorf("receiver close: %w", err))
	}
	err = f.senderRTPLogger.Close()
	if err != nil {
		errs = append(errs, fmt.Errorf("sender rtp logger close: %w", err))
	}
	err = f.senderRTCPLogger.Close()
	if err != nil {
		errs = append(errs, fmt.Errorf("sender rtcp logger close: %w", err))
	}
	err = f.ccLogger.Close()
	if err != nil {
		errs = append(errs, fmt.Errorf("cc logger close: %w", err))
	}
	err = f.receiverRTPLogger.Close()
	if err != nil {
		errs = append(errs, fmt.Errorf("receiver rtp logger close: %w", err))
	}
	err = f.receiverRTCPLogger.Close()
	if err != nil {
		errs = append(errs, fmt.Errorf("receiver rtcp logger close: %w", err))
	}
	return errors.Join(errs...)
}
