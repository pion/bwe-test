// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	plogging "github.com/pion/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSimpleFlow_ABRSenderMode(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Test ABR sender mode
	flow, err := NewSimpleFlow(loggerFactory, nm, 1, abrSenderMode, tempDir)
	require.NoError(t, err, "NewSimpleFlow() with ABR mode should not error")
	assert.NotNil(t, flow.sender, "Flow sender should not be nil")
	assert.NotNil(t, flow.receiver, "Flow receiver should not be nil")

	// Clean up
	err = flow.Close()
	assert.NoError(t, err, "Flow.Close() should not error")
}

func TestNewSimpleFlow_SimulcastSenderMode(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Test simulcast sender mode
	flow, err := NewSimpleFlow(loggerFactory, nm, 1, simulcastSenderMode, tempDir)
	require.NoError(t, err, "NewSimpleFlow() with simulcast mode should not error")
	assert.NotNil(t, flow.sender, "Flow sender should not be nil")
	assert.NotNil(t, flow.receiver, "Flow receiver should not be nil")

	// Clean up
	err = flow.Close()
	assert.NoError(t, err, "Flow.Close() should not error")
}

func TestNewSimpleFlow_VideoFileEncoderMode_WithVideoFiles(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Create test frame directories with actual frame files
	frameDir1 := filepath.Join(tempDir, "test_video1")
	frameDir2 := filepath.Join(tempDir, "test_video2")

	err = os.MkdirAll(frameDir1, 0o750)
	require.NoError(t, err, "Should create frame directory 1")

	err = os.MkdirAll(frameDir2, 0o750)
	require.NoError(t, err, "Should create frame directory 2")

	// Create test frame files
	for i := 1; i <= 3; i++ {
		framePath1 := filepath.Join(frameDir1, fmt.Sprintf("frame_%03d.jpg", i))
		framePath2 := filepath.Join(frameDir2, fmt.Sprintf("frame_%03d.jpg", i))

		// Create dummy JPG files
		err = os.WriteFile(framePath1, []byte("dummy jpg data"), 0o600)
		require.NoError(t, err, "Should create test frame file 1")

		err = os.WriteFile(framePath2, []byte("dummy jpg data"), 0o600)
		require.NoError(t, err, "Should create test frame file 2")
	}

	// Create test video files
	videoFiles := []VideoFileInfo{
		{
			FilePath: frameDir1,
			TrackID:  "track-1",
			Width:    640,
			Height:   480,
		},
		{
			FilePath: frameDir2,
			TrackID:  "track-2",
			Width:    1280,
			Height:   720,
		},
	}

	// Test video file encoder mode with video files
	flow, err := NewSimpleFlow(loggerFactory, nm, 1, videoFileEncoderMode, tempDir, videoFiles...)
	require.NoError(t, err, "NewSimpleFlow() with video file mode should not error")
	assert.NotNil(t, flow.sender, "Flow sender should not be nil")
	assert.NotNil(t, flow.receiver, "Flow receiver should not be nil")

	// Verify that received video directory was created
	receivedDir := filepath.Join(tempDir, "received_video")
	_, err = os.Stat(receivedDir)
	assert.NoError(t, err, "Received video directory should be created")

	// Clean up
	err = flow.Close()
	assert.NoError(t, err, "Flow.Close() should not error")
}

func TestNewSimpleFlow_VideoFileEncoderMode_WithoutVideoFiles(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Test video file encoder mode without video files (should fail)
	_, err = NewSimpleFlow(loggerFactory, nm, 1, videoFileEncoderMode, tempDir)
	assert.Error(t, err, "NewSimpleFlow() with video file mode without files should error")
	assert.Contains(t, err.Error(), "videoFileEncoderMode requires at least one video file",
		"Error should mention missing video files")
}

func TestNewSimpleFlow_InvalidSenderMode(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Test with invalid sender mode
	invalidMode := senderMode(999)
	_, err = NewSimpleFlow(loggerFactory, nm, 1, invalidMode, tempDir)
	assert.Error(t, err, "NewSimpleFlow() with invalid sender mode should error")
	assert.Contains(t, err.Error(), "unknown sender mode", "Error should mention unknown sender mode")
}

func TestFlow_Close(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Create flow
	flow, err := NewSimpleFlow(loggerFactory, nm, 1, abrSenderMode, tempDir)
	require.NoError(t, err, "NewSimpleFlow() should not error")

	// Test close
	err = flow.Close()
	assert.NoError(t, err, "Flow.Close() should not error")

	// Test multiple close calls (may error due to already closed files, which is expected)
	_ = flow.Close()
	// We don't assert on this since files may already be closed
}

