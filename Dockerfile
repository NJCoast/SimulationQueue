FROM golang:1.9.4
WORKDIR /go/src/github.com/NJCoast/SimulationQueue/
COPY main.go .
RUN go get k8s.io/client-go/... && \
    go get github.com/aws/aws-sdk-go/aws && \
    go get github.com/aws/aws-sdk-go/aws/awserr && \
    go get github.com/aws/aws-sdk-go/aws/session && \
    go get github.com/aws/aws-sdk-go/service/sqs && \
    CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o SimulationQueue .

FROM alpine:3.7  
RUN apk add --no-cache ca-certificates
WORKDIR /root/
COPY --from=0 /go/src/github.com/NJCoast/SimulationQueue .
CMD ["./SimulationQueue", "-n", "simulation_queue"] 