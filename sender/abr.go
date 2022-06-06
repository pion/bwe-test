package sender

import (
	"github.com/pion/bwe-test/syncodec"
	"github.com/pion/webrtc/v3/pkg/media"
	"log"
	"sync"
)

// StatisticalEncoderSource is a source that fakes a media encoder using syncodec.StatisticalCodec
type StatisticalEncoderSource struct {
	codec               syncodec.Codec
	sampleWriter        func(media.Sample) error
	updateTargetBitrate chan int
	newFrame            chan syncodec.Frame
	done                chan struct{}
	wg                  sync.WaitGroup
}

// NewStatisticalEncoderSource returns a new StatisticalEncoderSource
func NewStatisticalEncoderSource() *StatisticalEncoderSource {
	return &StatisticalEncoderSource{
		sampleWriter: func(_ media.Sample) error {
			panic("write on uninitialized StatisticalEncoderSource.WriteSample")
		},
		updateTargetBitrate: make(chan int),
		newFrame:            make(chan syncodec.Frame),
		done:                make(chan struct{}),
		wg:                  sync.WaitGroup{},
	}
}

func (s *StatisticalEncoderSource) SetTargetBitrate(rate int) {
	s.codec.SetTargetBitrate(rate)
}

func (s *StatisticalEncoderSource) SetWriter(f func(sample media.Sample) error) {
	s.sampleWriter = f
}

func (s *StatisticalEncoderSource) Start() error {
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
			log.Printf("failed to close codec: %v", err)
		}
	}()

	for {
		select {
		case rate := <-s.updateTargetBitrate:
			s.codec.SetTargetBitrate(rate)
		case frame := <-s.newFrame:
			err := s.sampleWriter(media.Sample{Data: frame.Content, Duration: frame.Duration})
			if err != nil {
				return err
			}
		case <-s.done:
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
