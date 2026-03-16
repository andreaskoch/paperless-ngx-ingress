FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /paperless-ngx-ingress .

FROM alpine:3.21

RUN apk --no-cache add ca-certificates
RUN adduser -D -u 1000 appuser

COPY --from=builder /paperless-ngx-ingress /usr/local/bin/paperless-ngx-ingress

USER appuser
EXPOSE 8471

ENTRYPOINT ["paperless-ngx-ingress"]
