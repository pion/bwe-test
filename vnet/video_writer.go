// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package main

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sync"

	"gocv.io/x/gocv"
)

// Global video writer manager for handling GoCV functionality
var globalVideoWriterManager *VideoWriterManager

func init() {
	globalVideoWriterManager = NewVideoWriterManager()
}

// FrameData represents raw frame data from VP8 decoder
type FrameData struct {
	Image      image.Image
	Width      int
	Height     int
	IsKeyFrame bool
	Timestamp  uint64
	TrackID    string
}

// VideoWriterManager manages multiple video writers for different tracks
type VideoWriterManager struct {
	writers map[string]*VideoWriter
	mu      sync.RWMutex
}

// VideoWriter handles video output functionality using GoCV
type VideoWriter struct {
	outputDir    string
	frameCounter int
	enableSave   bool

	// MP4 output support
	enableMP4 bool
	mp4Writer *gocv.VideoWriter
	mp4Path   string

	mu sync.Mutex
}

// NewVideoWriterManager creates a new video writer manager
func NewVideoWriterManager() *VideoWriterManager {
	return &VideoWriterManager{
		writers: make(map[string]*VideoWriter),
	}
}

// NewVideoWriter creates a new video writer
func NewVideoWriter(outputDir string, enableSave bool, enableMP4 bool, mp4Path string, width, height int, frameRate float64) (*VideoWriter, error) {
	vw := &VideoWriter{
		outputDir:    outputDir,
		frameCounter: 0,
		enableSave:   enableSave,
		enableMP4:    enableMP4,
		mp4Path:      mp4Path,
	}

	// Create MP4 writer if MP4 output is enabled
	if enableMP4 && mp4Path != "" {
		// Create MP4 output directory
		mp4Dir := filepath.Dir(mp4Path)
		if err := os.MkdirAll(mp4Dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create MP4 directory: %w", err)
		}

		mp4Writer, err := gocv.VideoWriterFile(mp4Path, "mp4v", frameRate, width, height, true)
		if err != nil {
			return nil, fmt.Errorf("failed to create MP4 writer: %w", err)
		}
		fmt.Printf("Created MP4 writer: %s (%dx%d at %.1f fps)\n", mp4Path, width, height, frameRate)
		vw.mp4Writer = mp4Writer
	}

	return vw, nil
}

// WriteFrame processes and writes a frame
func (vw *VideoWriter) WriteFrame(frameData *FrameData) error {
	vw.mu.Lock()
	defer vw.mu.Unlock()

	if frameData == nil || frameData.Image == nil {
		return fmt.Errorf("invalid frame data")
	}

	// Convert VpxImage to OpenCV Mat
	mat := vw.imageToMat(frameData.Image)
	defer mat.Close()

	if mat.Empty() {
		return fmt.Errorf("empty frame")
	}

	// Save as PNG if enabled
	if vw.enableSave && vw.outputDir != "" {
		if err := vw.saveDecodedFrame(mat); err != nil {
			return fmt.Errorf("failed to save PNG: %w", err)
		}
	}

	// Write to MP4 if enabled
	if vw.enableMP4 && vw.mp4Writer != nil {
		vw.mp4Writer.Write(mat)
		fmt.Printf("VideoWriter: wrote frame to MP4, count now: %d\n", vw.frameCounter+1)
	}

	vw.frameCounter++
	return nil
}

// imageToMat converts image.Image to OpenCV Mat in BGR format
func (vw *VideoWriter) imageToMat(img image.Image) gocv.Mat {
	if img == nil {
		panic("Invalid image")
	}

	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	// Check if it's already a YCbCr image
	if ycbcr, ok := img.(*image.YCbCr); ok {
		return vw.ycbcrToMat(ycbcr)
	}

	// For other image types, convert via RGBA
	rgba := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			rgba.Set(x, y, img.At(x, y))
		}
	}

	// Convert RGBA to OpenCV Mat
	mat, _ := gocv.NewMatFromBytes(h, w, gocv.MatTypeCV8UC4, rgba.Pix)
	bgrMat := gocv.NewMat()
	gocv.CvtColor(mat, &bgrMat, gocv.ColorRGBAToBGR)
	mat.Close()

	return bgrMat
}

