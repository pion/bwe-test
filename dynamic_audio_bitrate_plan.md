# Plan: Move audio Opus encoding into the bwe-test (Go) layer for GCC-driven dynamic bitrate

## Context

The RTC EV-mic audio stream is effectively **fixed bitrate**: audio is encoded on
the **C++** side (`guardian::OpusEncoder`, no bitrate ever set → Opus `OPUS_AUTO`
default) and handed to the Go/Pion layer as **pre-encoded** Opus frames via
`PushAudioFrame`. Because audio is already encoded before it reaches Pion, the Go
GCC bandwidth estimator — which dynamically drives **video** bitrate — cannot touch
it. When the network degrades, video scales down but audio keeps its bandwidth.

**Decision:** Move Opus encoding out of C++ and **into the `bwe-test` module**, so
audio becomes a first-class GCC-managed encoded track alongside video. The same
control loop (`updateBitrate` → `updateEncoderBitrate`) that drives VP8 will then
drive Opus natively, sharing the GCC bitrate pool. C++ will send **raw PCM**
instead of encoded Opus.

## Current Go video pipeline (the pattern to mirror) — all in `bwe-test`

`sender/rtc_sender.go` (in go mod cache):
- `AddVideoTrack` (:387): builds a `mediadevices` codec selector with a
  `codec.VideoEncoderBuilder`, a `FrameBuffer` source (`frame_buffer.go`), a
  `mediadevices.VideoTrack`, an `encodedReader = mediaTrack.NewEncodedReader(mime)`,
  a `webrtc.TrackLocalStaticSample`, stores an `EncodedTrack` (:58) in `s.tracks`,
  and launches `runEncodeLoop`.
- `runEncodeLoop`/`encodeAndSendTrack` (:763/:799): `encodedReader.Read()` →
  `videoTrack.WriteSample(...)`; tracks bitrate via `bitrateTracker`.
- GCC loop: `Start()` (:665) every 100ms → `updateBitrate(target)` (:730) →
  per-track `calculateTrackBitrate` (custom weights in `bitrateAllocation`) →
  `updateEncoderBitrate` (:704) which tries `codec.QPController` then a generic
  `SetBitrate(int,int)` interface.
- `AddAudioTrack` (:500): **pre-encoded** path — just a `TrackLocalStaticSample`,
  NOT in `s.tracks`, NOT in allocation. This is what we replace.

**Available Go Opus building blocks (mediadevices):**
- `pkg/codec/opus`: `opus.NewParams()` → `Params{BaseParams{BitRate}, Latency}`;
  implements `codec.AudioEncoderBuilder` (`BuildAudioEncoder`, `RTPCodec()` = Opus
  48k). Its encoder **controller implements `codec.BitRateController`** with
  `SetBitRate(int) error` (thread-safe, libopus FFI).
- `mediadevices.NewAudioTrack(AudioSource, selector)` + `WithAudioEncoders(&params)`;
  `AudioTrack.NewEncodedReader(webrtc.MimeTypeOpus)` works just like video.
- `AudioSource = audio.Reader (Read() (wave.Audio, func(), error)) + Source`;
  feed `wave.Int16Interleaved{Data []int16, Size ChunkInfo{Len,Channels,SamplingRate}}`.

**Packaging gotcha:** the Go layer is compiled into prebuilt
`libwebrtc_{x86_64,arm64}.so` from GCS via Bazel `@webrtc_x86_64`/`@webrtc_arm64`
(`onboard/real_time_communication/BUILD:12-21`). Go changes (main.go **and** the
bwe-test fork) require rebuilding + re-pinning those `.so` artifacts.

## Changes

### 1. Fork `bwe-test` (Nuro fork + `go.mod` replace)

Mirror the existing `gitent.corp.nuro.team/Nuro-ai/pion-server-sdk-go` fork
arrangement. In `webrtc/go.mod` add:
`replace github.com/pion/bwe-test => <nuro fork>@<rev>` (or a vendored/local path).

In the fork's `sender` package:

- **Generalize `EncodedTrack`** (`rtc_sender.go:58`) to carry audio:
  add `isAudio bool`, a generic `localTrack *webrtc.TrackLocalStaticSample` (used by
  both; video keeps writing through it), and audio fields
  `audioMediaTrack *mediadevices.AudioTrack`, `audioSource AudioSource`. Keep video
  fields. `encodedReader`, `bitrateTracker`, `encoderMu`, `mimeType` are reused as-is.

- **New `audio_buffer.go`** mirroring `frame_buffer.go`: an `AudioBuffer` that
  implements `audio.Reader`+`Source`, holds a bounded queue of
  `wave.Int16Interleaved` chunks, returns `ErrNoFrameAvailable`/`ErrBufferClosed`
  like `FrameBuffer`, and exposes `PushPCM(samples []int16, sampleRate, channels int)`.
  (Input is expected at 48 kHz mono/stereo to match `RTPCodec` 48k; C++ already
  targets 48 kHz — see §3. If a non-48k source appears, resample in `PushPCM`.)

- **New `AddEncodedAudioTrack(trackID string, params opus.Params) error`**: build
  `codecSelector = NewCodecSelector(WithAudioEncoders(&params))`,
  `mediaTrack = NewAudioTrack(audioBuffer, codecSelector)`,
  `encodedReader = mediaTrack.NewEncodedReader(webrtc.MimeTypeOpus)`,
  `localTrack = NewTrackLocalStaticSample(Opus caps)` — reuse the exact SDP caps
  from current `AddAudioTrack` (`MimeTypeOpus`, 48000, Channels 2,
  `minptime=10;useinbandfec=1`). Store an `EncodedTrack{isAudio:true,...}` in
  `s.tracks`, **do not** add to the PeerConnection (LiveKit publishes it, as today),
  then `go runEncodeLoop(...)`.

