<h1 align="center">
  <br>
  Pion Bandwidth Estimation Test Runner
  <br>
</h1>
<h4 align="center">Test runner for congestion control and bandwidth estimation algorithms in Pion</h4>
<p align="center">
  <a href="https://pion.ly"><img src="https://img.shields.io/badge/pion-webrtc-gray.svg?longCache=true&colorB=brightgreen" alt="Pion webrtc"></a>
  <a href="https://discord.gg/PngbdqpFbt"><img src="https://img.shields.io/badge/join-us%20on%20discord-gray.svg?longCache=true&logo=discord&colorB=brightblue" alt="join us on Discord"></a> <a href="https://bsky.app/profile/pion.ly"><img src="https://img.shields.io/badge/follow-us%20on%20bluesky-gray.svg?longCache=true&logo=bluesky&colorB=brightblue" alt="Follow us on Bluesky"></a>
  <br>
  <a href="https://pkg.go.dev/github.com/pion/bwe-test"><img src="https://godoc.org/github.com/pion/bwe-test?status.svg" alt="GoDoc"></a>
  <a href="https://codecov.io/gh/pion/bwe-test"><img src="https://codecov.io/gh/pion/bwe-test/branch/master/graph/badge.svg" alt="Coverage Status"></a>
  <a href="https://goreportcard.com/report/github.com/pion/bwe-test"><img src="https://goreportcard.com/badge/github.com/pion/bwe-test" alt="Go Report Card"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License: MIT"></a>
</p>
<br>

This repository implements the test cases for congestion control for interactive
real-time media described in [RFC8867](https://www.rfc-editor.org/rfc/rfc8867.html) for the algorithm(s)
[implemented in Pion](https://github.com/pion/interceptor).

### Implemented/Planned Test Cases and Applications
The current implementation uses [`vnet.Net` from
pion/transport](https://github.com/pion/transport) to simulate network
constraints. There are three test applications:

1. **Simulcast-like setup** - Uses a simple simulcast configuration
2. **Adaptive bitrate streaming** - Uses synthetic encoder for adaptive bitrate
3. **Video file reader** - Reads numbered JPG image files and sends them as video frames

#### Video File Reader
The video file reader (`VideoFileReader`) reads series of numbered JPG image files 
(e.g., `frame_000.jpg`, `frame_001.jpg`, etc.) from a directory and sends them as 
individual video frames using an `RTCSender`. This allows testing with real video 
content while maintaining precise control over frame timing and network conditions.

To use the video file reader test:
- Create directories with numbered JPG files (e.g., `../sample_videos_0/`, `../sample_videos_1/`)
- Files should be named with sequential numbers (frame_000.jpg, frame_001.jpg, etc.)
- The reader automatically discovers, sorts, and cycles through the frames

To run the simulcast test, you must create three input video files as described
in the [bandwidth-esimation-from-disk
example](https://github.com/pion/webrtc/tree/master/examples/bandwidth-estimation-from-disk)
and place them in the `vnet` directory.

- [ ] **Variable Available Capacity with a Single Flow**
- [ ] **Variable Available Capacity with Multiple Flows**
- [x] **Dual Video Tracks with Variable Available Capacity** - Uses video file reader with multiple video tracks
- [ ] **Congested Feedback Link with Bi-directional Media Flows**
- [ ] **Competing Media Flows with the Same Congestion Control Algorithm**
- [ ] **Round Trip Time Fairness**
- [ ] **Media Flow Competing with a Long TCP Flow**
- [ ] **Media Flow Competing with Short TCP Flows**
- [ ] **Media Pause and Resume**

- [ ] **Media Flows with Priority**
- [ ] **Explicit Congestion Notification Usage**
- [ ] **Multiple Bottlenecks**

### Evaluation
[RFC 8868](https://www.rfc-editor.org/rfc/rfc8868.html) describes guidelines to
evaluate congestion control algorithms for interactive real-time media.
Currently, live statistics can be viewed during the test run via a web
interface. In future, we might automate the evaluation.

### Running
To run the tests, run `go test -v ./vnet/`.

To run the main test application with all test cases (including the video file reader test):
```bash
cd vnet
go run .
```

The application will run multiple test scenarios including:
- ABR (Adaptive Bitrate) tests with single and multiple flows
- Simulcast tests with single and multiple flows  
- Video file reader test with dual video tracks (requires sample video directories)

#### Video File Reader Test Requirements

**Dependencies:**
- Test depends on libvpx-dev library. The procedure to install the library on linux machine is:
  ```bash
  sudo apt-get update
  sudo apt-get install -y libvpx-dev pkg-config
  ```

**Video Preparation:**
- User needs to first decode two videos into sequenced jpg files and put the files under `sample_videos_0` and `sample_videos_1` directories
- The command to use ffmpeg to decode the video is as follows:
  ```bash
  mkdir -p sample_videos_0
  ffmpeg -i /path/to/input/video0.mp4 -vsync 0 sample_videos_0/frame_%04d.jpg
  
  mkdir -p sample_videos_1
  ffmpeg -i /path/to/input/video1.mp4 -vsync 0 sample_videos_1/frame_%04d.jpg
  ```

### Roadmap
The library is used as a part of our WebRTC implementation. Please refer to that [roadmap](https://github.com/pion/webrtc/issues/9) to track our major milestones.

### Community
Pion has an active community on the [Discord](https://discord.gg/PngbdqpFbt).

Follow the [Pion Bluesky](https://bsky.app/profile/pion.ly) or [Pion Twitter](https://twitter.com/_pion) for project updates and important WebRTC news.

We are always looking to support **your projects**. Please reach out if you have something to build!
If you need commercial support or don't want to use public methods you can contact us at [team@pion.ly](mailto:team@pion.ly)

### Contributing
Check out the [contributing wiki](https://github.com/pion/webrtc/wiki/Contributing) to join the group of amazing people making this project possible

### License
MIT License - see [LICENSE](LICENSE) for full text