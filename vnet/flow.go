// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

// Package main implements virtual network functionality for bandwidth estimation tests.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pion/bwe-test/logging"
	"github.com/pion/bwe-test/receiver"
	"github.com/pion/bwe-test/sender"
	plogging "github.com/pion/logging"
	"github.com/pion/transport/v3/vnet"
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
	videoFiles ...VideoFileInfo,
) (Flow, error) {
	snd, err := newSender(loggerFactory, nm, id, senderMode, dataDir, videoFiles...)
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

	// Create output file path for received video
	outputVideoPath := ""
	if senderMode == videoFileEncoderMode {
		// Create a subdirectory for received videos
		receivedDir := filepath.Join(dataDir, "received_video")
		if mkdirErr := os.MkdirAll(receivedDir, 0o750); mkdirErr != nil {
			return Flow{}, fmt.Errorf("mkdir received_video: %w", mkdirErr)
		}

		// For multiple tracks, we'll create output files for all tracks
		// The receiver will handle multiple tracks automatically
		outputVideoPath = filepath.Join(receivedDir, fmt.Sprintf("received_%d.ivf", id))
	}

	rc, err := newReceiver(nm, id, dataDir, outputVideoPath, loggerFactory)
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

var (
	errUnknownSenderMode = errors.New("unknown sender mode")
	errVideoFileRequired = errors.New("videoFileEncoderMode requires at least one video file")
)

type sndr struct {
	sender           sender.WebRTCSender
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
	videoFiles ...VideoFileInfo,
) (sndr, error) {
	leftVnet, publicIPLeft, err := nm.GetLeftNet()
	if err != nil {
		return sndr{}, fmt.Errorf("get left net: %w", err)
	}

	loggers, err := createSenderLoggers(dataDir, id)
	if err != nil {
		return sndr{}, err
	}

	snd, err := createWebRTCSender(senderMode, leftVnet, publicIPLeft, loggers, loggerFactory, videoFiles...)
	if err != nil {
		return sndr{}, err
	}

	return sndr{
		sender:           snd,
		ccLogger:         loggers.ccLogger,
		senderRTPLogger:  loggers.rtpLogger,
		senderRTCPLogger: loggers.rtcpLogger,
	}, nil
}

type senderLoggers struct {
	ccLogger   io.WriteCloser
	rtpLogger  io.WriteCloser
	rtcpLogger io.WriteCloser
}

func createSenderLoggers(dataDir string, id int) (senderLoggers, error) {
	ccLogger, err := logging.GetLogFile(fmt.Sprintf("%v/%v_cc.log", dataDir, id))
	if err != nil {
		return senderLoggers{}, fmt.Errorf("get cc log file: %w", err)
	}

	senderRTPLogger, err := logging.GetLogFile(fmt.Sprintf("%v/%v_sender_rtp.log", dataDir, id))
	if err != nil {
		return senderLoggers{}, fmt.Errorf("get sender rtp log file: %w", err)
	}

	senderRTCPLogger, err := logging.GetLogFile(fmt.Sprintf("%v/%v_sender_rtcp.log", dataDir, id))
	if err != nil {
		return senderLoggers{}, fmt.Errorf("get sender rtcp log file: %w", err)
	}

	return senderLoggers{
		ccLogger:   ccLogger,
		rtpLogger:  senderRTPLogger,
		rtcpLogger: senderRTCPLogger,
	}, nil
}

func createWebRTCSender(
	senderMode senderMode,
	leftVnet *vnet.Net,
	publicIPLeft string,
	loggers senderLoggers,
	loggerFactory plogging.LoggerFactory,
	videoFiles ...VideoFileInfo,
) (sender.WebRTCSender, error) {
	commonOpts := []sender.Option{
		sender.SetVnet(leftVnet, []string{publicIPLeft}),
		sender.PacketLogWriter(loggers.rtpLogger, loggers.rtcpLogger),
		sender.GCC(100_000),
		sender.CCLogWriter(loggers.ccLogger),
		sender.SetLoggerFactory(loggerFactory),
	}

	switch senderMode {
	case abrSenderMode:
		return sender.NewSender(
			sender.NewStatisticalEncoderSource(),
			commonOpts...,
		)
	case simulcastSenderMode:
		return sender.NewSender(
			sender.NewSimulcastFilesSource(),
			commonOpts...,
		)
	case videoFileEncoderMode:
		if len(videoFiles) == 0 {
			return nil, errVideoFileRequired
		}

		return NewVideoFileSender(videoFiles, commonOpts...)
	default:
		return nil, fmt.Errorf("%w: %v", errUnknownSenderMode, senderMode)
	}
}

type recv struct {
	receiver           *receiver.Receiver
	receiverRTPLogger  io.WriteCloser
	receiverRTCPLogger io.WriteCloser
	videoOutputFile    *os.File
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

	// videoOutputFile is no longer used - receiver handles file closing internally

	return errors.Join(errs...)
}

func newReceiver(
	nm *NetworkManager,
	id int,
	dataDir string,
	outputVideoPath string,
	loggerFactory plogging.LoggerFactory,
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

	// Create options for the receiver
	opts := []receiver.Option{
		receiver.SetVnet(rightVnet, []string{publicIPRight}),
		receiver.PacketLogWriter(receiverRTPLogger, receiverRTCPLogger),
		receiver.DefaultInterceptors(),
		receiver.SetLoggerFactory(loggerFactory),
	}

	// Add video saving option if outputVideoPath is set
	if outputVideoPath != "" {
		// Use SaveVideo for video saving - remove .ivf extension from base path
		baseOutputPath := strings.TrimSuffix(outputVideoPath, ".ivf")
		opts = append(opts, receiver.SaveVideo(baseOutputPath))
	}

	rc, err := receiver.NewReceiver(opts...)
	if err != nil {
		return recv{}, fmt.Errorf("new receiver: %w", err)
	}

	return recv{
		receiver:           rc,
		receiverRTPLogger:  receiverRTPLogger,
		receiverRTCPLogger: receiverRTCPLogger,
		videoOutputFile:    nil, // No longer used with multiple track approach
	}, nil
}
