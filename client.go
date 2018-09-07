package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

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

	go c.Write()
	c.Read()

	c.CurrentJob.Worker = ""
	c.CurrentJob.Retried += 1
	if c.CurrentJob.Retried >= 3 {
		jobsFailed.Inc()
		c.CurrentJob.Failed = true
		log.Println("Job failed:", *c.CurrentJob)
	}
	log.Println("Deleted", c.WorkerID)
}

type Socket struct {
	Connection *websocket.Conn
	WorkerID   string
	Send       chan string
	CurrentJob *Job
}

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
			for _, pFolder := range ParameterQueue {
				for i := 0; i < len(pFolder); i++ {
					if !pFolder[i].Failed && !pFolder[i].Complete && pFolder[i].Worker == "" && s.CurrentJob == nil {
						data, err := json.Marshal(&pFolder[i])
						if err != nil {
							log.Fatalln(err)
						}

						found = true
						s.Send <- "DATA:" + string(data)
						s.CurrentJob = &pFolder[i]
						s.CurrentJob.Start = time.Now()
						s.CurrentJob.Worker = s.WorkerID
						break
					}
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
		}
	}
}

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
