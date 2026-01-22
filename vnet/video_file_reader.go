// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package main

import (
	"context"
	"fmt"
	"image"
	"time"

	"github.com/pion/bwe-test/sender"
	"github.com/pion/logging"
	"github.com/pion/mediadevices/pkg/codec/vpx"
	"gocv.io/x/gocv"
)

// VideoFileReader reads frames from a video file and sends them to RTCSender
type VideoFileReader struct {
	sender       *sender.RTCSender
	videoCapture *gocv.VideoCapture
	trackID      string
	width        int
	height       int
	fps          float64
	log          logging.LeveledLogger
}

// NewVideoFileReader creates a new video file reader
func NewVideoFileReader(rtcSender *sender.RTCSender, videoPath string, trackID string, width, height int) (*VideoFileReader, error) {
	capture, err := gocv.VideoCaptureFile(videoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open video file: %w", err)
	}

	fps := 10.0

	// Create VP8 encoder parameters
	vpxParams, err := vpx.NewVP8Params()
	if err != nil {
		capture.Close()
		return nil, fmt.Errorf("failed to create VP8 params: %w", err)
	}
	vpxParams.BitRate = 1_000_000 // 1 Mbps initial bitrate

	// Add video track to sender with VP8 encoder
	err = rtcSender.AddVideoTrack(sender.VideoTrackInfo{
		TrackID:        trackID,
		Width:          width,
		Height:         height,
		EncoderBuilder: &vpxParams,
	})
	if err != nil {
		capture.Close()
		return nil, fmt.Errorf("failed to add video track: %w", err)
	}

	return &VideoFileReader{
		sender:       rtcSender,
		videoCapture: capture,
		trackID:      trackID,
		width:        width,
		height:       height,
		fps:          fps,
		log:          logging.NewDefaultLoggerFactory().NewLogger("video_file_reader"),
	}, nil
}

// Start begins reading and sending frames
func (r *VideoFileReader) Start(ctx context.Context) error {
	frameDuration := time.Duration(1000/r.fps) * time.Millisecond
	ticker := time.NewTicker(frameDuration)
	defer ticker.Stop()

	mat := gocv.NewMat()
	defer mat.Close()

	resized := gocv.NewMat()
	defer resized.Close()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if ok := r.videoCapture.Read(&mat); !ok {
				// Loop back to beginning
				r.videoCapture.Set(gocv.VideoCapturePosMsec, 0)
				continue
			}

			if mat.Empty() {
				continue
			}

			var img image.Image
			var err error

			// Check if resize is needed
			if mat.Cols() != r.width || mat.Rows() != r.height {
				// Resize using OpenCV
				gocv.Resize(mat, &resized, image.Point{X: r.width, Y: r.height}, 0, 0, gocv.InterpolationLinear)
				img, err = resized.ToImage()
			} else {
				img, err = mat.ToImage()
			}

			if err != nil {
				continue
			}

			// Send frame to sender
			r.sender.SendFrame(r.trackID, img)
		}
	}
}

// Close releases resources
func (r *VideoFileReader) Close() error {
	if r.videoCapture != nil {
		r.videoCapture.Close()
	}
	return nil
}

// VideoFileSender is a wrapper that combines RTCSender with multiple VideoFileReaders
type VideoFileSender struct {
	*sender.RTCSender
	readers []*VideoFileReader
}

// VideoFileInfo holds information about a video file and its track
type VideoFileInfo struct {
	FilePath string
	TrackID  string
	Width    int
	Height   int
}

// NewVideoFileSender creates a sender that reads from multiple video files
func NewVideoFileSender(videoFiles []VideoFileInfo, opts ...sender.Option) (*VideoFileSender, error) {
	// Create RTCSender
	rtcSender, err := sender.NewRTCSender(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create RTCSender: %w", err)
	}

	// Create video file readers for each file
	var readers []*VideoFileReader
	for _, videoFile := range videoFiles {
		reader, err := NewVideoFileReader(rtcSender, videoFile.FilePath, videoFile.TrackID, videoFile.Width, videoFile.Height)
		if err != nil {
			// Clean up already created readers
			for _, r := range readers {
				r.Close()
			}
			return nil, fmt.Errorf("failed to create video reader for %s: %w", videoFile.FilePath, err)
		}
		readers = append(readers, reader)
	}

	return &VideoFileSender{
		RTCSender: rtcSender,
		readers:   readers,
	}, nil
}

// Start begins the video file sending process for all tracks
func (s *VideoFileSender) Start(ctx context.Context) error {
	// Start frame reading in background for each reader
	for i, reader := range s.readers {
		go func(r *VideoFileReader, index int) {
			r.Start(ctx)
		}(reader, i)
	}

	// Start the RTCSender
	return s.RTCSender.Start(ctx)
}

// Close releases all resources
func (s *VideoFileSender) Close() error {
	for _, reader := range s.readers {
		if reader != nil {
			reader.Close()
		}
	}
	return nil
}
