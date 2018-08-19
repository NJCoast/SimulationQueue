package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

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
			Key:         aws.String(fmt.Sprintf("%s/%s/%s/input_params.json", folder, username, id)),
			ContentType: aws.String("application/json"),
			Body:        r.Body,
		})

		log.Println(result, err)
	}
}

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

		folder := fmt.Sprintf("%s/%s/%s", folder, username, id)

		complete := false
		if _, ok := ParameterQueue[folder]; ok {
			if len(ParameterQueue[folder]) > 0 {
				complete = ParameterQueue[folder][0].Complete
			}
		}

		result := struct {
			Complete bool `json:"complete"`
		}{
			complete,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(&result); err != nil {
			log.Println(err)
			return
		}
	}
}
