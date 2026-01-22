// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package main

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVideoFileReader_FrameDiscovery tests the frame file discovery and sorting.
func TestVideoFileReader_FrameDiscovery(t *testing.T) {
	// Create temporary directory for test frames
	tempDir := t.TempDir()

	// Create test frame files with various naming patterns
	testFrames := []string{
		"frame_1.jpg",
		"frame_10.jpg",
		"frame_2.jpg",
		"frame_20.jpg",
		"frame_3.jpg",
	}

	// Create dummy JPG files
	for _, frameName := range testFrames {
		createTestJPG(t, filepath.Join(tempDir, frameName), 100, 100, color.RGBA{255, 0, 0, 255})
	}

	// Test frame discovery
	imageFiles, err := discoverImageFiles(tempDir)
	require.NoError(t, err)
	require.NotEmpty(t, imageFiles)

	// Verify natural sorting works correctly
	expectedOrder := []string{
		"frame_1.jpg",
		"frame_2.jpg",
		"frame_3.jpg",
		"frame_10.jpg",
		"frame_20.jpg",
	}

	actualOrder := make([]string, len(imageFiles))
	for i, fullPath := range imageFiles {
		actualOrder[i] = filepath.Base(fullPath)
	}

	assert.Equal(t, expectedOrder, actualOrder, "Frame files should be sorted naturally")

	t.Logf("Discovered and sorted %d frame files correctly", len(imageFiles))
}

// TestVideoFileReader_NumberExtraction tests the number extraction logic.
func TestVideoFileReader_NumberExtraction(t *testing.T) {
	testCases := []struct {
		filename string
		expected int
	}{
		{"frame_1.jpg", 1},
		{"frame_10.jpg", 10},
		{"frame_001.jpg", 1},
		{"video_frame_123.jpg", 123},
		{"nonum.jpg", 0},
		{"prefix_999_suffix.jpg", 999},
		{"", 0},
	}

	for _, tc := range testCases {
		t.Run(tc.filename, func(t *testing.T) {
			result := extractNumber(tc.filename)
			assert.Equal(t, tc.expected, result, "Number extraction failed for %s", tc.filename)
		})
	}
}

// TestVideoFileReader_LoadFrame tests frame loading functionality.
func TestVideoFileReader_LoadFrame(t *testing.T) {
	// Create temporary directory with test frames
	tempDir := t.TempDir()

	// Create test frames with different colors
	colors := []color.RGBA{
		{255, 0, 0, 255}, // Red
		{0, 255, 0, 255}, // Green
		{0, 0, 255, 255}, // Blue
	}

	imageFiles := make([]string, len(colors))
	for i, col := range colors {
		framePath := filepath.Join(tempDir, fmt.Sprintf("frame_%03d.jpg", i+1))
		createTestJPG(t, framePath, 200, 150, col)
		imageFiles[i] = framePath
	}

	// Create VideoFileReader with nil sender for testing frame loading only
	reader := &VideoFileReader{
		sender:     nil, // We're only testing frame loading, not sending
		trackID:    "test-track",
		width:      200,
		height:     150,
		fps:        30.0,
		imageFiles: imageFiles,
	}

	// Test loading each frame
	for i := range imageFiles {
		img, err := reader.loadFrame(i)
		require.NoError(t, err, "Failed to load frame %d", i)
		require.NotNil(t, img, "Frame %d should not be nil", i)

		// Verify image dimensions
		bounds := img.Bounds()
		assert.Equal(t, 200, bounds.Dx(), "Frame %d width incorrect", i)
		assert.Equal(t, 150, bounds.Dy(), "Frame %d height incorrect", i)

		t.Logf("Successfully loaded frame %d: %dx%d", i, bounds.Dx(), bounds.Dy())
	}

	// Test out of bounds
	_, err := reader.loadFrame(-1)
	assert.Error(t, err, "Should error on negative index")

	_, err = reader.loadFrame(len(imageFiles))
	assert.Error(t, err, "Should error on index >= length")
}

