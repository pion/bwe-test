package sender

import (
	"context"
	"io"
	"os"
	"sync"
	"time"

	"github.com/pion/logging"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/ivfreader"
)

const (
	lowFile    = "low.ivf"
	lowBitrate = 300_000

	medFile    = "med.ivf"
	medBitrate = 1_000_000

	highFile    = "high.ivf"
	highBitrate = 2_500_000

	initialBitrate = 300_000

	ivfHeaderSize = 32
)

type SimulcastFilesSource struct {
	qualityLevels []struct {
		fileName string
		bitrate  int
	}
	currentQualityLevel int
	updateTargetBitrate chan int
	WriteSample         func(media.Sample) error
	done                chan struct{}
	wg                  sync.WaitGroup
	log                 logging.LeveledLogger
}

func (s *SimulcastFilesSource) Close() error {
	defer s.wg.Wait()
	close(s.done)
	return nil
}

// NewSimulcastFilesSource returns a new SimulcastFilesSource
func NewSimulcastFilesSource() *SimulcastFilesSource {
	return &SimulcastFilesSource{
		qualityLevels: []struct {
			fileName string
			bitrate  int
		}{
			{lowFile, lowBitrate},
			{medFile, medBitrate},
			{highFile, highBitrate},
		},
		currentQualityLevel: 0,
		updateTargetBitrate: make(chan int),
		WriteSample: func(sample media.Sample) error {
			panic("write on uninitialized SimulcastFileSource.WriteSample")
		},
		done: make(chan struct{}),
		wg:   sync.WaitGroup{},
		log:  logging.NewDefaultLoggerFactory().NewLogger("simulcast_source"),
	}
}

func (s *SimulcastFilesSource) SetTargetBitrate(rate int) {
	s.updateTargetBitrate <- rate
}

func (s *SimulcastFilesSource) SetWriter(f func(sample media.Sample) error) {
	s.WriteSample = f
}

func (s *SimulcastFilesSource) Start(ctx context.Context) error {
	files := make(map[string]*os.File)
	file, err := os.Open(s.qualityLevels[s.currentQualityLevel].fileName)
	if err != nil {
		return err
	}
	files[s.qualityLevels[s.currentQualityLevel].fileName] = file
	defer func() {
		for _, file := range files {
			err1 := file.Close()
			if err1 != nil {
				s.log.Infof("failed to close file %v: %v", file.Name(), err1)
			}
		}
	}()

	ivf, header, err := ivfreader.NewWith(file)
	if err != nil {
		return err
	}
	// Send our video file frame at a time. Pace our sending so we send it at the same speed it should be played back as.
	// This isn't required since the video is timestamped, but we will such much higher loss if we send all at once.
	//
	// It is important to use a time.Ticker instead of time.Sleep because
	// * avoids accumulating skew, just calling time.Sleep didn't compensate for the time spent parsing the data
	// * works around latency issues with Sleep (see https://github.com/golang/go/issues/44343)
	ticker := time.NewTicker(time.Millisecond * time.Duration((float32(header.TimebaseNumerator)/float32(header.TimebaseDenominator))*1000))
	var frame []byte
	frameHeader := &ivfreader.IVFFrameHeader{}
	currentTimestamp := uint64(0)

	setReaderFile := func(filename string) (f func(_ int64) io.Reader, err error) {
		file, ok := files[s.qualityLevels[s.currentQualityLevel].fileName]
		if !ok {
			file, err = os.Open(filename)
			if err != nil {
				return nil, err
			}
			files[s.qualityLevels[s.currentQualityLevel].fileName] = file
		}
		if _, err = file.Seek(ivfHeaderSize, io.SeekStart); err != nil {
			return nil, err
		}
		return func(_ int64) io.Reader {
			return file
		}, nil
	}

	switchQualityLevel := func(newQualityLevel int) error {
		s.log.Infof("Switching from %s to %s \n", s.qualityLevels[s.currentQualityLevel].fileName, s.qualityLevels[newQualityLevel].fileName)
		s.currentQualityLevel = newQualityLevel

		readerFile, err1 := setReaderFile(s.qualityLevels[s.currentQualityLevel].fileName)
		if err1 != nil {
			return err1
		}
		ivf.ResetReader(readerFile)
		for {
			if frame, frameHeader, err = ivf.ParseNextFrame(); err != nil {
				break
			} else if frameHeader.Timestamp >= currentTimestamp && frame[0]&0x1 == 0 {
				break
			}
		}
		return nil
	}

	targetBitrate := initialBitrate
	for {
		select {
		case rate := <-s.updateTargetBitrate:
			targetBitrate = rate
		case <-ticker.C:
			switch {
			// If current quality level is below target bitrate drop to level below
			case s.currentQualityLevel != 0 && targetBitrate < s.qualityLevels[s.currentQualityLevel].bitrate:
				err = switchQualityLevel(s.currentQualityLevel - 1)
				if err != nil {
					return err
				}

				// If next quality level is above target bitrate move to next level
			case len(s.qualityLevels) > (s.currentQualityLevel+1) && targetBitrate > s.qualityLevels[s.currentQualityLevel+1].bitrate:
				err = switchQualityLevel(s.currentQualityLevel + 1)
				if err != nil {
					return err
				}

			// Adjust outbound bandwidth for probing
			default:
				frame, _, err = ivf.ParseNextFrame()
			}

			switch err {
			// No error write the video frame
			case nil:
				currentTimestamp = frameHeader.Timestamp
				if err = s.WriteSample(media.Sample{Data: frame, Duration: time.Second}); err != nil {
					return err
				}
			// If we have reached the end of the file start again
			case io.EOF:
				readerFile, err1 := setReaderFile(s.qualityLevels[s.currentQualityLevel].fileName)
				if err1 != nil {
					return err1
				}
				ivf.ResetReader(readerFile)
			// Error besides io.EOF that we dont know how to handle
			default:
				return err
			}
		case <-ctx.Done():
			return nil
		}
	}
}
