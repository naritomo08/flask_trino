FROM golang:1.22-alpine AS builder

WORKDIR /src

COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/trino-log-search .

FROM alpine:3.20

WORKDIR /app

COPY --from=builder /out/trino-log-search /usr/local/bin/trino-log-search
COPY templates ./templates
COPY static ./static

EXPOSE 5000

CMD ["trino-log-search"]
