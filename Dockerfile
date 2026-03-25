# 构建说明：
# 1) go.mod 为 go 1.25.x 时，请使用 **golang:1.25-*** 镜像，并设 GOTOOLCHAIN=local，
#    避免在 1.24 镜像里自动下载 go1.25 toolchain（走 proxy.golang.org，国内易超时）。
# 2) GOPROXY 默认 goproxy.cn，加速拉模块；海外可：docker build --build-arg GOPROXY=https://proxy.golang.org,direct .
# 3) apk 很慢时可在构建机换 Alpine 源或使用网络更好的环境。

FROM golang:1.25-alpine AS builder
WORKDIR /src
ENV GOTOOLCHAIN=local
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
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
COPY default.png /app/default.png
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/onepaper"]
CMD ["-config", "/app/config.yaml"]
