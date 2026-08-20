# Build stage: pure-Go, no CGO — compiles the static binary.
FROM golang:1.26-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=docker
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /gobby ./src

# Run stage: tiny Alpine + system ffmpeg (Gobby uses it from PATH; no runtime download).
FROM alpine:3.20
RUN apk add --no-cache ffmpeg ca-certificates
COPY --from=build /gobby /usr/local/bin/gobby

# /media = your library (mount read-only). /data = gobby.db (persistent volume).
ENV GOBBY_PATH=/media \
    GOBBY_DATA=/data \
    GOBBY_PORT=8420 \
    GOBBY_NO_BROWSER=1
VOLUME ["/data"]
EXPOSE 8420

ENTRYPOINT ["gobby"]
