FROM golang:1.24-alpine AS builder
WORKDIR /src
# go.mod 可能要求更高版本 toolchain 时自动拉取
ENV GOTOOLCHAIN=auto
RUN apk add --no-cache git ca-certificates tzdata
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/onepaper ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata wget
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY --from=builder /out/onepaper /app/onepaper
# 默认配置路径；运行时可通过挂载覆盖 /app/config.yaml
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/onepaper"]
CMD ["-config", "/app/config.yaml"]
