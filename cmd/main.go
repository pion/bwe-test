// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package main implements the entry point for the bandwidth estimation test tool.
package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/pion/bwe-test/logging"
	"github.com/pion/bwe-test/receiver"
	"github.com/pion/bwe-test/sender"
)

const initialBitrate = 100_000

func realMain() error {
	mode := flag.String("mode", "sender", "Mode: sender/receiver")
	addr := flag.String("addr", ":4242", "address to listen on /connect to")
	rtpLogFile := flag.String("rtp-log", "", "log RTP to file (use 'stdout' or a file name")
	rtcpLogFile := flag.String("rtcp-log", "", "log RTCP to file (use 'stdout' or a file name")
	ccLogFile := flag.String("cc-log", "", "log congestion control target bitrate")
	flag.Parse()

	if *mode == "receiver" {
		return receive(*addr, *rtpLogFile, *rtcpLogFile)
	}
	if *mode == "sender" {
		return send(*addr, *rtpLogFile, *rtcpLogFile, *ccLogFile)
	}

	log.Fatalf("invalid mode: %s", *mode)

	return nil
}

func receive(addr, rtpLogFile, rtcpLogFile string) error {
	rcv, err := newReceiver(rtpLogFile, rtcpLogFile)
	if err != nil {
		return err
	}
	defer func() {
		if err = rcv.Close(); err != nil {
			log.Printf("failed to close receiver: %v", err)
		}
	}()

	err = rcv.receiver.SetupPeerConnection()
	if err != nil {
		return err
	}
	http.Handle("/sdp", rcv.receiver.SDPHandler())

	//nolint:gosec
	return http.ListenAndServe(addr, nil)
}

type recv struct {
	receiver   *receiver.Receiver
	rtpLogger  io.WriteCloser
	rtcpLogger io.WriteCloser
}

func (c recv) Close() error {
	var errs []error

	err := c.receiver.Close()
	if err != nil {
		errs = append(errs, err)
	}

	if c.rtpLogger != nil {
		err = c.rtpLogger.Close()
		if err != nil {
			errs = append(errs, err)
		}
	}

	if c.rtcpLogger != nil {
		err = c.rtcpLogger.Close()
		if err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func newReceiver(rtpLogFile, rtcpLogFile string) (recv, error) {
	options := []receiver.Option{
		receiver.PacketLogWriter(os.Stdout, os.Stdout),
		receiver.DefaultInterceptors(),
	}
	var rtpLogger io.WriteCloser
	var rtcpLogger io.WriteCloser
	var err error
	if rtpLogFile != "" {
		rtpLogger, err = logging.GetLogFile(rtpLogFile)
		if err != nil {
			return recv{}, err
		}
	}
	if rtcpLogFile != "" {
		rtcpLogger, err = logging.GetLogFile(rtcpLogFile)
		if err != nil {
			return recv{}, err
		}
	}
	if rtpLogger != nil || rtcpLogger != nil {
		options = append(options, receiver.PacketLogWriter(rtpLogger, rtcpLogger))
	}
	r, err := receiver.NewReceiver(options...)
	if err != nil {
		return recv{}, err
	}

	return recv{
		receiver:   r,
		rtpLogger:  rtpLogger,
		rtcpLogger: rtcpLogger,
	}, nil
}

func send(addr, rtpLogFile, rtcpLogFile, ccLogFile string) error {
	snd, err := newSender(rtpLogFile, rtcpLogFile, ccLogFile)
	if err != nil {
		return err
	}
	defer func() {
		if err = snd.Close(); err != nil {
			log.Printf("failed to close sender: %v", err)
		}
	}()

	err = snd.sender.SetupPeerConnection()
	if err != nil {
		return err
	}
	err = snd.sender.SignalHTTP(addr, "sdp")
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer func() {
		signal.Stop(sigs)
		cancel()
	}()
	go func() {
		select {
		case <-sigs:
			cancel()
			log.Println("cancel called")
		case <-ctx.Done():
		}
	}()

	return snd.sender.Start(ctx)
}

type sndr struct {
	sender     *sender.Sender
	rtpLogger  io.WriteCloser
	rtcpLogger io.WriteCloser
	ccLogger   io.WriteCloser
}

func (s sndr) Close() error {
	var errs []error

	err := s.rtpLogger.Close()
	if err != nil {
		errs = append(errs, err)
	}

	err = s.rtcpLogger.Close()
	if err != nil {
		errs = append(errs, err)
	}

	err = s.ccLogger.Close()
	if err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func newSender(rtpLogFile, rtcpLogFile, ccLogFile string) (sndr, error) {
	options := []sender.Option{
		sender.DefaultInterceptors(),
		sender.GCC(initialBitrate),
	}
	var rtpLogger io.WriteCloser
	var rtcpLogger io.WriteCloser
	var ccLogger io.WriteCloser
	var err error
	if rtpLogFile != "" {
		rtpLogger, err = logging.GetLogFile(rtpLogFile)
		if err != nil {
			return sndr{}, err
		}
	}
	if rtcpLogFile != "" {
		rtcpLogger, err = logging.GetLogFile(rtcpLogFile)
		if err != nil {
			return sndr{}, err
		}
	}
	if ccLogFile != "" {
		ccLogger, err = logging.GetLogFile(ccLogFile)
		if err != nil {
			return sndr{}, err
		}
		options = append(options, sender.CCLogWriter(ccLogger))
	}
	if rtpLogger != nil || rtcpLogger != nil {
		options = append(options, sender.PacketLogWriter(rtpLogger, rtcpLogger))
	}
	snd, err := sender.NewSender(
		sender.NewStatisticalEncoderSource(),
		options...,
	)
	if err != nil {
		return sndr{}, err
	}

	return sndr{
		sender:     snd,
		rtpLogger:  rtpLogger,
		rtcpLogger: rtcpLogger,
		ccLogger:   ccLogger,
	}, nil
}

func main() {
	err := realMain()
	if err != nil {
		log.Fatal(err)
	}
}
