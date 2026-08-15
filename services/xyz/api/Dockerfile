ARG GO_BUILDER=golang:1.25.13-alpine3.23@sha256:4ce6af6747b07e99ca3a57eadb77565787390a41b0039dcc8e09ec4c57cfa125
FROM ${GO_BUILDER} AS builder
ARG TARGETARCH
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY services/xyz/api ./services/xyz/api
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/service ./services/xyz/api
FROM scratch
USER 65532:65532
COPY --from=builder /out/service /service
ENTRYPOINT ["/service"]