func TestNewSender_ABRSenderMode(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Test ABR sender creation
	snd, err := newSender(loggerFactory, nm, 1, abrSenderMode, tempDir)
	require.NoError(t, err, "newSender() with ABR mode should not error")
	assert.NotNil(t, snd.sender, "Sender should not be nil")
	assert.NotNil(t, snd.ccLogger, "CC logger should not be nil")
	assert.NotNil(t, snd.senderRTPLogger, "Sender RTP logger should not be nil")
	assert.NotNil(t, snd.senderRTCPLogger, "Sender RTCP logger should not be nil")

	// Clean up
	err = snd.Close()
	assert.NoError(t, err, "Sender.Close() should not error")
}

func TestNewSender_SimulcastSenderMode(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Test simulcast sender creation
	snd, err := newSender(loggerFactory, nm, 1, simulcastSenderMode, tempDir)
	require.NoError(t, err, "newSender() with simulcast mode should not error")
	assert.NotNil(t, snd.sender, "Sender should not be nil")
	assert.NotNil(t, snd.ccLogger, "CC logger should not be nil")
	assert.NotNil(t, snd.senderRTPLogger, "Sender RTP logger should not be nil")
	assert.NotNil(t, snd.senderRTCPLogger, "Sender RTCP logger should not be nil")

	// Clean up
	err = snd.Close()
	assert.NoError(t, err, "Sender.Close() should not error")
}

func TestNewSender_VideoFileEncoderMode(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Create test frame directory with actual frame files
	frameDir := filepath.Join(tempDir, "test_video")
	err = os.MkdirAll(frameDir, 0o750)
	require.NoError(t, err, "Should create frame directory")

	// Create test frame files
	for i := 1; i <= 3; i++ {
		framePath := filepath.Join(frameDir, fmt.Sprintf("frame_%03d.jpg", i))
		err = os.WriteFile(framePath, []byte("dummy jpg data"), 0o600)
		require.NoError(t, err, "Should create test frame file")
	}

	// Create test video files
	videoFiles := []VideoFileInfo{
		{
			FilePath: frameDir,
			TrackID:  "track-1",
			Width:    640,
			Height:   480,
		},
	}

	// Test video file encoder sender creation
	snd, err := newSender(loggerFactory, nm, 1, videoFileEncoderMode, tempDir, videoFiles...)
	require.NoError(t, err, "newSender() with video file mode should not error")
	assert.NotNil(t, snd.sender, "Sender should not be nil")
	assert.NotNil(t, snd.ccLogger, "CC logger should not be nil")
	assert.NotNil(t, snd.senderRTPLogger, "Sender RTP logger should not be nil")
	assert.NotNil(t, snd.senderRTCPLogger, "Sender RTCP logger should not be nil")

	// Clean up
	err = snd.Close()
	assert.NoError(t, err, "Sender.Close() should not error")
}

func TestNewSender_VideoFileEncoderMode_WithoutVideoFiles(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Test video file encoder sender creation without video files
	_, err = newSender(loggerFactory, nm, 1, videoFileEncoderMode, tempDir)
	assert.Error(t, err, "newSender() with video file mode without files should error")
	assert.Contains(t, err.Error(), "videoFileEncoderMode requires at least one video file",
		"Error should mention missing video files")
}

func TestNewSender_InvalidSenderMode(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Test with invalid sender mode
	invalidMode := senderMode(999)
	_, err = newSender(loggerFactory, nm, 1, invalidMode, tempDir)
	assert.Error(t, err, "newSender() with invalid sender mode should error")
	assert.Contains(t, err.Error(), "unknown sender mode", "Error should mention unknown sender mode")
}

func TestCreateSenderLoggers(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Test logger creation
	loggers, err := createSenderLoggers(tempDir, 1)
	require.NoError(t, err, "createSenderLoggers() should not error")
	assert.NotNil(t, loggers.ccLogger, "CC logger should not be nil")
	assert.NotNil(t, loggers.rtpLogger, "RTP logger should not be nil")
	assert.NotNil(t, loggers.rtcpLogger, "RTCP logger should not be nil")

	// Verify log files were created
	ccLogPath := filepath.Join(tempDir, "1_cc.log")
	rtpLogPath := filepath.Join(tempDir, "1_sender_rtp.log")
	rtcpLogPath := filepath.Join(tempDir, "1_sender_rtcp.log")

	_, err = os.Stat(ccLogPath)
	assert.NoError(t, err, "CC log file should be created")

	_, err = os.Stat(rtpLogPath)
	assert.NoError(t, err, "RTP log file should be created")

	_, err = os.Stat(rtcpLogPath)
	assert.NoError(t, err, "RTCP log file should be created")

	// Clean up
	err = loggers.ccLogger.Close()
	assert.NoError(t, err, "CC logger should close without error")

	err = loggers.rtpLogger.Close()
	assert.NoError(t, err, "RTP logger should close without error")

	err = loggers.rtcpLogger.Close()
	assert.NoError(t, err, "RTCP logger should close without error")
}

