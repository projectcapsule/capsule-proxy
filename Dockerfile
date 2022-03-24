FROM golang:1.18-alpine as builder
WORKDIR /workspace
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download
COPY main.go main.go
COPY internal internal
COPY api api
ARG GCFLAGS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} GO111MODULE=on go build -gcflags "${GCFLAGS}" -a -o capsule-proxy main.go

FROM golang:1.18-alpine as dlv
RUN CGO_ENABLED=0 go install github.com/go-delve/delve/cmd/dlv@latest
WORKDIR /
COPY --from=builder /workspace/capsule-proxy .
ENTRYPOINT ["dlv", "--listen=:2345", "--headless=true", "--api-version=2", "--accept-multiclient", "exec", "--", "/capsule-proxy"]

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/capsule-proxy .
USER nonroot:nonroot
ENTRYPOINT ["/capsule-proxy"]
