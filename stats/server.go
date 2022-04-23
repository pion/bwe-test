package stats

import (
	"context"
	"html/template"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

type DataPoint struct {
	Label     string
	Timestamp int64 // milliseconds after Start
	Value     float64
}

type Server struct {
	srv      *http.Server
	upgrader *websocket.Upgrader
	dataChan chan DataPoint
}

func New() *Server {
	return &Server{
		upgrader: &websocket.Upgrader{},
		dataChan: make(chan DataPoint),
	}
}

func (s *Server) Add(d DataPoint) {
	go func() {
		s.dataChan <- d
	}()
}

func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	c, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("s.upgrader.Upgrade:", err)
		return
	}
	defer c.Close()
	for dataPoint := range s.dataChan {
		if err = c.WriteJSON(dataPoint); err != nil {
			log.Print("c.WriteJSON:", err)
			return
		}
	}
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	homeTemplate.Execute(w, "ws://"+r.Host+"/update")
}

func (s *Server) Start() {
	mux := &http.ServeMux{}
	mux.HandleFunc("/", s.home)
	mux.HandleFunc("/update", s.update)
	s.srv = &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	s.srv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

var homeTemplate = template.Must(template.New("").Parse(`
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