func TestCreateWebRTCSender_ABRSenderMode(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Get left network
	leftVnet, publicIPLeft, err := nm.GetLeftNet()
	require.NoError(t, err, "GetLeftNet() should not error")

	// Create loggers
	loggers, err := createSenderLoggers(tempDir, 1)
	require.NoError(t, err, "createSenderLoggers() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Test ABR sender creation
	webrtcSender, err := createWebRTCSender(abrSenderMode, leftVnet, publicIPLeft, loggers, loggerFactory)
	require.NoError(t, err, "createWebRTCSender() with ABR mode should not error")
	assert.NotNil(t, webrtcSender, "WebRTC sender should not be nil")

	// Verify sender implements the interface
	_ = webrtcSender

	// Note: WebRTCSender interface doesn't have a Close method
	// Clean up loggers
	err = loggers.ccLogger.Close()
	assert.NoError(t, err, "CC logger should close without error")

	err = loggers.rtpLogger.Close()
	assert.NoError(t, err, "RTP logger should close without error")

	err = loggers.rtcpLogger.Close()
	assert.NoError(t, err, "RTCP logger should close without error")
}

func TestCreateWebRTCSender_SimulcastSenderMode(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Get left network
	leftVnet, publicIPLeft, err := nm.GetLeftNet()
	require.NoError(t, err, "GetLeftNet() should not error")

	// Create loggers
	loggers, err := createSenderLoggers(tempDir, 1)
	require.NoError(t, err, "createSenderLoggers() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Test simulcast sender creation
	webrtcSender, err := createWebRTCSender(simulcastSenderMode, leftVnet, publicIPLeft, loggers, loggerFactory)
	require.NoError(t, err, "createWebRTCSender() with simulcast mode should not error")
	assert.NotNil(t, webrtcSender, "WebRTC sender should not be nil")

	// Verify sender implements the interface
	_ = webrtcSender

	// Note: WebRTCSender interface doesn't have a Close method
	// Clean up loggers
	err = loggers.ccLogger.Close()
	assert.NoError(t, err, "CC logger should close without error")

	err = loggers.rtpLogger.Close()
	assert.NoError(t, err, "RTP logger should close without error")

	err = loggers.rtcpLogger.Close()
	assert.NoError(t, err, "RTCP logger should close without error")
}

func TestCreateWebRTCSender_VideoFileEncoderMode(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Get left network
	leftVnet, publicIPLeft, err := nm.GetLeftNet()
	require.NoError(t, err, "GetLeftNet() should not error")

	// Create loggers
	loggers, err := createSenderLoggers(tempDir, 1)
	require.NoError(t, err, "createSenderLoggers() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Create test frame directory with actual frame files
	frameDir := filepath.Join(tempDir, "test_video")
	err = os.MkdirAll(frameDir, 0o750)
	require.NoError(t, err, "Should create frame directory")

	// Create test frame files
	for i := 1; i <= 3; i++ {
		framePath := filepath.Join(frameDir, fmt.Sprintf("frame_%03d.jpg", i))
		err = os.WriteFile(framePath, []byte("dummy jpg data"), 0o600)
		require.NoError(t, err, "Should create test frame file")
	}

	// Create test video files
	videoFiles := []VideoFileInfo{
		{
			FilePath: frameDir,
			TrackID:  "track-1",
			Width:    640,
			Height:   480,
		},
	}

	// Test video file encoder sender creation
	webrtcSender, err := createWebRTCSender(videoFileEncoderMode, leftVnet, publicIPLeft, loggers,
		loggerFactory, videoFiles...)
	require.NoError(t, err, "createWebRTCSender() with video file mode should not error")
	assert.NotNil(t, webrtcSender, "WebRTC sender should not be nil")

	// Verify sender implements the interface
	_ = webrtcSender

	// Note: WebRTCSender interface doesn't have a Close method
	// Clean up loggers
	err = loggers.ccLogger.Close()
	assert.NoError(t, err, "CC logger should close without error")

	err = loggers.rtpLogger.Close()
	assert.NoError(t, err, "RTP logger should close without error")

	err = loggers.rtcpLogger.Close()
	assert.NoError(t, err, "RTCP logger should close without error")
}

func TestCreateWebRTCSender_VideoFileEncoderMode_WithoutVideoFiles(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Get left network
	leftVnet, publicIPLeft, err := nm.GetLeftNet()
	require.NoError(t, err, "GetLeftNet() should not error")

	// Create loggers
	loggers, err := createSenderLoggers(tempDir, 1)
	require.NoError(t, err, "createSenderLoggers() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Test video file encoder sender creation without video files
	_, err = createWebRTCSender(videoFileEncoderMode, leftVnet, publicIPLeft, loggers, loggerFactory)
	assert.Error(t, err, "createWebRTCSender() with video file mode without files should error")
	assert.Contains(t, err.Error(), "videoFileEncoderMode requires at least one video file",
		"Error should mention missing video files")

	// Clean up
	err = loggers.ccLogger.Close()
	assert.NoError(t, err, "CC logger should close without error")

	err = loggers.rtpLogger.Close()
	assert.NoError(t, err, "RTP logger should close without error")

	err = loggers.rtcpLogger.Close()
	assert.NoError(t, err, "RTCP logger should close without error")
}

func TestCreateWebRTCSender_InvalidSenderMode(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Get left network
	leftVnet, publicIPLeft, err := nm.GetLeftNet()
	require.NoError(t, err, "GetLeftNet() should not error")

	// Create loggers
	loggers, err := createSenderLoggers(tempDir, 1)
	require.NoError(t, err, "createSenderLoggers() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Test with invalid sender mode
	invalidMode := senderMode(999)
	_, err = createWebRTCSender(invalidMode, leftVnet, publicIPLeft, loggers, loggerFactory)
	assert.Error(t, err, "createWebRTCSender() with invalid sender mode should error")
	assert.Contains(t, err.Error(), "unknown sender mode", "Error should mention unknown sender mode")

	// Clean up
	err = loggers.ccLogger.Close()
	assert.NoError(t, err, "CC logger should close without error")

	err = loggers.rtpLogger.Close()
	assert.NoError(t, err, "RTP logger should close without error")

	err = loggers.rtcpLogger.Close()
	assert.NoError(t, err, "RTCP logger should close without error")
}

func TestNewReceiver(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Test receiver creation without output video path
	recv, err := newReceiver(nm, 1, tempDir, "", loggerFactory)
	require.NoError(t, err, "newReceiver() should not error")
	assert.NotNil(t, recv.receiver, "Receiver should not be nil")
	assert.NotNil(t, recv.receiverRTPLogger, "Receiver RTP logger should not be nil")
	assert.NotNil(t, recv.receiverRTCPLogger, "Receiver RTCP logger should not be nil")
	assert.Nil(t, recv.videoOutputFile, "Video output file should be nil when not specified")

	// Verify log files were created
	rtpLogPath := filepath.Join(tempDir, "1_receiver_rtp.log")
	rtcpLogPath := filepath.Join(tempDir, "1_receiver_rtcp.log")

	_, err = os.Stat(rtpLogPath)
	assert.NoError(t, err, "Receiver RTP log file should be created")

	_, err = os.Stat(rtcpLogPath)
	assert.NoError(t, err, "Receiver RTCP log file should be created")

	// Clean up - only close the loggers since the receiver may not be fully initialized
	err = recv.receiverRTPLogger.Close()
	assert.NoError(t, err, "Receiver RTP logger should close without error")

	err = recv.receiverRTCPLogger.Close()
	assert.NoError(t, err, "Receiver RTCP logger should close without error")
}

func TestNewReceiver_WithOutputVideoPath(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Test receiver creation with output video path
	outputVideoPath := filepath.Join(tempDir, "output.ivf")
	recv, err := newReceiver(nm, 1, tempDir, outputVideoPath, loggerFactory)
	require.NoError(t, err, "newReceiver() with output path should not error")
	assert.NotNil(t, recv.receiver, "Receiver should not be nil")
	assert.NotNil(t, recv.receiverRTPLogger, "Receiver RTP logger should not be nil")
	assert.NotNil(t, recv.receiverRTCPLogger, "Receiver RTCP logger should not be nil")
	assert.Nil(t, recv.videoOutputFile, "Video output file should be nil (handled internally)")

	// Clean up - only close the loggers since the receiver may not be fully initialized
	err = recv.receiverRTPLogger.Close()
	assert.NoError(t, err, "Receiver RTP logger should close without error")

	err = recv.receiverRTCPLogger.Close()
	assert.NoError(t, err, "Receiver RTCP logger should close without error")
}

func TestSndr_Close(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Create sender
	snd, err := newSender(loggerFactory, nm, 1, abrSenderMode, tempDir)
	require.NoError(t, err, "newSender() should not error")

	// Test close
	err = snd.Close()
	assert.NoError(t, err, "Sender.Close() should not error")

	// Test multiple close calls (may error due to already closed files, which is expected)
	_ = snd.Close()
	// We don't assert on this since files may already be closed
}

func TestRecv_Close(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Create receiver
	recv, err := newReceiver(nm, 1, tempDir, "", loggerFactory)
	require.NoError(t, err, "newReceiver() should not error")

	// Test close - only close the loggers since the receiver may not be fully initialized
	err = recv.receiverRTPLogger.Close()
	assert.NoError(t, err, "Receiver RTP logger should close without error")

	err = recv.receiverRTCPLogger.Close()
	assert.NoError(t, err, "Receiver RTCP logger should close without error")

	// Test multiple close calls (may error due to already closed files, which is expected)
	_ = recv.receiverRTPLogger.Close()
	// We don't assert on this since files may already be closed

	_ = recv.receiverRTCPLogger.Close()
	// We don't assert on this since files may already be closed
}

func TestFlowErrorConstants(t *testing.T) {
	// Test that error constants are properly defined
	assert.NotNil(t, errUnknownSenderMode, "errUnknownSenderMode should be defined")
	assert.NotNil(t, errVideoFileRequired, "errVideoFileRequired should be defined")

	// Test error messages
	assert.Contains(t, errUnknownSenderMode.Error(), "unknown sender mode",
		"Error message should mention unknown sender mode")
	assert.Contains(t, errVideoFileRequired.Error(), "videoFileEncoderMode requires at least one video file",
		"Error message should mention video file requirement")
}

func TestVideoFileInfo_Structure(t *testing.T) {
	// Test VideoFileInfo structure
	videoFile := VideoFileInfo{
		FilePath: "test_video.mp4",
		TrackID:  "track-1",
		Width:    640,
		Height:   480,
	}

	assert.Equal(t, "test_video.mp4", videoFile.FilePath, "FilePath should be set correctly")
	assert.Equal(t, "track-1", videoFile.TrackID, "TrackID should be set correctly")
	assert.Equal(t, 640, videoFile.Width, "Width should be set correctly")
	assert.Equal(t, 480, videoFile.Height, "Height should be set correctly")
}

func TestSenderLoggers_Structure(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Test sender loggers creation
	loggers, err := createSenderLoggers(tempDir, 1)
	require.NoError(t, err, "createSenderLoggers() should not error")

	// Test structure
	assert.NotNil(t, loggers.ccLogger, "CC logger should not be nil")
	assert.NotNil(t, loggers.rtpLogger, "RTP logger should not be nil")
	assert.NotNil(t, loggers.rtcpLogger, "RTCP logger should not be nil")

	// Clean up
	err = loggers.ccLogger.Close()
	assert.NoError(t, err, "CC logger should close without error")

	err = loggers.rtpLogger.Close()
	assert.NoError(t, err, "RTP logger should close without error")

	err = loggers.rtcpLogger.Close()
	assert.NoError(t, err, "RTCP logger should close without error")
}

func TestFlow_Integration(t *testing.T) {
	// Create temporary directory for test data
	tempDir := t.TempDir()

	// Create network manager
	nm, err := NewManager()
	require.NoError(t, err, "NewManager() should not error")

	// Create logger factory
	loggerFactory := plogging.NewDefaultLoggerFactory()

	// Test complete flow creation and cleanup
	flow, err := NewSimpleFlow(loggerFactory, nm, 1, abrSenderMode, tempDir)
	require.NoError(t, err, "NewSimpleFlow() should not error")

	// Verify flow components
	assert.NotNil(t, flow.sender, "Flow sender should not be nil")
	assert.NotNil(t, flow.receiver, "Flow receiver should not be nil")

	// Test cleanup
	err = flow.Close()
	assert.NoError(t, err, "Flow.Close() should not error")

	// Verify log files were created
	expectedLogFiles := []string{
		"1_cc.log",
		"1_sender_rtp.log",
		"1_sender_rtcp.log",
		"1_receiver_rtp.log",
		"1_receiver_rtcp.log",
	}

	for _, logFile := range expectedLogFiles {
		logPath := filepath.Join(tempDir, logFile)
		_, err = os.Stat(logPath)
		assert.NoError(t, err, "Log file %s should be created", logFile)
	}
}
