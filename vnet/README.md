# VP8 Decoding and MP4 Generation

This directory includes VP8 frame decoding with MP4 and PNG output generation.

## Running the Tests

### Quick Start (Default)

Run the tests with VP8 decoding enabled:

```bash
cd vnet
go run .
```

Or with log level:

```bash
cd vnet
go run . -log info
```

### With Custom Video Files

```bash
cd vnet
go run . -videos "../path/to/video1.mp4,../path/to/video2.mp4"
```

### Using Build Tags (If Needed)

If you encounter issues with the aruco module requiring OpenCV contrib packages, use the build tag:

```bash
cd vnet
go run -tags gocv_specific_modules . -log info
```

**Build Tag Note**: The `-tags gocv_specific_modules` build tag excludes the aruco module, which requires OpenCV contrib packages. This is only needed if you have issues with the default build.

## Output Structure

The test will create organized output directories:

```
vnet/data/TestVnetRunnerDualVideoTracks/VariableAvailableCapacitySingleFlow/
├── 0_cc.log
├── 0_receiver_rtcp.log
├── 0_receiver_rtp.log
├── 0_sender_rtcp.log
├── 0_sender_rtp.log
└── received_video/
    ├── received_0_track-1.ivf
    ├── received_0_track-2.ivf
    └── output_0/
        ├── mp4/
        │   ├── track-1.mp4
        │   └── track-2.mp4
        └── frames/
            ├── track-1/
            │   ├── decoded_frame_00000.png
            │   └── ...
            └── track-2/
                ├── decoded_frame_00000.png
                └── ...
```

## Dependencies

This module has its own `go.mod` to isolate GoCV and mediadevices dependencies from the main bwe-test package.

### Required

- Go 1.24+
- OpenCV 4.x (without contrib modules is fine)
- libvpx (for VP8 encoding/decoding)
- GoCV v0.31.0

### Installing Dependencies

```bash
# Ubuntu/Debian
sudo apt-get install libopencv-dev libvpx-dev

# macOS
brew install opencv vpx
```

