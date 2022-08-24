package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/pion/bwe-test/logging"
	"github.com/pion/bwe-test/receiver"
	"github.com/pion/bwe-test/sender"
)

const (
	initialBitrate   = 100_000
	minTargetBitrate = 100_000
	maxTargetBitrate = 50_000_000
)

func realMain() error {
	mode := flag.String("mode", "sender", "Mode: sender/receiver")
	addr := flag.String("addr", ":4242", "address to listen on /connect to")
	rtpLogFile := flag.String("rtp-log", "", "log RTP to file (use 'stdout' or a file name")
	rtcpInboundLogFile := flag.String("rtcp-inbound-log", "", "log RTCP to file (use 'stdout' or a file name")
	rtcpOutboundLogFile := flag.String("rtcp-outbound-log", "", "log RTCP to file (use 'stdout' or a file name")
	ccLogFile := flag.String("cc-log", "", "log congestion control target bitrate")
	flag.Parse()

	if *mode == "receiver" {
		return receive(*addr, *rtpLogFile, *rtcpInboundLogFile, *rtcpOutboundLogFile)
	}
	if *mode == "sender" {
		return send(*addr, *rtpLogFile, *rtcpInboundLogFile, *rtcpOutboundLogFile, *ccLogFile)
	}

	log.Fatalf("invalid mode: %s\n", *mode)
	return nil
}

func receive(addr, rtpLogFile, rtcpInboundLogFile, rtcpOutboundLogFile string) error {
	options := []receiver.Option{}

	rtpLogger, err := logging.GetLogFile(rtpLogFile)
	if err != nil {
		return err
	}
	defer rtpLogger.Close()

	rtcpInboundLogger, err := logging.GetLogFile(rtcpInboundLogFile)
	if err != nil {
		return err
	}
	defer rtcpInboundLogger.Close()

	rtcpOutboundLogger, err := logging.GetLogFile(rtcpOutboundLogFile)
	if err != nil {
		return err
	}
	defer rtcpOutboundLogger.Close()

	options = append(options,
		receiver.PacketLogWriter(rtpLogger, rtcpOutboundLogger, rtcpInboundLogger),
		receiver.RTCPReports(),
		receiver.TWCC(),
	)

	r, err := receiver.New(options...)
	if err != nil {
		return err
	}
	err = r.SetupPeerConnection()
	if err != nil {
		return err
	}
	http.Handle("/sdp", r.SDPHandler())
	log.Fatal(http.ListenAndServe(addr, nil))
	return nil
}

func send(addr, rtpLogFile, rtcpInboundLogFile, rtcpOutboundLogFile, ccLogFile string) error {
	options := []sender.Option{}

	rtpLogger, err := logging.GetLogFile(rtpLogFile)
	if err != nil {
		return err
	}
	defer rtpLogger.Close()

	rtcpInboundLogger, err := logging.GetLogFile(rtcpInboundLogFile)
	if err != nil {
		return err
	}
	defer rtcpInboundLogger.Close()

	rtcpOutboundLogger, err := logging.GetLogFile(rtcpOutboundLogFile)
	if err != nil {
		return err
	}
	defer rtcpOutboundLogger.Close()

	ccLogger, err := logging.GetLogFile(ccLogFile)
	if err != nil {
		return err
	}
	defer ccLogger.Close()

	options = append(options,
		sender.CCLogWriter(ccLogger),
		sender.PacketLogWriter(rtpLogger, rtcpOutboundLogger, rtcpInboundLogger),
		sender.RTCPReports(),
		sender.GCC(initialBitrate, minTargetBitrate, maxTargetBitrate, true),
	)
	s, err := sender.New(
		sender.NewStatisticalEncoderSource(),
		options...,
	)
	if err != nil {
		return err
	}
	err = s.SetupPeerConnection()
	if err != nil {
		return err
	}
	err = s.SignalHTTP(addr, "sdp")
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

	return s.Start(ctx)
}

func main() {
	err := realMain()
	if err != nil {
		log.Fatal(err)
	}
}
