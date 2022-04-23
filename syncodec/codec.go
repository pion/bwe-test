package syncodec

import (
	"fmt"
	"time"
)

type Frame struct {
	Content  []byte
	Duration time.Duration
}

func (f Frame) String() string {
	return fmt.Sprintf("FRAME: \n\tDURATION: %v\n\tSIZE: %v\n", f.Duration, len(f.Content))
}

type Codec interface {
	GetTargetBitrate() int
	SetTargetBitrate(int)
	Start()
	Close() error
}

type FrameWriter interface {
	WriteFrame(Frame)
}
