FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/tasq-server ./cmd/server

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /out/tasq-server /app/tasq-server
EXPOSE 8080
CMD ["/app/tasq-server"]
