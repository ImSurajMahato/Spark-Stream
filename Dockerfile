FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" -o /spark ./cmd/fsb
FROM alpine:3.23
RUN apk add --no-cache ca-certificates && addgroup -g 10001 spark && adduser -D -u 10001 -G spark spark
WORKDIR /app
RUN mkdir -p /app/data /app/logs && chown -R spark:spark /app
COPY --from=builder /spark /usr/local/bin/spark-stream
USER spark
WORKDIR /app/data
EXPOSE 8080
ENTRYPOINT ["spark-stream", "run"]
