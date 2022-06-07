package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/pion/bwe-test/receiver"
	"github.com/pion/bwe-test/sender"
)

func realMain() error {
	mode := flag.String("mode", "sender", "Mode: sender/receiver")
	flag.Parse()

	if *mode == "receiver" {
		r, err := receiver.NewReceiver()
		if err != nil {
			return err
		}
		err = r.SetupPeerConnection()
		if err != nil {
			return err
		}
		http.Handle("/sdp", r.SDPHandler())
		log.Fatal(http.ListenAndServe("0.0.0.0:8080", nil))
		return nil
	}
	if *mode == "sender" {
		s, err := sender.NewSender(
			sender.NewStatisticalEncoderSource(),
		)
		if err != nil {
			return err
		}
		err = s.SetupPeerConnection()
		if err != nil {
			return err
		}
		err = s.SignalHTTP("10.0.0.1:8080", "sdp")
		if err != nil {
			return err
		}
		return s.Start()
	}

	log.Fatalf("invalid mode: %s\n", *mode)
	return nil
}

func main() {
	err := realMain()
	if err != nil {
		log.Fatal(err)
	}
}
