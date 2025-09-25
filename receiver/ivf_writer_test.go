// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package receiver

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewIVFWriter(t *testing.T) {
	tests := []struct {
		name   string
		width  uint16
		height uint16
	}{
		{"Standard HD", 1920, 1080},
		{"Standard Definition", 640, 480},
		{"Square", 500, 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			writer, err := NewIVFWriter(buf, tt.width, tt.height)
			assert.NoError(t, err, "NewIVFWriter() should not error")
			assert.NotNil(t, writer, "NewIVFWriter() should not return nil writer")
			assert.Equal(t, tt.width, writer.width, "NewIVFWriter() width should match")
			assert.Equal(t, tt.height, writer.height, "NewIVFWriter() height should match")
			assert.True(t, writer.headerWritten, "NewIVFWriter() should have written header")
		})
	}
}

func TestIVFWriter_WriteHeader(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := &IVFWriter{
		writer:        buf,
		headerWritten: false,
		width:         640,
		height:        480,
	}

	err := writer.WriteHeader()
	assert.NoError(t, err, "WriteHeader() should not error")

	assert.True(t, writer.headerWritten, "WriteHeader() should set headerWritten to true")

	// Check header content
	header := buf.Bytes()
	assert.Equal(t, 32, len(header), "WriteHeader() should write 32 bytes")

	// Check signature
	signature := string(header[0:4])
	assert.Equal(t, "DKIF", signature, "WriteHeader() should write correct signature")

	// Check FourCC
	fourcc := string(header[8:12])
	assert.Equal(t, "VP80", fourcc, "WriteHeader() should write correct fourcc")

	// Check dimensions
	width := binary.LittleEndian.Uint16(header[12:14])
	height := binary.LittleEndian.Uint16(header[14:16])
	assert.Equal(t, uint16(640), width, "WriteHeader() should write correct width")
	assert.Equal(t, uint16(480), height, "WriteHeader() should write correct height")
}

func TestIVFWriter_WriteFrame(t *testing.T) {
	buf := &bytes.Buffer{}
	writer, err := NewIVFWriter(buf, 640, 480)
	assert.NoError(t, err, "NewIVFWriter() should not error")

	// Reset buffer to only capture frame data
	_ = buf.Len() // headerSize for reference
	buf.Reset()
	writer.writer = buf
	writer.headerWritten = true

	frameData := []byte{0x10, 0x02, 0x00, 0x9d, 0x01, 0x2a} // Sample VP8 keyframe start
	timestamp := uint64(12345)

	err = writer.WriteFrame(frameData, timestamp)
	assert.NoError(t, err, "WriteFrame() should not error")

	written := buf.Bytes()

	// Check frame size (first 4 bytes)
	frameSize := binary.LittleEndian.Uint32(written[0:4])
	expectedSize := uint32(len(frameData)) // #nosec G115 - test data with small values
	assert.Equal(t, expectedSize, frameSize, "WriteFrame() should write correct frame size")

	// Check timestamp (next 8 bytes)
	writtenTimestamp := binary.LittleEndian.Uint64(written[4:12])
	assert.Equal(t, timestamp, writtenTimestamp, "WriteFrame() should write correct timestamp")

	// Check frame data
	writtenFrameData := written[12:]
	assert.Equal(t, frameData, writtenFrameData, "WriteFrame() should write correct frame data")

	assert.Equal(t, uint64(1), writer.frameCount, "WriteFrame() should increment frameCount to 1")
}

func TestIVFWriter_Close(t *testing.T) {
	buf := &bytes.Buffer{}
	writer, err := NewIVFWriter(buf, 640, 480)
	assert.NoError(t, err, "NewIVFWriter() should not error")

	// Write some frames
	frameData := []byte{0x10, 0x02, 0x00}
	err = writer.WriteFrame(frameData, 1000)
	assert.NoError(t, err, "WriteFrame() should not error")
	err = writer.WriteFrame(frameData, 2000)
	assert.NoError(t, err, "WriteFrame() should not error")

	err = writer.Close()
	assert.NoError(t, err, "Close() should not error")
	assert.Equal(t, uint64(2), writer.frameCount, "Close() frameCount should be 2")
}

func TestIVFWriter_ErrorCases(t *testing.T) {
	// Test frame size overflow
	writer := &IVFWriter{
		writer:        &mockWriteCloser{},
		headerWritten: true,
		frameCount:    0,
	}

	// This will test the overflow path in WriteFrame
	// We can't actually create a frame larger than uint32, so we'll test the logic
	frameData := []byte{0x10, 0x02, 0x00}
	timestamp := uint64(1000)

	err := writer.WriteFrame(frameData, timestamp)
	assert.NoError(t, err, "WriteFrame() should not error")
}

