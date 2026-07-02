// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"context"
	"image"

	"github.com/pion/webrtc/v4"
)

// WebRTCSender is a common interface for different sender implementations.
type WebRTCSender interface {
	SetupPeerConnection() error
	CreateOffer() (*webrtc.SessionDescription, error)
	AcceptAnswer(answer *webrtc.SessionDescription) error
	Start(ctx context.Context) error
}

// VideoSource represents any source of video frames (camera, file, virtual buffer, etc.)
type VideoSource interface {
	ID() string
	Read() (image.Image, func(), error)
	Close() error
}

// captureTimeSource exposes the capture timestamp of the most recently dequeued
// frame/chunk. Both *FrameBuffer (video) and *AudioBuffer (audio) implement it,
// letting encodeAndSendTrack read capture stamps without knowing the media type.
type captureTimeSource interface {
	LastCaptureTSUs() int64
	LastDequeueWallUs() int64
}

// frameSignaler exposes a buffer's enqueue/close notifications so runEncodeLoop
// can wait poll-free for the next frame/chunk. Implemented by both buffers.
type frameSignaler interface {
	FrameReady() <-chan struct{}
	Closed() <-chan struct{}
}
