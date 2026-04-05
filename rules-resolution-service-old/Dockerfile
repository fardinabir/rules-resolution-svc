FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /rules-resolution-service ./cmd/server

FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=builder /rules-resolution-service-old .
COPY --from=builder /app/migrations ./migrations
COPY --from=builder /app/env.yaml ./env.yaml
COPY --from=builder /app/sr_backend_assignment_data ./sr_backend_assignment_data
EXPOSE 8080 8081
ENTRYPOINT ["/app/rules-resolution-service"]
