<h1 align="center">
  <br>
  Pion Bandwidth Estimation Test Runner
  <br>
</h1>
<h4 align="center">Test runner for congestion control and bandwidth estimation algorithms in Pion</h4>
<p align="center">
  <a href="https://pion.ly"><img src="https://img.shields.io/badge/pion-webrtc-gray.svg?longCache=true&colorB=brightgreen" alt="Pion webrtc"></a>
  <a href="https://pion.ly/slack"><img src="https://img.shields.io/badge/join-us%20on%20slack-gray.svg?longCache=true&logo=slack&colorB=brightgreen" alt="Slack Widget"></a>
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
constraints. There are two test applications, one using a simple simulcast-like
setup and the other one using adaptive bitrate streaming with a synthetic
encoder.

To run the simulcast test, you must create three input video files as described
in the [bandwidth-esimation-from-disk
example](https://github.com/pion/webrtc/tree/master/examples/bandwidth-estimation-from-disk)
and place them in the `vnet` directory.

- [ ] **Variable Available Capacity with a Single Flow**
- [ ] **Variable Available Capacity with Multiple Flows**
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

### Roadmap
The library is used as a part of our WebRTC implementation. Please refer to that [roadmap](https://github.com/pion/webrtc/issues/9) to track our major milestones.

### Community
Pion has an active community on the [Slack](https://pion.ly/slack).

Follow the [Pion Twitter](https://twitter.com/_pion) for project updates and important WebRTC news.

We are always looking to support **your projects**. Please reach out if you have something to build!
If you need commercial support or don't want to use public methods you can contact us at [team@pion.ly](mailto:team@pion.ly)

### Contributing
Check out the [contributing wiki](https://github.com/pion/webrtc/wiki/Contributing) to join the group of amazing people making this project possible

### License
MIT License - see [LICENSE](LICENSE) for full text