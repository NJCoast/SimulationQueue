FROM golang:1.13.0
WORKDIR /go/src/github.com/NJCoast/SimulationQueue/
RUN mkdir -p $GOPATH/k8s.io/ && cd $GOPATH/k8s.io && git clone https://github.com/kubernetes/klog && cd klog
RUN go get -d k8s.io/client-go/... && cd $GOPATH/k8s.io/klog && git checkout a6a74fbce3a592242b0fc24cd93fd98a4cea0a98 && go install k8s.io/client-go/...
RUN go get github.com/google/uuid
RUN go get github.com/gorilla/websocket
RUN go get github.com/aws/aws-sdk-go/aws
RUN go get github.com/aws/aws-sdk-go/aws/awserr
RUN go get github.com/aws/aws-sdk-go/aws/session
RUN go get github.com/aws/aws-sdk-go/service/sqs
RUN go get github.com/prometheus/client_golang/prometheus
RUN go get github.com/prometheus/client_golang/prometheus/promhttp
COPY *.go /go/src/github.com/NJCoast/SimulationQueue/
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o SimulationQueue .

FROM alpine:3.7  
RUN apk add --no-cache ca-certificates
WORKDIR /root/
COPY --from=0 /go/src/github.com/NJCoast/SimulationQueue .
CMD ["./SimulationQueue", "-n", "simulation_queue"] 