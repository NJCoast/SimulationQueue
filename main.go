package main

import (
	"flag"
	"log"
	"strings"
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/aws/aws-sdk-go/aws"
    "github.com/aws/aws-sdk-go/aws/awserr"
    "github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
)

type Records struct {
	Data []Record `json:"Records"`
}

type Record struct {
	Source string `json:"eventSource"`
	Event string `json:"eventName"`
	Data S3Record `json:"s3"`
}

type S3Record struct {
	Object S3ObjectRecord `json:"object"`
}

type S3ObjectRecord struct {
	Name string `json:"key"`
	Size int `json:"size"`
	eTag string `json:"eTag"`
}

func main() {
	var name string
    flag.StringVar(&name, "n", "", "Queue name")
    flag.Parse()

    if len(name) == 0 {
        flag.PrintDefaults()
        log.Fatalln("Queue name required")
    }

	// Setup AWS
    sess, err := session.NewSession(&aws.Config{
        Region: aws.String("us-east-1")},
	)
	
	svc := sqs.New(sess)
	
    resultURL, err := svc.GetQueueUrl(&sqs.GetQueueUrlInput{ QueueName: aws.String(name) })
    if err != nil {
        if aerr, ok := err.(awserr.Error); ok && aerr.Code() == sqs.ErrCodeQueueDoesNotExist {
            log.Fatalf("Unable to find queue %q\n", name)
        }
        log.Fatalf("Unable to queue %q, %v.\n", name, err)
	}

	// Setup Kubernetes
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	for{
		result, err := svc.ReceiveMessage(&sqs.ReceiveMessageInput{
			QueueUrl: resultURL.QueueUrl,
			AttributeNames: aws.StringSlice([]string{"SentTimestamp"}),
			MaxNumberOfMessages: aws.Int64(1),
			MessageAttributeNames: aws.StringSlice([]string{"ALL"}),
			WaitTimeSeconds: aws.Int64(20),
		})
		if err != nil {
			log.Fatalf("Unable to receive message from queue %q, %v.\n", name, err)
		}

		if len(result.Messages) > 0 {
			log.Printf("Received %d messages.\n", len(result.Messages))
			log.Println(result.Messages)

			var data Records
			if err := json.NewDecoder(strings.NewReader(*result.Messages[0].Body)).Decode(&data); err != nil {
				log.Println("error:", err)
			}
			
			if data.Data[0].Source == "aws:s3" && data.Data[0].Event == "ObjectCreated:Put" && strings.Contains(data.Data[0].Data.Object.Name, "input_params.json") {
				jobsClient := clientset.BatchV1().Jobs("default")

				var falseVal = false
				batchJob := &batchv1.Job{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Job",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						GenerateName:   "runup-",
						Labels: make(map[string]string),
					},
					Spec: batchv1.JobSpec{
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								GenerateName:   "runup-",
								Labels: make(map[string]string),
							},
							Spec: corev1.PodSpec{
								InitContainers: []corev1.Container{},
								Containers: []corev1.Container{
									{
										Name:    "runup",
										Image:   "perl",
										Command: []string{"sleep", "30"},
										SecurityContext: &corev1.SecurityContext{
											Privileged: &falseVal,
										},
										ImagePullPolicy: corev1.PullPolicy(corev1.PullIfNotPresent),
										Env:             []corev1.EnvVar{},
										VolumeMounts:    []corev1.VolumeMount{},
									},
								},
								RestartPolicy:    corev1.RestartPolicyOnFailure,
								Volumes:          []corev1.Volume{},
								ImagePullSecrets: []corev1.LocalObjectReference{},
							},
						},
					},
				}

				newJob, err := jobsClient.Create(batchJob)
				if err != nil {
					log.Fatalln(err)
				}

				log.Println("New job name: ", newJob.Name)
			}

			if _, err := svc.DeleteMessage(&sqs.DeleteMessageInput{QueueUrl: resultURL.QueueUrl, ReceiptHandle: result.Messages[0].ReceiptHandle }); err != nil {
				log.Println("Delete Error", err)
				return
			}
		}
	}
}
