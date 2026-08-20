# 构建阶段 - Go
FROM golang:1.26-alpine AS go-builder

ARG VERSION=2.0.0-dev
ARG GIT_SHA=worktree
ARG BUILD_DATE=unknown

WORKDIR /app

ENV GOPROXY=https://goproxy.cn,direct
ENV GO111MODULE=on

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN BUILD_LDFLAGS="-s -w -X github.com/keepbuild/seewxapkg/internal/version.Version=${VERSION} -X github.com/keepbuild/seewxapkg/internal/version.Commit=${GIT_SHA} -X github.com/keepbuild/seewxapkg/internal/version.BuiltAt=${BUILD_DATE}" && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="$BUILD_LDFLAGS" -o server ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="$BUILD_LDFLAGS" -o worker ./cmd/worker && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="$BUILD_LDFLAGS" -o repack-src-only ./cmd/repack

# 构建阶段 - Node.js dependencies
FROM node:24-alpine AS node-builder

WORKDIR /beautify

COPY internal/beautify/package.json internal/beautify/package-lock.json ./
RUN npm ci --omit=dev

WORKDIR /beautify/wxappUnpacker
COPY internal/beautify/wxappUnpacker/package.json internal/beautify/wxappUnpacker/package-lock.json ./
RUN npm ci --omit=dev

# 运行阶段
FROM node:24-alpine

WORKDIR /app

# 复制 Go 服务
COPY --chown=node:node --from=go-builder /app/server .
COPY --chown=node:node --from=go-builder /app/worker .
COPY --chown=node:node --from=go-builder /app/repack-src-only .

# 复制 Node.js 美化服务
COPY --chown=node:node --from=node-builder /beautify/node_modules ./internal/beautify/node_modules
COPY --chown=node:node --from=node-builder /beautify/wxappUnpacker/node_modules ./internal/beautify/wxappUnpacker/node_modules
COPY --chown=node:node internal/beautify/core.js internal/beautify/server.js internal/beautify/package.json ./internal/beautify/
COPY --chown=node:node internal/beautify/plugins ./internal/beautify/plugins
COPY --chown=node:node internal/beautify/runtime ./internal/beautify/runtime
COPY --chown=node:node internal/beautify/wxappUnpacker/*.js ./internal/beautify/wxappUnpacker/
COPY --chown=node:node internal/beautify/wxappUnpacker/LICENSE internal/beautify/wxappUnpacker/package.json ./internal/beautify/wxappUnpacker/

# 安装时区数据，使生产环境的 TZ 配置同时作用于 Go 与 Node 日志。
RUN apk add --no-cache tzdata && \
    mkdir -p /tmp/seewxapkg /output /data/tasks /data/output /data/samples && \
    chown -R node:node /app /tmp/seewxapkg /output /data

# 环境变量
ENV BEAUTIFY_ENABLED=true
ENV BEAUTIFY_TIMEOUT=30
ENV BEAUTIFY_MAX_FILE_SIZE=8388608
ENV BEAUTIFY_FORMAT_MAX_FILE_SIZE=4194304
ENV BEAUTIFY_WORKER_MEMORY_MB=384
ENV BEAUTIFY_WORKERS=2
ENV DEOBFUSCATE_ENABLED=false
ENV TEMP_DIR=/tmp/seewxapkg
ENV OUTPUT_DIR=/output

EXPOSE 8080 9090

USER node

HEALTHCHECK --interval=30s --timeout=3s \
  CMD sh -c 'wget --no-verbose --tries=1 --spider "http://localhost:${SERVER_PORT:-9090}/api/health" || exit 1'

CMD ["./server"]
