FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod ./
COPY cmd ./cmd

RUN go build -o /out/tasq-mcp ./cmd/server

FROM alpine:3.21
WORKDIR /app

COPY --from=build /out/tasq-mcp /app/tasq-mcp

EXPOSE 8091

ENTRYPOINT ["/app/tasq-mcp"]
