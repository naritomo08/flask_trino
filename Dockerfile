FROM golang:1.22 AS build

WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN go test ./...
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/flask-trino .

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=build /out/flask-trino /app/flask-trino
COPY templates /app/templates
COPY static /app/static

ENV PORT=5000
ENV TEMPLATE_DIR=/app/templates
ENV STATIC_DIR=/app/static

EXPOSE 5000

ENTRYPOINT ["/app/flask-trino"]
