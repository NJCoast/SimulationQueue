FROM golang:1.9.4
WORKDIR /go/src/github.com/NJCoast/SimulationQueue/
RUN go get k8s.io/client-go/...
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