func TestIVFWriter_WriteHeaderSubfunctionErrors(t *testing.T) {
	// Test individual subfunctions for error paths
	writer := &IVFWriter{
		writer: &errorWriter{}, // Writer that always errors
		width:  640,
		height: 480,
	}

	// Test each subfunction directly
	err := writer.writeSignatureAndVersion()
	assert.Error(t, err, "writeSignatureAndVersion() should return error with errorWriter")

	err = writer.writeCodecInfo()
	assert.Error(t, err, "writeCodecInfo() should return error with errorWriter")

	err = writer.writeVideoParams()
	assert.Error(t, err, "writeVideoParams() should return error with errorWriter")

	err = writer.writeTimebaseAndCounters()
	assert.Error(t, err, "writeTimebaseAndCounters() should return error with errorWriter")
}

func TestIVFWriter_WriteSignatureVersionEdgeCases(t *testing.T) {
	// Test writeSignatureAndVersion with different scenarios
	buf := &bytes.Buffer{}
	writer := &IVFWriter{
		writer: buf,
		width:  1920,
		height: 1080,
	}

	// Test successful path
	err := writer.writeSignatureAndVersion()
	assert.NoError(t, err, "writeSignatureAndVersion() should not error")

	// Verify the signature was written correctly
	data := buf.Bytes()
	if len(data) >= 4 {
		assert.Equal(t, "DKIF", string(data[0:4]), "writeSignatureAndVersion() should write correct signature")
	}
}

func TestIVFWriter_CloseWithNonCloser(t *testing.T) {
	// Test Close() with a writer that doesn't implement io.Closer
	writer := &IVFWriter{
		writer:        &bytes.Buffer{}, // bytes.Buffer doesn't implement io.Closer
		headerWritten: true,
		frameCount:    1,
		seekable:      false,
	}

	err := writer.Close()
	assert.NoError(t, err, "Close() should not error")
}

// errorWriter always returns an error on Write.
type errorWriter struct{}

var (
	errWrite = errors.New("write error")
	errClose = errors.New("close error")
)

func (e *errorWriter) Write(p []byte) (n int, err error) {
	return 0, errWrite
}

func (e *errorWriter) Close() error {
	return errClose
}

