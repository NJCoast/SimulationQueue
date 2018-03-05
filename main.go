package main

import (
	"fmt"
	"time"
	"log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	for {
		jobsClient := clientset.BatchV1().Jobs("default")

		jobsList, err := jobsClient.List(metav1.ListOptions{})
		if err != nil {
			log.Fatalln(err)
		}

		// Loop over all jobs and print their name
		for i, job := range jobsList.Items {
			fmt.Printf("Job %d: %s\n", i, job.Name)
		}

		var falseVal = false

		batchJob := &batchv1.Job{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Job",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:   "k8sexp-testjob",
				Labels: make(map[string]string),
			},
			Spec: batchv1.JobSpec{
				// Optional: Parallelism:,
				// Optional: Completions:,
				// Optional: ActiveDeadlineSeconds:,
				// Optional: Selector:,
				// Optional: ManualSelector:,
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "k8sexp-testpod",
						Labels: make(map[string]string),
					},
					Spec: corev1.PodSpec{
						InitContainers: []corev1.Container{}, // Doesn't seem obligatory(?)...
						Containers: []corev1.Container{
							{
								Name:    "k8sexp-testimg",
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
			// Optional, not used by pach: JobStatus:,
		}

		newJob, err := jobsClient.Create(batchJob)
		if err != nil {
			log.Fatalln(err)
		}

		fmt.Println("New job name: ", newJob.Name)

		time.Sleep(60 * time.Second)
	}
}
