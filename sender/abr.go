// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package sender implements WebRTC sender functionality for bandwidth estimation tests.
package sender

import (
	"context"
	"errors"
	"sync"

	"github.com/pion/bwe-test/syncodec"
	"github.com/pion/logging"
	"github.com/pion/webrtc/v4/pkg/media"
)

// StatisticalEncoderSource is a source that fakes a media encoder using syncodec.StatisticalCodec.
type StatisticalEncoderSource struct {
	codec               syncodec.Codec
	sampleWriter        func(media.Sample) error
	updateTargetBitrate chan int
	newFrame            chan syncodec.Frame
	done                chan struct{}
	wg                  sync.WaitGroup
	log                 logging.LeveledLogger
}

var errUninitializedtatisticalEncoderSource = errors.New("write on uninitialized StatisticalEncoderSource.WriteSample")

// NewStatisticalEncoderSource returns a new StatisticalEncoderSource.
func NewStatisticalEncoderSource() *StatisticalEncoderSource {
	return &StatisticalEncoderSource{
		codec: nil,
		sampleWriter: func(_ media.Sample) error {
			return errUninitializedtatisticalEncoderSource
		},
		updateTargetBitrate: make(chan int),
		newFrame:            make(chan syncodec.Frame),
		done:                make(chan struct{}),
		wg:                  sync.WaitGroup{},
		log:                 logging.NewDefaultLoggerFactory().NewLogger("statistical_encoder_source"),
	}
}

// SetTargetBitrate sets the target bitrate for the encoder.
func (s *StatisticalEncoderSource) SetTargetBitrate(rate int) {
	s.updateTargetBitrate <- rate
}

// SetWriter sets the sample writer function.
func (s *StatisticalEncoderSource) SetWriter(f func(sample media.Sample) error) {
	s.sampleWriter = f
}

// Start begins the encoding process and runs until context is done.
func (s *StatisticalEncoderSource) Start(ctx context.Context) error {
	s.wg.Add(1)
	defer s.wg.Done()

	codec, err := syncodec.NewStatisticalEncoder(s)
	if err != nil {
		return err
	}
	s.codec = codec
	go s.codec.Start()
	defer func() {
		if err := s.codec.Close(); err != nil {
			s.log.Errorf("failed to close codec: %v", err)
		}
	}()

	for {
		select {
		case rate := <-s.updateTargetBitrate:
			s.codec.SetTargetBitrate(rate)
			s.log.Infof("target bitrate = %v", rate)
		case frame := <-s.newFrame:
			err := s.sampleWriter(media.Sample{Data: frame.Content, Duration: frame.Duration})
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// WriteFrame writes a frame to the encoder.
func (s *StatisticalEncoderSource) WriteFrame(frame syncodec.Frame) {
	s.newFrame <- frame
}

// Close stops the encoder and cleans up resources.
func (s *StatisticalEncoderSource) Close() error {
	defer s.wg.Wait()
	close(s.done)

	return nil
}
