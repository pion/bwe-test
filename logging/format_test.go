// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package logging

import (
	"strings"
	"testing"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRTPFormat_BasicPacket(t *testing.T) {
	formatter := &RTPFormatter{}

	pkt := &rtp.Packet{
		Header: rtp.Header{
			PayloadType:    96,
			SSRC:           12345,
			SequenceNumber: 1,
			Timestamp:      160,
			Marker:         true,
		},
		Payload: []byte{0x01, 0x02, 0x03},
	}

	data, err := formatter.RTPFormat(pkt, interceptor.Attributes{})
	require.NoError(t, err)
	require.NotEmpty(t, data)

	line := string(data)
	assert.True(t, strings.HasSuffix(line, "\n"))
	// Should contain comma-separated values: timestamp, PT, SSRC, seqnr, ts, marker, size, twcc, unwrapped
	parts := strings.Split(strings.TrimSpace(line), ", ")
	assert.Len(t, parts, 9)
}

func TestRTPFormat_WithTWCCExtension(t *testing.T) {
	formatter := &RTPFormatter{}

	// Build a TWCC extension.
	twcc := rtp.TransportCCExtension{TransportSequence: 42}
	twccData, err := twcc.Marshal()
	require.NoError(t, err)

	pkt := &rtp.Packet{
		Header: rtp.Header{
			PayloadType:      96,
			SSRC:             12345,
			SequenceNumber:   5,
			Timestamp:        800,
			Extension:        true,
			ExtensionProfile: 0xBEDE, // RFC 5285 one-byte header
		},
		Payload: []byte{0x01},
	}
	require.NoError(t, pkt.SetExtension(1, twccData))

	data, err := formatter.RTPFormat(pkt, interceptor.Attributes{})
	require.NoError(t, err)
	assert.Contains(t, string(data), "42")
}

func TestRTPFormat_InvalidTWCCExtension(t *testing.T) {
	formatter := &RTPFormatter{}

	pkt := &rtp.Packet{
		Header: rtp.Header{
			PayloadType:      96,
			SSRC:             12345,
			SequenceNumber:   1,
			Extension:        true,
			ExtensionProfile: 0xBEDE, // RFC 5285 one-byte header
		},
		Payload: []byte{0x01},
	}
	// Set an invalid extension payload — too short for TWCC.
	require.NoError(t, pkt.SetExtension(1, []byte{}))

	_, err := formatter.RTPFormat(pkt, interceptor.Attributes{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshaling TWCC")
}

func TestRTCPFormat_TransportLayerCC(t *testing.T) {
	pkt := &rtcp.TransportLayerCC{
		SenderSSRC: 1,
		MediaSSRC:  2,
	}

	data, err := RTCPFormat(pkt, interceptor.Attributes{})
	require.NoError(t, err)
	require.NotEmpty(t, data)

	line := string(data)
	assert.True(t, strings.HasSuffix(line, "\n"))
	parts := strings.Split(strings.TrimSpace(line), ", ")
	assert.Len(t, parts, 2)
}

func TestRTCPFormat_RawPacket(t *testing.T) {
	raw := rtcp.RawPacket([]byte{0x01, 0x02, 0x03})

	data, err := RTCPFormat(&raw, interceptor.Attributes{})
	require.NoError(t, err)
	assert.Contains(t, string(data), "3") // size = 3 bytes
}

func TestRTCPFormat_UnknownType(t *testing.T) {
	// Use a type that doesn't match the switch — size stays 0.
	pkt := &rtcp.SenderReport{SSRC: 1}

	data, err := RTCPFormat(pkt, interceptor.Attributes{})
	require.NoError(t, err)
	assert.Contains(t, string(data), ", 0") // size = 0
}
