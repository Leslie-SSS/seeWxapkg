# 构建阶段 - Go
FROM golang:1.23-alpine AS go-builder

WORKDIR /app

ENV GOPROXY=https://goproxy.cn,direct
ENV GO111MODULE=on

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server ./cmd/server

# 构建阶段 - Node.js dependencies
FROM node:20-alpine AS node-builder

WORKDIR /beautify

COPY internal/beautify/package.json ./
COPY internal/beautify/package-lock.json ./
RUN npm ci --omit=dev

WORKDIR /beautify/wxappUnpacker
COPY internal/beautify/wxappUnpacker/package.json ./
COPY internal/beautify/wxappUnpacker/package-lock.json ./
RUN npm ci --omit=dev

# 运行阶段
FROM node:20-alpine

WORKDIR /app

# 复制 Go 服务
COPY --from=go-builder /app/server .

# 复制 Node.js 美化服务
COPY --from=node-builder /beautify/node_modules ./internal/beautify/node_modules
COPY --from=node-builder /beautify/wxappUnpacker/node_modules ./internal/beautify/wxappUnpacker/node_modules
COPY internal/beautify/core.js ./internal/beautify/
COPY internal/beautify/plugins ./internal/beautify/plugins
COPY internal/beautify/wxappUnpacker ./internal/beautify/wxappUnpacker
COPY internal/beautify/server.js ./internal/beautify/
COPY internal/beautify/package.json ./internal/beautify/
COPY internal/beautify/package-lock.json ./internal/beautify/

# 创建必要目录
RUN mkdir -p /tmp/seewxapkg /output

# 环境变量
ENV BEAUTIFY_ENABLED=false
ENV BEAUTIFY_TIMEOUT=5
ENV BEAUTIFY_MAX_FILE_SIZE=512000
ENV DEOBFUSCATE_ENABLED=true
ENV TEMP_DIR=/tmp/seewxapkg
ENV OUTPUT_DIR=/output

EXPOSE 8080 9090

HEALTHCHECK --interval=30s --timeout=3s \
  CMD sh -c 'wget --no-verbose --tries=1 --spider "http://localhost:${SERVER_PORT:-9090}/api/health" || exit 1'

CMD ["./server"]