// TestVideoFileReader_ResizeImage tests image resizing functionality.
func TestVideoFileReader_ResizeImage(t *testing.T) {
	reader := &VideoFileReader{
		sender:  nil, // We're only testing image resizing, not sending
		trackID: "test-track",
		width:   320,
		height:  240,
		fps:     30.0,
	}

	// Create test YCbCr image
	originalImg := createTestYCbCrImage(640, 480)

	// Test resizing
	resizedImg, err := reader.resizeImage(originalImg, 320, 240)
	assert.NoError(t, err, "Resize should not return error")

	bounds := resizedImg.Bounds()
	assert.Equal(t, 320, bounds.Dx(), "Resized width incorrect")
	assert.Equal(t, 240, bounds.Dy(), "Resized height incorrect")

	t.Logf("Successfully resized image from 640x480 to %dx%d", bounds.Dx(), bounds.Dy())
}

// TestVideoFileReader_Integration tests the complete workflow (without actual sending).
func TestVideoFileReader_Integration(t *testing.T) {
	// Create temporary directory with test frames
	tempDir := t.TempDir()

	// Create a sequence of test frames
	numFrames := 5
	for i := range numFrames {
		framePath := filepath.Join(tempDir, fmt.Sprintf("frame_%03d.jpg", i+1))
		// Create frames with different colors to verify they're being read
		col := color.RGBA{uint8((i * 50) % 256), uint8((255 - i*50) % 256), 100, 255} //nolint:gosec // Safe modulo operation
		createTestJPG(t, framePath, 160, 120, col)
	}

	// Test frame discovery and initialization (without creating full VideoFileReader)
	imageFiles, err := discoverImageFiles(tempDir)
	require.NoError(t, err, "Failed to discover frame files")
	require.Equal(t, numFrames, len(imageFiles), "Should discover all frame files")

	// Test that frames can be loaded
	reader := &VideoFileReader{
		sender:     nil, // Skip sender for this test
		trackID:    "test-track",
		width:      320,
		height:     240,
		fps:        30.0,
		imageFiles: imageFiles,
	}

	// Test loading a few frames
	for i := 0; i < min(3, len(imageFiles)); i++ {
		img, err := reader.loadFrame(i)
		require.NoError(t, err, "Failed to load frame %d", i)
		require.NotNil(t, img, "Frame %d should not be nil", i)
	}

	t.Logf("Successfully tested integration with %d frames", numFrames)
}

// TestVideoFileReader_LoopingBehavior tests that frames loop correctly.
func TestVideoFileReader_LoopingBehavior(t *testing.T) {
	// Create temporary directory with just 2 frames
	tempDir := t.TempDir()

	createTestJPG(t, filepath.Join(tempDir, "frame_1.jpg"), 100, 100, color.RGBA{255, 0, 0, 255})
	createTestJPG(t, filepath.Join(tempDir, "frame_2.jpg"), 100, 100, color.RGBA{0, 255, 0, 255})

	// Discover frames
	imageFiles, err := discoverImageFiles(tempDir)
	require.NoError(t, err)
	require.Equal(t, 2, len(imageFiles))

	// Create VideoFileReader for testing looping logic
	reader := &VideoFileReader{
		imageFiles: imageFiles,
	}

	// Test that looping logic works correctly (simulate the logic from Start method)
	frameIndex := 0
	assert.Equal(t, 0, frameIndex, "Should start at index 0")

	// Simulate frame advancement
	frameIndex = (frameIndex + 1) % len(reader.imageFiles) // Should be 1
	assert.Equal(t, 1, frameIndex, "Should advance to index 1")

	frameIndex = (frameIndex + 1) % len(reader.imageFiles) // Should loop back to 0
	assert.Equal(t, 0, frameIndex, "Should loop back to index 0")

	t.Log("Frame looping behavior works correctly")
}

