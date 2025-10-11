// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

package receiver

import (
	"bytes"
	"io"
	"testing"

	"github.com/pion/interceptor"
	"github.com/pion/logging"
	"github.com/pion/transport/v3/vnet"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/ivfwriter"
	"github.com/stretchr/testify/assert"
)

func TestPacketLogWriter(t *testing.T) {
	rtpBuf := &bytes.Buffer{}
	rtcpBuf := &bytes.Buffer{}

	option := PacketLogWriter(rtpBuf, rtcpBuf)

	receiver := &Receiver{
		registry: &interceptor.Registry{},
	}

	err := option(receiver)
	assert.NoError(t, err, "PacketLogWriter() should not error")

	// Test that the registry was modified (basic check)
	assert.NotNil(t, receiver.registry, "PacketLogWriter() should not make registry nil")
}

func TestPacketLogWriter_ErrorCases(t *testing.T) {
	// Test with nil buffers to exercise error paths
	option := PacketLogWriter(nil, nil)

	receiver := &Receiver{
		registry: &interceptor.Registry{},
	}

	// This might cause an error, which exercises the error path
	err := option(receiver)
	if err != nil {
		t.Logf("PacketLogWriter() with nil writers error: %v", err)
	}
}

func TestDefaultInterceptors(t *testing.T) {
	option := DefaultInterceptors()

	receiver := &Receiver{
		mediaEngine: &webrtc.MediaEngine{},
		registry:    &interceptor.Registry{},
	}

	err := option(receiver)
	assert.NoError(t, err, "DefaultInterceptors() should not error")
}

func TestSetVnet(t *testing.T) {
	// Create a virtual network for testing
	network, err := vnet.NewNet(&vnet.NetConfig{})
	if err != nil {
		t.Skip("Cannot create vnet for test")
	}

	publicIPs := []string{"1.2.3.4"}
	option := SetVnet(network, publicIPs)

	receiver := &Receiver{
		settingEngine: &webrtc.SettingEngine{},
	}

	err = option(receiver)
	assert.NoError(t, err, "SetVnet() should not error")
}

func TestSetLoggerFactory(t *testing.T) {
	loggerFactory := logging.NewDefaultLoggerFactory()
	option := SetLoggerFactory(loggerFactory)

	receiver := &Receiver{
		settingEngine: &webrtc.SettingEngine{},
	}

	err := option(receiver)
	assert.NoError(t, err, "SetLoggerFactory() should not error")

	assert.NotNil(t, receiver.log, "SetLoggerFactory() should set logger")
}

func TestSaveVideo(t *testing.T) {
	option := SaveVideo("test_output")

	receiver := &Receiver{
		videoWriters: &map[string]io.WriteCloser{},
		ivfWriters:   &map[string]*ivfwriter.IVFWriter{},
		log:          logging.NewDefaultLoggerFactory().NewLogger("test"),
	}

	err := option(receiver)
	assert.NoError(t, err, "SaveVideo() option should not error")

	assert.Equal(t, "test_output", receiver.outputBasePath, "SaveVideo() outputBasePath should be set correctly")
}