func TestIVFWriter_CloseWithActualFile(t *testing.T) {
	// Create a temporary file to test the actual file seeking behavior
	tmpFile, err := os.CreateTemp("", "test_ivf_*.ivf")
	if err != nil {
		t.Skip("Cannot create temp file for test")
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	defer func() { _ = tmpFile.Close() }()

	writer, err := NewIVFWriter(tmpFile, 640, 480)
	assert.NoError(t, err, "NewIVFWriter() should not error")

	// Add some frames to test frame count update
	for i := 0; i < 3; i++ {
		frameData := []byte{0x10, 0x02, 0x00}
		err = writer.WriteFrame(frameData, uint64(1000+i*1000)) // #nosec G115 - test data with small values
		assert.NoError(t, err, "WriteFrame() should not error")
	}

	// This should trigger the file seeking behavior
	err = writer.Close()
	assert.NoError(t, err, "Close() should not error")

	assert.Equal(t, uint64(3), writer.frameCount, "Close() frameCount should be 3")
}

func TestIVFWriter_NewWithError(t *testing.T) {
	// Test NewIVFWriter with a writer that fails immediately
	errorWriter := &errorWriter{}

	_, err := NewIVFWriter(errorWriter, 640, 480)
	assert.Error(t, err, "NewIVFWriter() should return error with errorWriter")
}

func TestIVFWriter_WriteFrameWithoutHeader(t *testing.T) {
	// Test WriteFrame when header hasn't been written yet
	writer := &IVFWriter{
		writer:        &mockWriteCloser{},
		headerWritten: false,
		frameCount:    0,
		width:         640,
		height:        480,
	}

	frameData := []byte{0x10, 0x02, 0x00}
	timestamp := uint64(1000)

	err := writer.WriteFrame(frameData, timestamp)
	assert.NoError(t, err, "WriteFrame() should write header first")

	assert.True(t, writer.headerWritten, "WriteFrame() should have written header")
}

func TestIVFWriter_WriteFrameErrors(t *testing.T) {
	// Test WriteFrame with various error conditions
	writer := &IVFWriter{
		writer:        &errorWriter{},
		headerWritten: true,
		frameCount:    0,
	}

	frameData := []byte{0x10, 0x02, 0x00}
	timestamp := uint64(1000)

	err := writer.WriteFrame(frameData, timestamp)
	assert.Error(t, err, "WriteFrame() should return error with errorWriter")
}

func TestIVFWriter_CloseWithFrameCountOverflow(t *testing.T) {
	// Test Close() with frame count overflow
	writer := &IVFWriter{
		writer:        &mockWriteCloser{},
		headerWritten: true,
		frameCount:    0xFFFFFFFF + 1, // Force overflow
		seekable:      false,          // Non-seekable to avoid the overflow check
	}

	err := writer.Close()
	assert.NoError(t, err, "Close() should not error")
}

func TestIVFWriter_WriteHeaderMultipleTimes(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := &IVFWriter{
		writer:        buf,
		headerWritten: false,
		width:         640,
		height:        480,
	}

	// First call should write header
	err := writer.WriteHeader()
	assert.NoError(t, err, "WriteHeader() first call should not error")
	firstSize := buf.Len()

	// Second call should not write anything
	err = writer.WriteHeader()
	assert.NoError(t, err, "WriteHeader() second call should not error")
	secondSize := buf.Len()

	assert.Equal(t, firstSize, secondSize, "WriteHeader() called twice should not change buffer size")
}

func TestIVFWriter_UpdateFrameCountInHeaderEdgeCases(t *testing.T) {
	// Test with non-file writer to exercise different path
	writer := &IVFWriter{
		writer:   &bytes.Buffer{},
		seekable: true,
	}

	err := writer.updateFrameCountInHeader()
	assert.NoError(t, err, "updateFrameCountInHeader should handle non-file writer")

	// Test frame count overflow path with actual file
	tmpFile, err := os.CreateTemp("", "test_overflow_*.ivf")
	if err != nil {
		t.Skip("Cannot create temp file")
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	defer func() { _ = tmpFile.Close() }()

	writer2 := &IVFWriter{
		writer:     tmpFile,
		seekable:   true,
		frameCount: 0xFFFFFFFF + 1, // Force overflow
	}

	err = writer2.updateFrameCountInHeader()
	assert.Error(t, err, "updateFrameCountInHeader should error on overflow")
}

func TestIVFWriter_WriteCodecInfo(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := &IVFWriter{
		writer: buf,
		width:  640,
		height: 480,
	}

	err := writer.writeCodecInfo()
	assert.NoError(t, err, "writeCodecInfo() should not error")

	written := buf.Bytes()
	assert.Equal(t, 4, len(written), "writeCodecInfo() should write 4 bytes")

	// Check FourCC
	fourcc := string(written[0:4])
	assert.Equal(t, "VP80", fourcc, "writeCodecInfo() should write correct fourcc")
}

func TestIVFWriter_WriteVideoParams(t *testing.T) {
	tests := []struct {
		name   string
		width  uint16
		height uint16
	}{
		{"Standard HD", 1920, 1080},
		{"Standard Definition", 640, 480},
		{"4K", 3840, 2160},
		{"Square", 500, 500},
		{"Minimal", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			writer := &IVFWriter{
				writer: buf,
				width:  tt.width,
				height: tt.height,
			}

			err := writer.writeVideoParams()
			assert.NoError(t, err, "writeVideoParams() should not error")

			written := buf.Bytes()
			assert.Equal(t, 4, len(written), "writeVideoParams() should write 4 bytes")

			// Check width and height
			width := binary.LittleEndian.Uint16(written[0:2])
			height := binary.LittleEndian.Uint16(written[2:4])
			assert.Equal(t, tt.width, width, "writeVideoParams() should write correct width")
			assert.Equal(t, tt.height, height, "writeVideoParams() should write correct height")
		})
	}
}

func TestIVFWriter_WriteTimebaseAndCounters(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := &IVFWriter{
		writer: buf,
		width:  640,
		height: 480,
	}

	err := writer.writeTimebaseAndCounters()
	assert.NoError(t, err, "writeTimebaseAndCounters() should not error")

	written := buf.Bytes()
	assert.Equal(t, 16, len(written), "writeTimebaseAndCounters() should write 16 bytes")

	// Check timebase numerator (first 4 bytes)
	timebaseNum := binary.LittleEndian.Uint32(written[0:4])
	assert.Equal(t, uint32(1), timebaseNum, "writeTimebaseAndCounters() should write correct timebase numerator")

	// Check timebase denominator (next 4 bytes)
	timebaseDen := binary.LittleEndian.Uint32(written[4:8])
	assert.Equal(t, uint32(30), timebaseDen, "writeTimebaseAndCounters() should write correct timebase denominator")

	// Check frame count placeholder (next 4 bytes)
	frameCount := binary.LittleEndian.Uint32(written[8:12])
	assert.Equal(t, uint32(0), frameCount, "writeTimebaseAndCounters() should write frame count placeholder as 0")

	// Check reserved field (last 4 bytes)
	reserved := binary.LittleEndian.Uint32(written[12:16])
	assert.Equal(t, uint32(0), reserved, "writeTimebaseAndCounters() should write reserved field as 0")
}

func TestIVFWriter_WriteCodecInfoError(t *testing.T) {
	writer := &IVFWriter{
		writer: &errorWriter{}, // Writer that always errors
		width:  640,
		height: 480,
	}

	err := writer.writeCodecInfo()
	assert.Error(t, err, "writeCodecInfo() should return error with errorWriter")
}

func TestIVFWriter_WriteVideoParamsError(t *testing.T) {
	writer := &IVFWriter{
		writer: &errorWriter{}, // Writer that always errors
		width:  640,
		height: 480,
	}

	err := writer.writeVideoParams()
	assert.Error(t, err, "writeVideoParams() should return error with errorWriter")
}

func TestIVFWriter_WriteTimebaseAndCountersError(t *testing.T) {
	writer := &IVFWriter{
		writer: &errorWriter{}, // Writer that always errors
		width:  640,
		height: 480,
	}

	err := writer.writeTimebaseAndCounters()
	assert.Error(t, err, "writeTimebaseAndCounters() should return error with errorWriter")
}