- **`encodeAndSendTrack`** (:799): write through `track.localTrack`; pick frame
  duration by type (audio = 20 ms, video = `time.Second/10`). The `FrameBuffer`
  type-assert for capture stamps already no-ops for non-video sources. Skip the
  VP8 keyframe-byte sniff (`onEncodedFrame`) for audio.

- **`updateEncoderBitrate`** (:704): add a branch — if the controller implements
  `codec.BitRateController` (`SetBitRate(int) error`), call it (clamp audio target
  to `[kAudioMinBps, kAudioMaxBps]`, e.g. 8k–32k). This is the path Opus uses.

- **Allocation**: audio is now in `s.tracks`, so it flows through
  `calculateTrackBitrate` with whatever weight `SetBitrateAllocation` assigns —
  no special case needed beyond the clamp above. Guard `ForceKeyFrame`/keyframe
  paths and `recreateEncoder`’s VP8-specific bits with `!isAudio`.

- **PCM feed**: `SendAudioFrame(trackID string, pcm []int16, sampleRate, channels int) error`
  → looks up the audio track and calls `audioSource.PushPCM(...)`.

### 2. `onboard/real_time_communication/webrtc/main.go`

- Replace `rtcSender.AddAudioTrack("audio_ev_external")` (:589) with
  `rtcSender.AddEncodedAudioTrack("audio_ev_external", opusParams)` where
  `opusParams = opus.NewParams()` (set `BaseParams.BitRate = kAudioInitBitrate`,
  `Latency = opus.Latency20ms`). Add the `opus`/`codec` imports.
- Include audio in `bitrateAllocations` so `updateBitrate` drives it (small weight,
  with the encoder-side clamp keeping it in the Opus band). Add audio bitrate
  constants near `totalInitBitrate` (:121).
- New cgo export `PushAudioPCM(trackID, pcmPtr unsafe.Pointer, numSamples C.int,
  sampleRate C.int, channels C.int)` → `C.GoBytes`/slice → `rtcSender.SendAudioFrame`.
  Remove the now-unused pre-encoded `PushAudioFrame` (:1904).
- LiveKit publishing path (:1082–1113) is unchanged — it still resolves the track
  via `GetWebRTCTrackLocal("audio_ev_external")`, which now returns the managed
  encoded-audio track.

### 3. C++ `real_time_communication_module.{h,cc}`

- Delete `audio_opus_encoder_` member and `guardian::OpusEncoder` usage.
- In `HandleAudioEvMics` (`real_time_communication_module.cc:1156`): keep ev_mic_00
  selection and the 24→16-bit conversion (Opus/Go wants 16-bit), then call
  `PushAudioPCM("audio_ev_external", pcm16, numSamples, sampleRate, channels)`
  instead of encode+`PushAudioFrame`. Drop the `Enqueue/HasEncoded/GetEncoded` loop
  and `kOpusFrameDurationMs`.
- Remove `webrtc.h`'s old `PushAudioFrame` decl; add `PushAudioPCM`. Drop the
  `//guardian/audio:opus_codec` BUILD dep if unused elsewhere in this module.

### 4. Build / packaging

- `go.mod` replace for the bwe-test fork must resolve in the `.so` build env.
- Rebuild `libwebrtc_x86_64.so` (and arm64) via the cgo `c-shared` workflow and
  re-pin the `@webrtc_x86_64`/`@webrtc_arm64` GCS artifacts (the prebuilt-.so pin).
- `bazel build //onboard/real_time_communication:real_time_communication`.

## Files to modify

- `bwe-test` fork: `sender/rtc_sender.go` (generalize `EncodedTrack`,
  `AddEncodedAudioTrack`, `encodeAndSendTrack`, `updateEncoderBitrate`,
  `SendAudioFrame`, keyframe guards) + new `sender/audio_buffer.go`.
- `onboard/real_time_communication/webrtc/go.mod` (replace directive).
- `onboard/real_time_communication/webrtc/main.go` (track creation, allocation,
  `PushAudioPCM`, remove `PushAudioFrame`).
- `onboard/real_time_communication/webrtc/webrtc.h` (swap `PushAudioFrame`→`PushAudioPCM`).
- `onboard/real_time_communication/real_time_communication_module.{cc,h}` and `BUILD`
  (push raw PCM; drop the C++ Opus encoder).

## Open implementation detail

mediadevices' Opus encoder frames at 48 kHz per `Latency` (20 ms). C++ already
targets 48 kHz mono; ensure `PushAudioPCM` delivers 48 kHz 16-bit. If a P2 source
arrives at another rate, resample in `AudioBuffer.PushPCM` (or keep the existing
C++ resample and push 48 kHz). Confirm the encoder's input `prop.Media` sample
rate matches what we push during impl.

## Verification

1. Build the bwe-test fork + Go `.so`, re-pin artifacts, then
   `bazel build //onboard/real_time_communication:...`; `n lint --fix`.
2. Bench/CarBench e2e: confirm the audio track negotiates as **Opus** and plays.
3. Confirm GCC drives audio: add a `Debugf`/`LOG` when `SetBitRate` is applied to
   the audio track; throttle then restore the network and verify the applied audio
   bitrate tracks the GCC target down to the floor and back up, staying within
   `[kAudioMinBps, kAudioMaxBps]`, audio intelligible at the floor.
4. Regression: video adaptation still works; total audio+video respects the GCC
   estimate (audio shares the pool, not additive); no audio dropouts across changes.
