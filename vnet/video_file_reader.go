// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bamiaux/rez"
	"github.com/pion/bwe-test/sender"
	"github.com/pion/logging"
	"github.com/pion/mediadevices/pkg/codec/vpx"
)

// VideoFileReader reads frames from JPG files in a directory and sends them to RTCSender.
type VideoFileReader struct {
	sender     *sender.RTCSender
	trackID    string
	width      int
	height     int
	fps        float64
	log        logging.LeveledLogger
	imagePath  string   // Directory containing frame files
	imageFiles []string // Sorted list of frame files
}

var (
	errNoJPGFilesFound      = errors.New("no JPG frame files found in directory")
	errDirectoryNotExist    = errors.New("directory does not exist")
	errNoJPGFilesInDir      = errors.New("no JPG files found in directory")
	errNoFrameFiles         = errors.New("no frame files available")
	errFrameIndexOutOfRange = errors.New("frame index out of range")
	errImageNotYCbCr        = errors.New("decoded image is not in YCbCr format")
	errSourceNotYCbCr       = errors.New("source image is not in YCbCr format")
	errInvalidImageFilePath = errors.New("invalid image file path")
)

// NewVideoFileReader creates a new video file reader that reads JPG frames from a directory.
// imagePath should be a directory containing JPG files with pattern like "frame_001.jpg", "frame_002.jpg", etc.
func NewVideoFileReader(
	rtcSender *sender.RTCSender,
	imagePath string,
	trackID string,
	width, height int,
) (*VideoFileReader, error) {
	// Create VP8 encoder builder
	vp8Params, err := vpx.NewVP8Params()
	if err != nil {
		return nil, fmt.Errorf("failed to create VP8 params: %w", err)
	}

	// Configure VP8 parameters for video streaming
	vp8Params.BitRate = 1_000_000 // 1 Mbps

	// Add video track to sender
	err = rtcSender.AddVideoTrack(sender.VideoTrackInfo{
		TrackID:        trackID,
		Width:          width,
		Height:         height,
		EncoderBuilder: &vp8Params,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to add video track: %w", err)
	}

	// Discover JPG files in the directory
	imageFiles, err := discoverImageFiles(imagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to discover frame files in %s: %w", imagePath, err)
	}

	if len(imageFiles) == 0 {
		return nil, fmt.Errorf("%w: %s", errNoJPGFilesFound, imagePath)
	}

	return &VideoFileReader{
		sender:     rtcSender,
		trackID:    trackID,
		width:      width,
		height:     height,
		fps:        10.0, // Default FPS
		log:        logging.NewDefaultLoggerFactory().NewLogger("video_file_reader"),
		imagePath:  imagePath,
		imageFiles: imageFiles,
	}, nil
}

// discoverImageFiles finds and sorts JPG files in the given directory.
// Supports patterns like frame_001.jpg, frame_002.jpg, etc.
func discoverImageFiles(imageDir string) ([]string, error) {
	// Check if directory exists
	if _, err := os.Stat(imageDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s", errDirectoryNotExist, imageDir)
	}

	// Find all JPG files
	pattern := filepath.Join(imageDir, "*.jpg")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to glob JPG files: %w", err)
	}

	if len(matches) == 0 {
		return nil, errNoJPGFilesInDir
	}

	// Sort files naturally (frame_1.jpg, frame_2.jpg, ..., frame_10.jpg)
	sort.Slice(matches, func(i, j int) bool {
		return naturalLess(filepath.Base(matches[i]), filepath.Base(matches[j]))
	})

	return matches, nil
}

// naturalLess compares two filenames naturally (handles numbers correctly).
func naturalLess(a, b string) bool {
	// Extract numeric parts for comparison
	aNum := extractNumber(a)
	bNum := extractNumber(b)

	if aNum != bNum {
		return aNum < bNum
	}

	// If numbers are equal, fall back to string comparison
	return a < b
}

// extractNumber extracts the first number found in a filename.
func extractNumber(filename string) int {
	// Remove extension
	name := strings.TrimSuffix(filename, filepath.Ext(filename))

	// Find all digits
	var numStr strings.Builder
	for _, char := range name {
		if char >= '0' && char <= '9' {
			numStr.WriteRune(char)
		} else if numStr.Len() > 0 {
			// Stop at first non-digit after finding digits
			break
		}
	}

	if numStr.Len() == 0 {
		return 0
	}

	num, err := strconv.Atoi(numStr.String())
	if err != nil {
		return 0
	}

	return num
}

