FROM golang:1.9.4
WORKDIR /go/src/github.com/omegaice/HurricaneSimulationQueue/
COPY main.go .
RUN go get k8s.io/client-go/... && CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o HurricaneSimulationQueue .

FROM alpine:3.7  
WORKDIR /root/
COPY --from=0 /go/src/github.com/omegaice/HurricaneSimulationQueue/HurricaneSimulationQueue .
CMD ["./HurricaneSimulationQueue"] 