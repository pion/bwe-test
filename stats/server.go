// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package stats provides functionality for collecting and visualizing statistics.
package stats

import (
	"html/template"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/pion/logging"
)

// DataPoint represents a single data point for visualization.
type DataPoint struct {
	Label     string
	Timestamp int64 // milliseconds after Start
	Value     float64
}

// Server handles WebSocket connections for real-time data visualization.
type Server struct {
	upgrader *websocket.Upgrader
	dataChan chan DataPoint
	log      logging.LeveledLogger
}

// New creates a new statistics server.
func New() *Server {
	return &Server{
		upgrader: &websocket.Upgrader{},
		dataChan: make(chan DataPoint),
		log:      logging.NewDefaultLoggerFactory().NewLogger("server"),
	}
}

// Add adds a data point to the server for broadcasting to clients.
func (s *Server) Add(d DataPoint) {
	go func() {
		s.dataChan <- d
	}()
}

// Start starts the statistics server on the specified address.
func (s *Server) Start(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.home)
	mux.HandleFunc("/update", s.update)

	//nolint:gosec
	return http.ListenAndServe(addr, mux)
}

func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	wsConn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Errorf("s.upgrader.Upgrade: %v", err)

		return
	}
	defer func() {
		if err = wsConn.Close(); err != nil {
			s.log.Errorf("failed to close websocket connection: %v", err)
		}
	}()

	for dataPoint := range s.dataChan {
		if err = wsConn.WriteJSON(dataPoint); err != nil {
			s.log.Errorf("c.WriteJSON: %v", err)

			return
		}
	}
}

func (s *Server) home(respWriter http.ResponseWriter, req *http.Request) {
	homeTemplate := template.Must(template.New("").Parse(`
<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <meta http-equiv="X-UA-Compatible" content="ie=edge">
    <title>My test page</title>
    <script src="https://cdn.plot.ly/plotly-latest.min.js"></script>
  </head>
  <body>
    <div id="graph"></div>
    <script>
      function rand() {
        return Math.random();
      }

      Plotly.plot('graph', [{
        y: [],
			x: [],
        mode: 'lines',
        line: {color: '#80CAF6'},
        type: 'scatter'
      }]);

      var cnt = 0;

		  const socket = new WebSocket("{{.}}");
      socket.onmessage = function(event) {
			data = JSON.parse(event.data)
			console.log(data)
        Plotly.extendTraces('graph', {
          y: [[data['Value']]],
			  x: [[data['Timestamp']]]
        }, [0])
      }
    </script>
  </body>
</html>
`))

	if err := homeTemplate.Execute(respWriter, "ws://"+req.Host+"/update"); err != nil {
		s.log.Errorf("failed to execute template: %v", err)
		http.Error(respWriter, "Internal server error", http.StatusInternalServerError)
	}
}