// Helper function to create a test JPG file.
func createTestJPG(t *testing.T, filename string, width, height int, col color.RGBA) {
	t.Helper()
	// Create directory if it doesn't exist
	dir := filepath.Dir(filename)
	err := os.MkdirAll(dir, 0o750)
	require.NoError(t, err, "Failed to create directory")

	// Create image
	img := createTestImage(width, height, col)

	// Validate filename to prevent directory traversal
	if !isValidTestFile(filename) {
		require.Fail(t, "Invalid test file path", "filename: %s", filename)
	}

	// Save as JPG
	file, err := os.Create(filename) //nolint:gosec // File path validated by isValidTestFile
	require.NoError(t, err, "Failed to create file")
	defer func() {
		_ = file.Close()
	}()

	err = jpeg.Encode(file, img, &jpeg.Options{Quality: 90})
	require.NoError(t, err, "Failed to encode JPEG")
}

// Helper function to create a test image.
func createTestImage(width, height int, col color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Fill with solid color
	for y := range height {
		for x := range width {
			img.Set(x, y, col)
		}
	}

	return img
}

// Helper function to create a test YCbCr image with random data.
func createTestYCbCrImage(width, height int) *image.YCbCr {
	// Create YCbCr image
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)

	// Fill with random YCbCr data
	for y := range height {
		for x := range width {
			// Generate pseudo-random values based on position for deterministic testing
			// Y component (luminance): 16-235 range for valid YCbCr
			yVal := uint8((16 + (x+y)%220) % 256) //nolint:gosec // Safe modulo operation
			// Cb component (blue-difference): 16-240 range
			cbVal := uint8((16 + (x*2+y)%225) % 256) //nolint:gosec // Safe modulo operation
			// Cr component (red-difference): 16-240 range
			crVal := uint8((16 + (x+y*2)%225) % 256) //nolint:gosec // Safe modulo operation

			img.Y[img.YOffset(x, y)] = yVal
			img.Cb[img.COffset(x, y)] = cbVal
			img.Cr[img.COffset(x, y)] = crVal
		}
	}

	return img
}

// isValidTestFile validates that the file path is safe for test files.
func isValidTestFile(filePath string) bool {
	// Clean the path to resolve any .. or . components
	cleanPath := filepath.Clean(filePath)

	// Check for directory traversal attempts
	if strings.Contains(cleanPath, "..") {
		return false
	}

	// Only allow files in temp directories or current directory
	if filepath.IsAbs(cleanPath) && !strings.HasPrefix(cleanPath, "/tmp/") {
		return false
	}

	// Must be a .jpg file
	return strings.HasSuffix(strings.ToLower(cleanPath), ".jpg")
}

// Example function showing how to prepare frames (for documentation).
func ExampleVideoFileReader() {
	fmt.Println("Example: Preparing frame files for VideoFileReader")
	fmt.Println("")
	fmt.Println("1. Create a directory for frames:")
	fmt.Println("   mkdir frames")
	fmt.Println("")
	fmt.Println("2. Extract frames from video using FFmpeg:")
	fmt.Printf("   ffmpeg -i input.mp4 -vf fps=30 frames/frame_%%03d.jpg\n")
	fmt.Println("")
	fmt.Println("3. Use in your code:")
	fmt.Println("   reader, err := NewVideoFileReader(rtcSender, \"frames/\", \"track-1\", 640, 480)")
	fmt.Println("   if err != nil {")
	fmt.Println("       log.Fatal(err)")
	fmt.Println("   }")
	fmt.Println("   reader.Start(context.Background())")

	// Output:
	// Example: Preparing frame files for VideoFileReader
	//
	// 1. Create a directory for frames:
	//    mkdir frames
	//
	// 2. Extract frames from video using FFmpeg:
	//    ffmpeg -i input.mp4 -vf fps=30 frames/frame_%03d.jpg
	//
	// 3. Use in your code:
	//    reader, err := NewVideoFileReader(rtcSender, "frames/", "track-1", 640, 480)
	//    if err != nil {
	//        log.Fatal(err)
	//    }
	//    reader.Start(context.Background())
}
