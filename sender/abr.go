// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"context"
	"sync"

	"github.com/pion/bwe-test/syncodec"
	"github.com/pion/logging"
	"github.com/pion/webrtc/v3/pkg/media"
)

// StatisticalEncoderSource is a source that fakes a media encoder using syncodec.StatisticalCodec
type StatisticalEncoderSource struct {
	codec               syncodec.Codec
	sampleWriter        func(media.Sample) error
	updateTargetBitrate chan int
	newFrame            chan syncodec.Frame
	done                chan struct{}
	wg                  sync.WaitGroup
	log                 logging.LeveledLogger
}

// NewStatisticalEncoderSource returns a new StatisticalEncoderSource
func NewStatisticalEncoderSource() *StatisticalEncoderSource {
	return &StatisticalEncoderSource{
		codec: nil,
		sampleWriter: func(_ media.Sample) error {
			panic("write on uninitialized StatisticalEncoderSource.WriteSample")
		},
		updateTargetBitrate: make(chan int),
		newFrame:            make(chan syncodec.Frame),
		done:                make(chan struct{}),
		wg:                  sync.WaitGroup{},
		log:                 logging.NewDefaultLoggerFactory().NewLogger("statistical_encoder_source"),
	}
}

func (s *StatisticalEncoderSource) SetTargetBitrate(rate int) {
	s.updateTargetBitrate <- rate
}

func (s *StatisticalEncoderSource) SetWriter(f func(sample media.Sample) error) {
	s.sampleWriter = f
}

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
			s.log.Infof("failed to close codec: %v", err)
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

func (s *StatisticalEncoderSource) WriteFrame(frame syncodec.Frame) {
	s.newFrame <- frame
}

func (s *StatisticalEncoderSource) Close() error {
	defer s.wg.Wait()
	close(s.done)
	return nil
}
