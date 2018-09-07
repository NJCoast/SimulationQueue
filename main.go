package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

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
	Folder   string `json:"folder"`
	Worker   string
	Complete bool
	Failed bool
	Retried int
	SLR      float64 `json:"slr"`
	Tide     int     `json:"tide"`
	Analysis int     `json:"analysis"`
	Start    time.Time
}

var ParameterQueue map[string][]Job
var name, folder string
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
	jobsFailed = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "queue_jobs_failed_total",
			Help: "Number of jobs failed.",
		},
	)
	jobsDuration = prometheus.NewSummary(prometheus.SummaryOpts{
		Name:       "queue_jobs_duration",
		Help:       "The duration of job execution.",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	})

	inFlightGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "in_flight_requests",
		Help: "A gauge of requests currently being served by the wrapped handler.",
	})
	counter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "api_requests_total",
			Help: "A counter for requests to the wrapped handler.",
		},
		[]string{"code", "method"},
	)
	duration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "request_duration_seconds",
			Help:    "A histogram of latencies for requests.",
			Buckets: []float64{.25, .5, 1, 2.5, 5, 10},
		},
		[]string{"handler", "method"},
	)
	responseSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "response_size_bytes",
			Help:    "A histogram of response sizes for requests.",
			Buckets: []float64{200, 500, 900, 1500},
		},
		[]string{},
	)
)

func init() {
	prometheus.MustRegister(jobsCreated, jobsCompleted, jobsFailed, jobsDuration)
	prometheus.MustRegister(inFlightGauge, counter, duration, responseSize)
}

func main() {
	flag.StringVar(&name, "n", "", "Queue name")
	flag.StringVar(&folder, "f", "/", "Folder to watch")
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

	singleChain := promhttp.InstrumentHandlerInFlight(inFlightGauge,
		promhttp.InstrumentHandlerDuration(duration.MustCurryWith(prometheus.Labels{"handler": "expert"}),
			promhttp.InstrumentHandlerCounter(counter,
				promhttp.InstrumentHandlerResponseSize(responseSize, http.HandlerFunc(singleHandler)),
			),
		),
	)
	http.Handle("/single", singleChain)

	statusChain := NoCache(promhttp.InstrumentHandlerInFlight(inFlightGauge,
		promhttp.InstrumentHandlerDuration(duration.MustCurryWith(prometheus.Labels{"handler": "status"}),
			promhttp.InstrumentHandlerCounter(counter,
				promhttp.InstrumentHandlerResponseSize(responseSize, http.HandlerFunc(statusHandler)),
			),
		),
	))
	http.Handle("/status", statusChain)

	queueChain := NoCache(promhttp.InstrumentHandlerInFlight(inFlightGauge,
		promhttp.InstrumentHandlerDuration(duration.MustCurryWith(prometheus.Labels{"handler": "failed"}),
			promhttp.InstrumentHandlerCounter(counter,
				promhttp.InstrumentHandlerResponseSize(responseSize, http.HandlerFunc(queueHandler)),
			),
		),
	))
	http.Handle("/scheduled", queueChain)

	failedChain := NoCache(promhttp.InstrumentHandlerInFlight(inFlightGauge,
		promhttp.InstrumentHandlerDuration(duration.MustCurryWith(prometheus.Labels{"handler": "failed"}),
			promhttp.InstrumentHandlerCounter(counter,
				promhttp.InstrumentHandlerResponseSize(responseSize, http.HandlerFunc(failedHandler)),
			),
		),
	))
	http.Handle("/failed", failedChain)

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

		for _, message := range result.Messages {
			var data Records
			if err := json.NewDecoder(strings.NewReader(*message.Body)).Decode(&data); err != nil {
				log.Println("error:", err)
			}

			if data.Data[0].Source == "aws:s3" {
				log.Printf("Received %d messages.\n", len(result.Messages))
				log.Println(result.Messages)

				object := data.Data[0].Data.Object
				if strings.HasPrefix(object.Name, folder) {
					if object.Size > 0 && data.Data[0].Event == "ObjectCreated:Put" {
						if strings.Contains(object.Name, "input_params.json") {
							jFolder := filepath.Dir(object.Name)

							// Create Job Queue
							var jobs []Job
							jobs = append(jobs, Job{ID: uuid.New().String(), Folder: jFolder, Complete: false, SLR: -1, Tide: -1, Analysis: -1})
							jobsCreated.Inc()
							(*pQueue)[jFolder] = jobs
						}

						if strings.Contains(object.Name, "input.geojson") {
							jFolder := filepath.Dir(object.Name)

							// Create Job Queue
							var jobs []Job
							for tide := 0; tide < 3; tide++ {
								for analysis := 0; analysis < 3; analysis++ {
									jobs = append(jobs, Job{ID: uuid.New().String(), Folder: jFolder, Complete: false, SLR: 1.0, Tide: tide, Analysis: analysis})
									jobsCreated.Inc()
								}
							}
							(*pQueue)[jFolder] = jobs
						}
					}
					
					if _, err := svc.DeleteMessage(&sqs.DeleteMessageInput{QueueUrl: resultURL.QueueUrl, ReceiptHandle: result.Messages[0].ReceiptHandle}); err != nil {
						log.Println("Delete Error", err)
						return
					}
				}
			}
		}
	}
}
