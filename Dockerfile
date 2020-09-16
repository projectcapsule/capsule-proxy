FROM golang:1.13 as builder
WORKDIR /workspace
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download
COPY main.go main.go
COPY webserver webserver
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o capsule-ns-filter main.go

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/capsule-ns-filter .
USER nonroot:nonroot

ENTRYPOINT ["/capsule-ns-filter"]