// Start begins reading and sending frames from JPG files.
func (r *VideoFileReader) Start(ctx context.Context) error {
	if len(r.imageFiles) == 0 {
		return errNoFrameFiles
	}

	frameDuration := time.Duration(1000/r.fps) * time.Millisecond
	ticker := time.NewTicker(frameDuration)
	defer ticker.Stop()

	r.log.Infof("Starting video file reader with %d frames at %.1f FPS", len(r.imageFiles), r.fps)

	// Local frame index, starts from 0 for each Start() call
	frameIndex := 0

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// Load current frame
			var img image.Image
			img, err := r.loadFrame(frameIndex)
			if err != nil {
				r.log.Warnf("Failed to load frame %d (%s): %v", frameIndex, r.imageFiles[frameIndex], err)
				// Skip to next frame on error
				frameIndex = (frameIndex + 1) % len(r.imageFiles)

				continue
			}

			// Resize if needed
			if img.Bounds().Dx() != r.width || img.Bounds().Dy() != r.height {
				img, err = r.resizeImage(img, r.width, r.height)
				if err != nil {
					r.log.Warnf("Failed to resize image: %v", err)
					// Skip to next frame on error
					frameIndex = (frameIndex + 1) % len(r.imageFiles)

					continue
				}
			}

			// Send the frame
			err = r.sender.SendFrame(r.trackID, img)
			if err != nil {
				r.log.Warnf("Failed to send frame: %v", err)
			}

			// Move to next frame (loop back to 0 if at end)
			frameIndex = (frameIndex + 1) % len(r.imageFiles)
		}
	}
}

// loadFrame loads a JPG image from the specified frame index and returns it as YCbCr.
func (r *VideoFileReader) loadFrame(frameIndex int) (*image.YCbCr, error) {
	if frameIndex < 0 || frameIndex >= len(r.imageFiles) {
		return nil, fmt.Errorf("%w: %d [0, %d)", errFrameIndexOutOfRange, frameIndex, len(r.imageFiles))
	}

	framePath := r.imageFiles[frameIndex]

	// Validate file path to prevent directory traversal
	if !isValidImageFile(framePath) {
		return nil, fmt.Errorf("%w: %s", errInvalidImageFilePath, framePath)
	}

	// Open the image file
	file, err := os.Open(framePath) //nolint:gosec // File path validated by isValidImageFile
	if err != nil {
		return nil, fmt.Errorf("failed to open frame file %s: %w", framePath, err)
	}
	defer func() {
		_ = file.Close()
	}()

	// Decode the JPEG image
	img, err := jpeg.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("failed to decode JPEG %s: %w", framePath, err)
	}

	// Type assert to *image.YCbCr (JPEG images are typically decoded as YCbCr)
	ycbcrImg, ok := img.(*image.YCbCr)
	if !ok {
		return nil, fmt.Errorf("%w, got %T", errImageNotYCbCr, img)
	}

	return ycbcrImg, nil
}

// resizeImage resizes an YCbCr image to the specified dimensions.
func (r *VideoFileReader) resizeImage(src image.Image, width, height int) (image.Image, error) {
	// Check if source is YCbCr and try to maintain color space
	if srcYCbCr, ok := src.(*image.YCbCr); ok {
		// Create destination YCbCr image with same subsampling ratio
		dst := image.NewYCbCr(image.Rect(0, 0, width, height), srcYCbCr.SubsampleRatio)

		// Use rez library for high-quality YCbCr resizing (maintains color space)
		err := rez.Convert(dst, src, rez.NewBicubicFilter())
		if err != nil {
			return nil, err
		}

		return dst, nil
	}

	return nil, fmt.Errorf("%w, got %T", errSourceNotYCbCr, src)
}

// isValidImageFile validates that the file path is safe and is an image file.
func isValidImageFile(filePath string) bool {
	// Clean the path to resolve any .. or . components
	cleanPath := filepath.Clean(filePath)

	// Check for directory traversal attempts
	if strings.Contains(cleanPath, "..") {
		return false
	}

	// Only allow files in the current directory or subdirectories
	// This prevents access to system files or files outside the project
	if filepath.IsAbs(cleanPath) && !strings.HasPrefix(cleanPath, "/tmp/") {
		return false
	}

	// Must be a .jpg file
	return strings.HasSuffix(strings.ToLower(cleanPath), ".jpg")
}

// VideoFileSender is a wrapper that combines RTCSender with multiple VideoFileReaders.
type VideoFileSender struct {
	*sender.RTCSender
	readers []*VideoFileReader
}

// VideoFileInfo holds information about a video file and its track.
type VideoFileInfo struct {
	FilePath string
	TrackID  string
	Width    int
	Height   int
}

// NewVideoFileSender creates a sender that reads from multiple video files.
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
			return nil, fmt.Errorf("failed to create video reader for %s: %w", videoFile.FilePath, err)
		}
		readers = append(readers, reader)
	}

	return &VideoFileSender{
		RTCSender: rtcSender,
		readers:   readers,
	}, nil
}

// Start begins the video file sending process for all tracks.
func (s *VideoFileSender) Start(ctx context.Context) error {
	// Start all video file readers
	for _, reader := range s.readers {
		go func(reader *VideoFileReader) {
			_ = reader.Start(ctx)
		}(reader)
	}

	// Start the full sender (with bandwidth estimation for vnet WebRTC)
	return s.RTCSender.Start(ctx)
}

// Close releases all resources.
func (s *VideoFileSender) Close() error {
	return s.RTCSender.Close()
}
