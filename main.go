package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Records struct {
	Data []Record `json:"Records"`
}

type Record struct {
	Source string   `json:"eventSource"`
	Event  string   `json:"eventName"`
	Data   S3Record `json:"s3"`
}

type S3Record struct {
	Object S3ObjectRecord `json:"object"`
}

type S3ObjectRecord struct {
	Name string `json:"key"`
	Size int    `json:"size"`
	eTag string `json:"eTag"`
}

type Job struct {
	ID       string `json:"string"`
	Folder	string `json:"folder"`
	Worker   string
	Complete bool
	SLR      float64 `json:"slr"`
	Tide     int     `json:"tide"`
	Analysis int     `json:"analysis"`
}

var ParameterQueue map[string][]Job
var name string
var sess *session.Session


var (
	jobsCreated = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "queue_jobs_total",
			Help: "Number of jobs createad.",
		},
	)
	jobsCompleted = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "queue_jobs_complete_total",
			Help: "Number of jobs completed.",
		},
	)
)

func init() {
	prometheus.MustRegister(jobsCreated)
	prometheus.MustRegister(jobsCompleted)
}

func main() {
	flag.StringVar(&name, "n", "", "Queue name")
	flag.Parse()

	if len(name) == 0 {
		flag.PrintDefaults()
		log.Fatalln("Queue name required")
	}

	ParameterQueue = make(map[string][]Job)

	sess, _ = session.NewSession(&aws.Config{
		Region: aws.String("us-east-1")},
	)

	go S3MessageQueue(name, &ParameterQueue)

	http.HandleFunc("/", clientHandler)
	http.HandleFunc("/single", singleHandler)
	http.HandleFunc("/status", statusHandler)
	http.Handle("/metrics", promhttp.Handler())
	log.Fatalln(http.ListenAndServe(fmt.Sprintf(":%d", 9090), nil))
}

func S3MessageQueue(qName string, pQueue *map[string][]Job) {
	svc := sqs.New(sess)

	resultURL, err := svc.GetQueueUrl(&sqs.GetQueueUrlInput{QueueName: aws.String(qName)})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok && aerr.Code() == sqs.ErrCodeQueueDoesNotExist {
			log.Fatalf("Unable to find queue %q\n", qName)
		}
		log.Fatalf("Unable to queue %q, %v.\n", qName, err)
	}

	for {
		result, err := svc.ReceiveMessage(&sqs.ReceiveMessageInput{
			QueueUrl:              resultURL.QueueUrl,
			AttributeNames:        aws.StringSlice([]string{"SentTimestamp"}),
			MaxNumberOfMessages:   aws.Int64(1),
			MessageAttributeNames: aws.StringSlice([]string{"ALL"}),
			WaitTimeSeconds:       aws.Int64(20),
		})
		if err != nil {
			log.Fatalf("Unable to receive message from queue %q, %v.\n", qName, err)
		}

		if len(result.Messages) > 0 {
			log.Printf("Received %d messages.\n", len(result.Messages))
			log.Println(result.Messages)

			var data Records
			if err := json.NewDecoder(strings.NewReader(*result.Messages[0].Body)).Decode(&data); err != nil {
				log.Println("error:", err)
			}

			if data.Data[0].Data.Object.Size > 0 {
				if data.Data[0].Source == "aws:s3" && data.Data[0].Event == "ObjectCreated:Put" && strings.Contains(data.Data[0].Data.Object.Name, "input_params.json") {
					folder := filepath.Dir(data.Data[0].Data.Object.Name)

					// Create Job Queue
					var jobs []Job
					jobs = append(jobs, Job{ID: uuid.New().String(), Folder: folder, Complete: false, SLR: -1, Tide: -1, Analysis: -1})
					jobsCreated.Inc()
					(*pQueue)[folder] = jobs
				}

				if data.Data[0].Source == "aws:s3" && data.Data[0].Event == "ObjectCreated:Put" && strings.Contains(data.Data[0].Data.Object.Name, "input.geojson") {
					folder := filepath.Dir(data.Data[0].Data.Object.Name)

					// Create Job Queue
					var jobs []Job
					for slr := 0.0; slr <= 1.5; slr += 0.5 {
						for tide := 0; tide < 3; tide++ {
							for analysis := 0; analysis < 3; analysis++ {
								jobs = append(jobs, Job{ID: uuid.New().String(), Folder: folder, Complete: false, SLR: slr, Tide: tide, Analysis: analysis})
								jobsCreated.Inc()
							}
						}
					}
					(*pQueue)[folder] = jobs
				}
			}

			if _, err := svc.DeleteMessage(&sqs.DeleteMessageInput{QueueUrl: resultURL.QueueUrl, ReceiptHandle: result.Messages[0].ReceiptHandle}); err != nil {
				log.Println("Delete Error", err)
				return
			}
		}
	}
}
