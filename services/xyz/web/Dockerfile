ARG NODE_IMAGE=node:24.17.0-alpine@sha256:156b55f92e98ccd5ef49578a8cea0df4679826564bad1c9d4ef04462b9f0ded6
ARG GO_BUILDER=golang:1.25.13-alpine3.23@sha256:4ce6af6747b07e99ca3a57eadb77565787390a41b0039dcc8e09ec4c57cfa125
FROM ${NODE_IMAGE} AS frontend
WORKDIR /src
COPY services/xyz/web/package.json services/xyz/web/package-lock.json ./
RUN npm ci
COPY services/xyz/web ./
RUN npm run build
FROM ${GO_BUILDER} AS server
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY services/xyz/web/server ./services/xyz/web/server
COPY --from=frontend /src/dist ./services/xyz/web/server/static
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/web ./services/xyz/web/server
FROM scratch
USER 65532:65532
COPY --from=server /out/web /web
ENTRYPOINT ["/web"]
