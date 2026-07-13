ARG GO_BUILDER=golang:1.25.12-alpine3.23@sha256:cc985ef6f9c3bf9ece7488129c9abe0a150388ccdfa428d886fc709dca0b230a
FROM ${GO_BUILDER} AS builder
ARG TARGETARCH
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY apps/demo-app ./apps/demo-app
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/demo-app ./apps/demo-app

FROM scratch
USER 65532:65532
COPY --from=builder /out/demo-app /demo-app
ENTRYPOINT ["/demo-app"]
