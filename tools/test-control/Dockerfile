FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod ./
RUN go mod download || true
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/test-control .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/test-control /app/test-control
EXPOSE 8080
ENTRYPOINT ["/app/test-control"]
