FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o server ./cmd/server

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/server .

ENV HOST=0.0.0.0
ENV PORT=8045
ENV API_USER_AGENT='antigravity/1.11.17 windows/amd64'

ENV DATA_DIR=./data
ENV TIMEOUT=180000
ENV ENDPOINT_MODE=production

ENV RETRY_STATUS_CODES=429,500
ENV RETRY_MAX_ATTEMPTS=3
ENV DEBUG=off

# Go 内存优化配置
ENV GOMEMLIMIT=100MiB
ENV GOGC=50

EXPOSE 8045
CMD ["./server"]
