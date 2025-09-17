//go:build !js
// +build !js

// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package sender

import (
	"bytes"
	"io"
	"testing"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
	plogging "github.com/pion/logging"
	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockConfigurableWebRTCSender for testing options.
type MockConfigurableWebRTCSender struct {
	ccLogWriter   io.Writer
	logger        plogging.LeveledLogger
	settingEngine *webrtc.SettingEngine
	mediaEngine   *webrtc.MediaEngine
	registry      *interceptor.Registry
	estimatorChan chan cc.BandwidthEstimator
}

func (m *MockConfigurableWebRTCSender) GetSettingEngine() *webrtc.SettingEngine {
	if m.settingEngine == nil {
		m.settingEngine = &webrtc.SettingEngine{}
	}

	return m.settingEngine
}

func (m *MockConfigurableWebRTCSender) GetMediaEngine() *webrtc.MediaEngine {
	if m.mediaEngine == nil {
		m.mediaEngine = &webrtc.MediaEngine{}
	}

	return m.mediaEngine
}

func (m *MockConfigurableWebRTCSender) GetRegistry() *interceptor.Registry {
	if m.registry == nil {
		m.registry = &interceptor.Registry{}
	}

	return m.registry
}

func (m *MockConfigurableWebRTCSender) GetEstimatorChan() chan cc.BandwidthEstimator {
	if m.estimatorChan == nil {
		m.estimatorChan = make(chan cc.BandwidthEstimator, 1)
	}

	return m.estimatorChan
}

func (m *MockConfigurableWebRTCSender) SetCCLogWriter(w io.Writer) {
	m.ccLogWriter = w
}

func (m *MockConfigurableWebRTCSender) SetLogger(logger plogging.LeveledLogger) {
	m.logger = logger
}

func TestPacketLogWriter(t *testing.T) {
	rtpBuf := &bytes.Buffer{}
	rtcpBuf := &bytes.Buffer{}

	option := PacketLogWriter(rtpBuf, rtcpBuf)
	require.NotNil(t, option)

	// Test that option is a function
	assert.IsType(t, Option(nil), option)

	// Note: Full testing would require WebRTC setup, so we just test the option creation
}

func TestCCLogWriter(t *testing.T) {
	buf := &bytes.Buffer{}

	option := CCLogWriter(buf)
	require.NotNil(t, option)

	// Test applying the option
	mock := &MockConfigurableWebRTCSender{}
	err := option(mock)
	require.NoError(t, err)

	assert.Equal(t, buf, mock.ccLogWriter)
}

func TestGCC(t *testing.T) {
	initialBitrate := 1000000

	option := GCC(initialBitrate)
	require.NotNil(t, option)

	// Test that option is a function
	assert.IsType(t, Option(nil), option)

	// Note: Full testing would require WebRTC setup, so we just test the option creation
}

func TestSetLoggerFactory(t *testing.T) {
	loggerFactory := plogging.NewDefaultLoggerFactory()

	option := SetLoggerFactory(loggerFactory)
	require.NotNil(t, option)

	// Test that option is a function
	assert.IsType(t, Option(nil), option)

	// Note: Full testing would require WebRTC setup, so we just test the option creation
}

func TestSetVnet(t *testing.T) {
	// Test SetVnet option creation
	option := SetVnet(nil, nil)
	require.NotNil(t, option)

	// Test that option is a function
	assert.IsType(t, Option(nil), option)

	// Note: Full testing would require vnet setup, so we just test the option creation
}

func TestOptionTypeSignature(t *testing.T) {
	// Test that Option has the correct type signature
	var option Option = func(ConfigurableWebRTCSender) error {
		return nil
	}

	mock := &MockConfigurableWebRTCSender{}
	err := option(mock)
	assert.NoError(t, err)
}

func TestMultipleOptions(t *testing.T) {
	// Test that multiple options can be applied
	buf1 := &bytes.Buffer{}
	loggerFactory := plogging.NewDefaultLoggerFactory()

	mock := &MockConfigurableWebRTCSender{}

	// Apply multiple options
	options := []Option{
		CCLogWriter(buf1),
		SetLoggerFactory(loggerFactory),
	}

	for _, option := range options {
		err := option(mock)
		require.NoError(t, err)
	}

	assert.Equal(t, buf1, mock.ccLogWriter)
}
