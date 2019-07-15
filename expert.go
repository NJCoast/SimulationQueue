package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

// singleHandler is a HTTP function to allow a user to post a simulation to
// the system.
func singleHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST,GET,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Origin, X-Requested-With, Content-Type, Accept")

	if r.Method == "POST" {
		if err := r.ParseForm(); err != nil {
			fmt.Fprintf(w, "ParseForm() err: %v", err)
			return
		}

		username := r.FormValue("name")
		id := r.FormValue("id")

		if username == "" || id == "" {
			return
		}

		result, err := s3manager.NewUploader(sess).Upload(&s3manager.UploadInput{
			ACL:         aws.String("public-read"),
			Bucket:      aws.String("simulation.njcoast.us"),
			Key:         aws.String(fmt.Sprintf("%s/simulation/%s/%s/input_params.json", folder, username, id)),
			ContentType: aws.String("application/json"),
			Body:        r.Body,
		})

		log.Println(result, err)
	}
}

// statusHandler is a HTTP function that checks if a worker has completed
// a specific job.
func statusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST,GET,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Origin, X-Requested-With, Content-Type, Accept")

	if r.Method == "GET" {
		if err := r.ParseForm(); err != nil {
			fmt.Fprintf(w, "ParseForm() err: %v", err)
			return
		}

		username := r.FormValue("name")
		id := r.FormValue("id")

		if username == "" || id == "" {
			return
		}

		complete, position := false, 1
		for i := 0; i < len(ParameterQueue); i++ {
			if strings.HasSuffix(ParameterQueue[i].Folder, id) {
				complete = ParameterQueue[i].Complete
				break
			}

			if !ParameterQueue[i].Failed && !ParameterQueue[i].Complete {
				position++
			}
		}

		result := struct {
			Complete bool `json:"complete"`
			Workers  int  `json:"workers"`
			Position int  `json:"position"`
		}{
			complete,
			JobWorkers,
			position,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(&result); err != nil {
			log.Println(err)
			return
		}
	}
}

// queueHandler is a HTTP function that provides a list of all jobs currently
// waiting to be executed by a worker.
func queueHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST,GET,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Origin, X-Requested-With, Content-Type, Accept")

	if r.Method == "GET" {
		var jobs []Job
		for i := 0; i < len(ParameterQueue); i++ {
			if !ParameterQueue[i].Failed && !ParameterQueue[i].Complete {
				jobs = append(jobs, ParameterQueue[i])
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(&jobs); err != nil {
			log.Println(err)
			return
		}
	}
}

// queueHandler is a HTTP function that provides a list of all jobs which
// failed to execute on a worker.
func failedHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST,GET,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Origin, X-Requested-With, Content-Type, Accept")

	if r.Method == "GET" {
		var jobs []Job
		for i := 0; i < len(ParameterQueue); i++ {
			if ParameterQueue[i].Failed {
				jobs = append(jobs, ParameterQueue[i])
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(&jobs); err != nil {
			log.Println(err)
			return
		}
	}
}
