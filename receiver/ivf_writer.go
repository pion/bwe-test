// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package receiver

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

var ErrTooManyFrames = errors.New("too many frames")

// IVFWriter creates valid IVF formatted files from VP8 frames.
type IVFWriter struct {
	writer        io.Writer
	headerWritten bool
	frameCount    uint64
	width         uint16
	height        uint16
	seekable      bool   // Indicates if the writer supports seeking (for updating frame count)
	firstKeyframe []byte // Store first keyframe for possible rewriting
}

// NewIVFWriter creates a new IVF writer.
func NewIVFWriter(w io.Writer, width, height uint16) (*IVFWriter, error) {
	ivf := &IVFWriter{
		writer:        w,
		headerWritten: false,
		frameCount:    0,
		width:         width,
		height:        height,
		seekable:      false,
		firstKeyframe: nil,
	}

	// Check if the writer supports seeking
	if _, ok := w.(*os.File); ok {
		ivf.seekable = true
	}

	// Write header immediately
	if err := ivf.WriteHeader(); err != nil {
		return nil, err
	}

	return ivf, nil
}

// WriteHeader writes the IVF file header.
func (i *IVFWriter) WriteHeader() error {
	if i.headerWritten {
		return nil
	}

	if err := i.writeSignatureAndVersion(); err != nil {
		return err
	}

	if err := i.writeCodecInfo(); err != nil {
		return err
	}

	if err := i.writeVideoParams(); err != nil {
		return err
	}

	if err := i.writeTimebaseAndCounters(); err != nil {
		return err
	}

	i.headerWritten = true

	return nil
}

// writeSignatureAndVersion writes the IVF signature and version.
func (i *IVFWriter) writeSignatureAndVersion() error {
	// IVF signature: "DKIF"
	signature := []byte{'D', 'K', 'I', 'F'}
	if _, err := i.writer.Write(signature); err != nil {
		return err
	}

	// Version: 0
	version := uint16(0)
	if err := binary.Write(i.writer, binary.LittleEndian, &version); err != nil {
		return err
	}

	// Header length: 32
	headerLength := uint16(32)

	return binary.Write(i.writer, binary.LittleEndian, &headerLength)
}

// writeCodecInfo writes the codec FourCC.
func (i *IVFWriter) writeCodecInfo() error {
	// Codec FourCC: "VP80"
	fourcc := []byte{'V', 'P', '8', '0'}
	_, err := i.writer.Write(fourcc)

	return err
}

// writeVideoParams writes width and height.
func (i *IVFWriter) writeVideoParams() error {
	if err := binary.Write(i.writer, binary.LittleEndian, &i.width); err != nil {
		return err
	}

	return binary.Write(i.writer, binary.LittleEndian, &i.height)
}

// writeTimebaseAndCounters writes timebase and frame counters.
func (i *IVFWriter) writeTimebaseAndCounters() error {
	// Timebase numerator and denominator (30fps)
	timebaseNum := uint32(1)
	timebaseDen := uint32(30)
	if err := binary.Write(i.writer, binary.LittleEndian, &timebaseNum); err != nil {
		return err
	}
	if err := binary.Write(i.writer, binary.LittleEndian, &timebaseDen); err != nil {
		return err
	}

	// Frame count placeholder (will be updated at the end)
	frameCount := uint32(0)
	if err := binary.Write(i.writer, binary.LittleEndian, &frameCount); err != nil {
		return err
	}

	// Reserved
	reserved := uint32(0)

	return binary.Write(i.writer, binary.LittleEndian, &reserved)
}

// WriteFrame writes a VP8 frame to the IVF file.
func (i *IVFWriter) WriteFrame(frame []byte, timestamp uint64) error {
	if !i.headerWritten {
		if err := i.WriteHeader(); err != nil {
			return err
		}
	}

	// Store the first keyframe for possible rewriting
	if i.frameCount == 0 && IsKeyframe(frame) {
		i.firstKeyframe = make([]byte, len(frame))
		copy(i.firstKeyframe, frame)
	}

	frameSize := uint32(len(frame)) // #nosec G115 - len(frame) is always within uint32 range in practice
	if err := binary.Write(i.writer, binary.LittleEndian, &frameSize); err != nil {
		return err
	}

	// Timestamp
	if err := binary.Write(i.writer, binary.LittleEndian, &timestamp); err != nil {
		return err
	}

	// Frame data
	if _, err := i.writer.Write(frame); err != nil {
		return err
	}

	i.frameCount++

	return nil
}

// Close finalizes the IVF file, updating the frame count in the header if possible.
func (i *IVFWriter) Close() error {
	// Only attempt to update the frame count if the writer is seekable
	if i.seekable {
		if err := i.updateFrameCountInHeader(); err != nil {
			return err
		}
	}

	// Close the underlying writer if it implements io.Closer
	if closer, ok := i.writer.(io.Closer); ok {
		return closer.Close()
	}

	return nil
}

// updateFrameCountInHeader updates the frame count in the IVF header for seekable files.
func (i *IVFWriter) updateFrameCountInHeader() error {
	file, ok := i.writer.(*os.File)
	if !ok {
		return nil // Not a file, skip update
	}

	// Go back to the frame count position in the header (24 bytes from start)
	_, err := file.Seek(24, io.SeekStart)
	if err != nil {
		return err // Return seek error
	}

	// Write the actual frame count with overflow check
	if i.frameCount > uint64(^uint32(0)) {
		return fmt.Errorf("frame count exceeds maximum uint32 value: %w", ErrTooManyFrames)
	}
	frameCount := uint32(i.frameCount)

	return binary.Write(file, binary.LittleEndian, &frameCount)
}
