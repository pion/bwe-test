// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package main implements virtual network functionality for bandwidth estimation tests.
package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/pion/bwe-test/logging"
	"github.com/pion/bwe-test/receiver"
	"github.com/pion/bwe-test/sender"
	plogging "github.com/pion/logging"
)

// Flow represents a WebRTC connection between a sender and receiver over a virtual network.
type Flow struct {
	sender   sndr
	receiver recv
}

// NewSimpleFlow creates a new Flow with the specified parameters.
func NewSimpleFlow(
	loggerFactory plogging.LoggerFactory,
	nm *NetworkManager,
	id int,
	senderMode senderMode,
	dataDir string,
) (Flow, error) {
	snd, err := newSender(loggerFactory, nm, id, senderMode, dataDir)
	if err != nil {
		return Flow{}, fmt.Errorf("new sender: %w", err)
	}

	err = snd.sender.SetupPeerConnection()
	if err != nil {
		return Flow{}, fmt.Errorf("sender setup peer connection: %w", err)
	}

	offer, err := snd.sender.CreateOffer()
	if err != nil {
		return Flow{}, fmt.Errorf("sender create offer: %w", err)
	}

	rc, err := newReceiver(nm, id, dataDir)
	if err != nil {
		return Flow{}, fmt.Errorf("new sender: %w", err)
	}

	err = rc.receiver.SetupPeerConnection()
	if err != nil {
		return Flow{}, fmt.Errorf("receiver setup peer connection: %w", err)
	}

	answer, err := rc.receiver.AcceptOffer(offer)
	if err != nil {
		return Flow{}, fmt.Errorf("receiver accept offer: %w", err)
	}

	err = snd.sender.AcceptAnswer(answer)
	if err != nil {
		return Flow{}, fmt.Errorf("sender accept answer: %w", err)
	}

	return Flow{
		sender:   snd,
		receiver: rc,
	}, nil
}

// Close stops the flow and cleans up all resources.
func (f Flow) Close() error {
	var errs []error
	err := f.receiver.Close()
	if err != nil {
		errs = append(errs, fmt.Errorf("receiver close: %w", err))
	}
	err = f.sender.Close()
	if err != nil {
		errs = append(errs, fmt.Errorf("sender close: %w", err))
	}

	return errors.Join(errs...)
}

var errUnknownSenderMode = errors.New("unknown sender mode")

type sndr struct {
	sender           *sender.Sender
	ccLogger         io.WriteCloser
	senderRTPLogger  io.WriteCloser
	senderRTCPLogger io.WriteCloser
}

func (s sndr) Close() error {
	var errs []error

	err := s.ccLogger.Close()
	if err != nil {
		errs = append(errs, err)
	}

	err = s.senderRTPLogger.Close()
	if err != nil {
		errs = append(errs, err)
	}

	err = s.senderRTCPLogger.Close()
	if err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func newSender(
	loggerFactory plogging.LoggerFactory,
	nm *NetworkManager,
	id int,
	senderMode senderMode,
	dataDir string,
) (sndr, error) {
	leftVnet, publicIPLeft, err := nm.GetLeftNet()
	if err != nil {
		return sndr{}, fmt.Errorf("get left net: %w", err)
	}

	ccLogger, err := logging.GetLogFile(fmt.Sprintf("%v/%v_cc.log", dataDir, id))
	if err != nil {
		return sndr{}, fmt.Errorf("get cc log file: %w", err)
	}

	senderRTPLogger, err := logging.GetLogFile(fmt.Sprintf("%v/%v_sender_rtp.log", dataDir, id))
	if err != nil {
		return sndr{}, fmt.Errorf("get sender rtp log file: %w", err)
	}

	senderRTCPLogger, err := logging.GetLogFile(fmt.Sprintf("%v/%v_sender_rtcp.log", dataDir, id))
	if err != nil {
		return sndr{}, fmt.Errorf("get sender rtcp log file: %w", err)
	}

	var snd *sender.Sender
	switch senderMode {
	case abrSenderMode:
		snd, err = sender.NewSender(
			sender.NewStatisticalEncoderSource(),
			sender.SetVnet(leftVnet, []string{publicIPLeft}),
			sender.PacketLogWriter(senderRTPLogger, senderRTCPLogger),
			sender.GCC(100_000),
			sender.CCLogWriter(ccLogger),
			sender.SetLoggerFactory(loggerFactory),
		)
		if err != nil {
			return sndr{}, fmt.Errorf("new abr sender: %w", err)
		}
	case simulcastSenderMode:
		snd, err = sender.NewSender(
			sender.NewSimulcastFilesSource(),
			sender.SetVnet(leftVnet, []string{publicIPLeft}),
			sender.PacketLogWriter(senderRTPLogger, senderRTCPLogger),
			sender.GCC(100_000),
			sender.CCLogWriter(ccLogger),
			sender.SetLoggerFactory(loggerFactory),
		)
		if err != nil {
			return sndr{}, fmt.Errorf("new simulcast sender: %w", err)
		}
	default:
		return sndr{}, fmt.Errorf("%w: %v", errUnknownSenderMode, senderMode)
	}

	return sndr{
		sender:           snd,
		ccLogger:         ccLogger,
		senderRTPLogger:  senderRTPLogger,
		senderRTCPLogger: senderRTCPLogger,
	}, nil
}

type recv struct {
	receiver           *receiver.Receiver
	receiverRTPLogger  io.WriteCloser
	receiverRTCPLogger io.WriteCloser
}

func (s recv) Close() error {
	var errs []error

	err := s.receiver.Close()
	if err != nil {
		errs = append(errs, err)
	}

	err = s.receiverRTPLogger.Close()
	if err != nil {
		errs = append(errs, err)
	}

	err = s.receiverRTCPLogger.Close()
	if err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func newReceiver(
	nm *NetworkManager,
	id int,
	dataDir string,
) (recv, error) {
	rightVnet, publicIPRight, err := nm.GetRightNet()
	if err != nil {
		return recv{}, fmt.Errorf("get right net: %w", err)
	}

	receiverRTPLogger, err := logging.GetLogFile(fmt.Sprintf("%v/%v_receiver_rtp.log", dataDir, id))
	if err != nil {
		return recv{}, fmt.Errorf("get receiver rtp log file: %w", err)
	}

	receiverRTCPLogger, err := logging.GetLogFile(fmt.Sprintf("%v/%v_receiver_rtcp.log", dataDir, id))
	if err != nil {
		return recv{}, fmt.Errorf("get receiver rtcp log file: %w", err)
	}

	rc, err := receiver.NewReceiver(
		receiver.SetVnet(rightVnet, []string{publicIPRight}),
		receiver.PacketLogWriter(receiverRTPLogger, receiverRTCPLogger),
		receiver.DefaultInterceptors(),
	)
	if err != nil {
		return recv{}, fmt.Errorf("new receiver: %w", err)
	}

	return recv{
		receiver:           rc,
		receiverRTPLogger:  receiverRTPLogger,
		receiverRTCPLogger: receiverRTCPLogger,
	}, nil
}
