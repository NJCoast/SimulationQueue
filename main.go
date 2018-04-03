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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
    "github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
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
	Worker   string
	Complete bool
	SLR      float64 `json:"slr"`
	Tide     int     `json:"tide"`
	Analysis int     `json:"analysis"`
}

var ParameterQueue map[string][]Job
var name string
var sess *session.Session

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

	queue := make(chan string)
	go S3MessageQueue(name, queue, &ParameterQueue)
	go KubernetesJobQueue(&ParameterQueue, queue)

	http.HandleFunc("/", clientHandler)
	http.HandleFunc("/single", singleHandler)
	http.HandleFunc("/status", statusHandler)
	log.Fatalln(http.ListenAndServe(fmt.Sprintf(":%d", 9090), nil))
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST,GET,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Origin, X-Requested-With, Content-Type, Accept");

	if r.Method == "GET" {
		if err := r.ParseForm(); err != nil {
			fmt.Fprintf(w, "ParseForm() err: %v", err)
			return
		}

		username := r.FormValue("name")
		id := r.FormValue("id")
		log.Println(username, id)

		if username == "" || id == "" {
			return
		}

		folder := fmt.Sprintf("simulation/%s/%s", username, id)

		result := struct {
			Complete bool `json:"complete"`
		}{
			ParameterQueue[folder][0].Complete,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(&result); err != nil {
			log.Println(err)
			return
		}
	}
}

func singleHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST,GET,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Origin, X-Requested-With, Content-Type, Accept");
	log.Printf("%+v\n", r)

	if r.Method == "POST" {
		if err := r.ParseForm(); err != nil {
			fmt.Fprintf(w, "ParseForm() err: %v", err)
			return
		}

		username := r.FormValue("name")
		id := r.FormValue("id")
		log.Println(username, id)

		if username == "" || id == "" {
			return
		}

		result, err := s3manager.NewUploader(sess).Upload(&s3manager.UploadInput{
			ACL:         aws.String("public-read"),
			Bucket:      aws.String("simulation.njcoast.us"),
			Key:         aws.String(fmt.Sprintf("simulation/%s/%s/input_params.json", username, id)),
			ContentType: aws.String("application/json"),
			Body:        r.Body,
		})

		log.Println(result, err)
	}
}

func S3MessageQueue(qName string, queue chan<- string, pQueue *map[string][]Job) {
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
					queue <- folder

					// Create Job Queue
					var jobs []Job
					jobs = append(jobs, Job{ID: uuid.New().String(), Complete: false, SLR: -1, Tide: -1, Analysis: -1})
					(*pQueue)[folder] = jobs
				}

				if data.Data[0].Source == "aws:s3" && data.Data[0].Event == "ObjectCreated:Put" && strings.Contains(data.Data[0].Data.Object.Name, "input.geojson") {
					folder := filepath.Dir(data.Data[0].Data.Object.Name)
					queue <- folder

					// Create Job Queue
					var jobs []Job
					for slr := 0.0; slr <= 1.5; slr += 0.5 {
						for tide := 0; tide < 3; tide++ {
							for analysis := 0; analysis < 3; analysis++ {
								jobs = append(jobs, Job{ID: uuid.New().String(), Complete: false, SLR: slr, Tide: tide, Analysis: analysis})
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

func KubernetesJobQueue(pQueue *map[string][]Job, queue <-chan string) {
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	for {
		select {
		case folder := <-queue:
			// Start Workers
			var falseVal = false

			var parallelism int32 = 1
			if len((*pQueue)[folder]) > 1 {
				parallelism = 4
			}

			batchJob := &batchv1.Job{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Job",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "runup-",
					Labels:       make(map[string]string),
				},
				Spec: batchv1.JobSpec{
					Parallelism: &parallelism,
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							GenerateName: "runup-",
							Labels:       make(map[string]string),
						},
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{},
							Containers: []corev1.Container{
								{
									Name:  "runup",
									Image: "234514569215.dkr.ecr.us-east-1.amazonaws.com/simulation-queue:worker",
									SecurityContext: &corev1.SecurityContext{
										Privileged: &falseVal,
									},
									ImagePullPolicy: corev1.PullPolicy(corev1.PullAlways),
									Env: []corev1.EnvVar{
										{
											Name: "AWS_ACCESS_KEY_ID",
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "simulationqueue",
													},
													Key: "AWS_ACCESS_KEY_ID",
												},
											},
										},
										{
											Name: "AWS_SECRET_ACCESS_KEY",
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "simulationqueue",
													},
													Key: "AWS_SECRET_ACCESS_KEY",
												},
											},
										},
										{
											Name: "POD_NAME",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													FieldPath: "metadata.name",
												},
											},
										},
										{
											Name:  "JOB_FOLDER",
											Value: folder,
										},
									},
									VolumeMounts: []corev1.VolumeMount{},
								},
							},
							RestartPolicy:    corev1.RestartPolicyOnFailure,
							Volumes:          []corev1.Volume{},
							ImagePullSecrets: []corev1.LocalObjectReference{{Name: "awsecr-cred"}},
						},
					},
				},
			}

			newJob, err := clientset.BatchV1().Jobs("default").Create(batchJob)
			if err != nil {
				log.Fatalln(err)
			}
			log.Println("New job name: ", newJob.Name)
		}
	}
}

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

	c := &Socket{Connection: ws, WorkerID: r.FormValue("id"), WorkerFolder: r.FormValue("folder"), Send: make(chan string, 10)}
	log.Println("Added", c.WorkerID)

	go c.Write()
	c.Read()

	for i := 0; i < len(ParameterQueue[c.WorkerFolder]); i++ {
		if !ParameterQueue[c.WorkerFolder][i].Complete && ParameterQueue[c.WorkerFolder][i].Worker == c.WorkerID {
			ParameterQueue[c.WorkerFolder][i].Worker = ""
		}
	}

	log.Println("Deleted", c.WorkerID)
}

type Socket struct {
	Connection   *websocket.Conn
	WorkerID     string
	WorkerFolder string
	Send         chan string
}

func (s *Socket) Read() {
	defer func() {
		s.Connection.Close()
	}()

	s.Connection.SetReadLimit(1024)
	s.Connection.SetReadDeadline(time.Now().Add(60 * time.Second))
	s.Connection.SetPongHandler(func(string) error {
		s.Connection.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := s.Connection.ReadMessage()
		if err != nil {
			break
		}
		log.Println("Recieved["+s.WorkerID+"]", string(message))

		parts := strings.Split(string(message), ":")

		switch parts[0] {
		case "GET":
			found := false
			for i := 0; i < len(ParameterQueue[s.WorkerFolder]); i++ {
				if !ParameterQueue[s.WorkerFolder][i].Complete && ParameterQueue[s.WorkerFolder][i].Worker == "" {
					data, err := json.Marshal(&ParameterQueue[s.WorkerFolder][i])
					if err != nil {
						log.Fatalln(err)
					}

					found = true
					s.Send <- "DATA:" + string(data)
					break
				}
			}

			// No more work
			if !found {
				s.Send <- "DATA:"
			}
		case "COMPLETE":
			for i := 0; i < len(ParameterQueue[s.WorkerFolder]); i++ {
				if parts[1] == ParameterQueue[s.WorkerFolder][i].ID {
					ParameterQueue[s.WorkerFolder][i].Complete = true
				}
			}
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