// ycbcrToMat converts YCbCr image to OpenCV Mat
func (vw *VideoWriter) ycbcrToMat(ycbcr *image.YCbCr) gocv.Mat {
	bounds := ycbcr.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	ySize := w * h
	uvSize := w * h / 2
	yuv := make([]byte, ySize+uvSize)

	// Copy Y plane
	for i := 0; i < h; i++ {
		start := i * w
		yStart := i * ycbcr.YStride
		copy(yuv[start:start+w], ycbcr.Y[yStart:yStart+w])
	}

	// Interleave U and V into NV12 format (UVUV...)
	uvOffset := ySize
	for i := 0; i < h/2; i++ {
		uStart := i * ycbcr.CStride
		vStart := i * ycbcr.CStride
		for j := 0; j < w/2; j++ {
			yuv[uvOffset] = ycbcr.Cb[uStart+j]
			yuv[uvOffset+1] = ycbcr.Cr[vStart+j]
			uvOffset += 2
		}
	}

	// Create Mat from NV12 raw data (Y plane + interleaved UV)
	yuvMat, _ := gocv.NewMatFromBytes(h+h/2, w, gocv.MatTypeCV8UC1, yuv)

	// Convert to BGR
	bgrMat := gocv.NewMat()
	gocv.CvtColor(yuvMat, &bgrMat, gocv.ColorYUVToRGBNV21)
	yuvMat.Close()

	return bgrMat
}

// saveDecodedFrame saves a decoded frame as PNG (moved from receiver)
func (vw *VideoWriter) saveDecodedFrame(mat gocv.Mat) error {
	if vw.outputDir == "" {
		return nil
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(vw.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	filename := filepath.Join(vw.outputDir, fmt.Sprintf("decoded_frame_%05d.png", vw.frameCounter))

	if ok := gocv.IMWrite(filename, mat); !ok {
		return fmt.Errorf("failed to save decoded frame: %s", filename)
	}

	return nil
}

// Close stops the video writer and cleans up resources
func (vw *VideoWriter) Close() error {
	vw.mu.Lock()
	defer vw.mu.Unlock()

	// Close MP4 writer if it exists
	if vw.mp4Writer != nil {
		vw.mp4Writer.Close()
		fmt.Printf("Closed MP4 writer. Total frames written: %d\n", vw.frameCounter)
	}

	return nil
}

// GetFrameCount returns the number of processed frames
func (vw *VideoWriter) GetFrameCount() int {
	vw.mu.Lock()
	defer vw.mu.Unlock()
	return vw.frameCounter
}

// CreateWriter creates a new video writer for a track
func (vwm *VideoWriterManager) CreateWriter(trackID string, outputDir string, enableSave bool, enableMP4 bool, mp4Path string, width, height int, frameRate float64) error {
	vwm.mu.Lock()
	defer vwm.mu.Unlock()

	if _, exists := vwm.writers[trackID]; exists {
		return fmt.Errorf("writer for track %s already exists", trackID)
	}

	writer, err := NewVideoWriter(outputDir, enableSave, enableMP4, mp4Path, width, height, frameRate)
	if err != nil {
		return fmt.Errorf("failed to create video writer for %s: %w", trackID, err)
	}

	vwm.writers[trackID] = writer
	return nil
}

// WriteFrame writes a frame for a specific track
func (vwm *VideoWriterManager) WriteFrame(frameData *FrameData) error {
	vwm.mu.RLock()
	writer, exists := vwm.writers[frameData.TrackID]
	vwm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no writer found for track %s", frameData.TrackID)
	}

	return writer.WriteFrame(frameData)
}

// CloseWriter closes and removes a writer for a track
func (vwm *VideoWriterManager) CloseWriter(trackID string) error {
	vwm.mu.Lock()
	defer vwm.mu.Unlock()

	writer, exists := vwm.writers[trackID]
	if !exists {
		return fmt.Errorf("no writer found for track %s", trackID)
	}

	err := writer.Close()
	delete(vwm.writers, trackID)
	return err
}

// CloseAll closes all writers
func (vwm *VideoWriterManager) CloseAll() error {
	vwm.mu.Lock()
	defer vwm.mu.Unlock()

	var errs []error
	for trackID, writer := range vwm.writers {
		if err := writer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close writer for %s: %w", trackID, err))
		}
	}

	// Clear the map
	vwm.writers = make(map[string]*VideoWriter)

	if len(errs) > 0 {
		return fmt.Errorf("errors closing writers: %v", errs)
	}
	return nil
}

// GetFrameCount returns frame count for a specific track
func (vwm *VideoWriterManager) GetFrameCount(trackID string) int {
	vwm.mu.RLock()
	defer vwm.mu.RUnlock()

	if writer, exists := vwm.writers[trackID]; exists {
		return writer.GetFrameCount()
	}
	return 0
}

// GetGlobalVideoWriterManager returns the global video writer manager
func GetGlobalVideoWriterManager() *VideoWriterManager {
	return globalVideoWriterManager
}
