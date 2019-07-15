package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var JobWorkers int = 0

// clientHandler is a web link that workers can connect to that upgrades the
// connection to a websocket and adds them to the workers list.
func clientHandler(w http.ResponseWriter, r *http.Request) {
	var upgrader websocket.Upgrader

	upgrader.CheckOrigin = func(r *http.Request) bool {
		return true
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket:", err)
		return
	}

	c := &Socket{Connection: ws, WorkerID: r.FormValue("id"), Send: make(chan string, 10)}
	log.Println("Added", c.WorkerID)
	JobWorkers++

	go c.Write()
	c.Read()

	c.CurrentJob.Worker = ""
	c.CurrentJob.Retried++
	if c.CurrentJob.Retried >= 3 {
		jobsFailed.Inc()
		c.CurrentJob.Failed = true
		log.Println("Job failed:", *c.CurrentJob)
	}
	log.Println("Deleted", c.WorkerID)
	JobWorkers--
}

// The Socket structure contains information relevant to each worker connected
// through a websocket
type Socket struct {
	Connection *websocket.Conn
	WorkerID   string
	Send       chan string
	CurrentJob *Job
}

// The Read function handles the websocket's read loop. It looks for new job
// requests, completed jobs and failed jobs.
func (s *Socket) Read() {
	defer func() {
		s.Connection.Close()
	}()

	s.Connection.SetReadLimit(2048)
	s.Connection.SetReadDeadline(time.Now().Add(60 * time.Second))
	s.Connection.SetPongHandler(func(string) error {
		s.Connection.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := s.Connection.ReadMessage()
		if err != nil {
			log.Println(err)
			break
		}
		log.Println("Recieved["+s.WorkerID+"]", string(message))

		parts := strings.Split(string(message), ":")

		switch parts[0] {
		case "GET":
			found := false

			// Try to find any user simulations first
			for i := 0; i < len(ParameterQueue); i++ {
				if !ParameterQueue[i].Failed && !ParameterQueue[i].Complete && ParameterQueue[i].Worker == "" && ParameterQueue[i].SLR == -1 && s.CurrentJob == nil {
					data, err := json.Marshal(&ParameterQueue[i])
					if err != nil {
						log.Fatalln(err)
					}

					found = true
					s.Send <- "DATA:" + string(data)
					s.CurrentJob = &ParameterQueue[i]
					s.CurrentJob.Start = time.Now()
					s.CurrentJob.Worker = s.WorkerID
					break
				}
			}

			// Search remaining simulations
			for i := 0; i < len(ParameterQueue); i++ {
				if !ParameterQueue[i].Failed && !ParameterQueue[i].Complete && ParameterQueue[i].Worker == "" && ParameterQueue[i].SLR != -1 && s.CurrentJob == nil {
					data, err := json.Marshal(&ParameterQueue[i])
					if err != nil {
						log.Fatalln(err)
					}

					found = true
					s.Send <- "DATA:" + string(data)
					s.CurrentJob = &ParameterQueue[i]
					s.CurrentJob.Start = time.Now()
					s.CurrentJob.Worker = s.WorkerID
					break
				}
			}

			// No more work
			if !found {
				s.Send <- "DATA:"
			}
		case "COMPLETE":
			jobsCompleted.Inc()
			jobsDuration.Observe(time.Now().Sub(s.CurrentJob.Start).Seconds())
			s.CurrentJob.Complete = true
			s.CurrentJob = nil
		case "FAILED":
			s.CurrentJob.Worker = ""
			s.CurrentJob.Retried++
			if s.CurrentJob.Retried >= 3 {
				jobsFailed.Inc()
				s.CurrentJob.Failed = true
				log.Println("Job failed:", *s.CurrentJob)
			}
			s.CurrentJob = nil
		}
	}
}

// The Write function handles the websocket's write loop. This includes the
// ping to keep the socket alive as well as sending anything that is sent 
// by the channel.
func (s *Socket) Write() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		s.Connection.Close()
	}()

	for {
		select {
		case data := <-s.Send:
			log.Println("Sending["+s.WorkerID+"]", data)
			if err := s.Connection.WriteMessage(websocket.TextMessage, []byte(data)); err != nil {
				return
			}
		case <-ticker.C:
			if err := s.Connection.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}
