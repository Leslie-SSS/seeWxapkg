# 单机生产部署

本目录提供 API、离线 Worker、前端的生产 Compose，以及供现有 TLS Nginx 网关加载的站点配置。任务状态与队列使用 Docker named volumes，不需要外部数据库、缓存或消息队列；Worker 默认无外部网络。API 仅为首页 Star 数向固定的 GitHub 仓库接口发起短时缓存查询，失败不会影响反编译。

> `docker-compose.yml` 不会创建或管理公网 Nginx。外部网关必须加入项目专用的 `seewxapkg-network`，并实际加载 `nginx-seewxapkg.conf`，否则域名无法访问这些容器。不要把应用接入承载无关服务的共享网络。

## 使用前检查

1. 将 `.env.example` 复制为 `.env`，使用不可变镜像标签或 digest。
2. 在 `nginx-seewxapkg.conf` 中替换域名和证书路径。
3. 在站点公网开放前，由上层网关接入身份认证。本示例只包含 TLS、限流、安全响应头和日志最小化，不包含用户体系。
4. 确认 Docker volumes 位于加密或受访问控制的磁盘，并按需要调整 `RETAIN_ARTIFACTS_HOURS`。

## 创建共享网络并启动应用

生产 Compose 将 `seewxapkg-network` 声明为外部专用网络，因此首次部署必须先创建；网络已存在时不要重复创建。

```bash
docker network inspect seewxapkg-network >/dev/null 2>&1 || docker network create seewxapkg-network
docker compose config
docker compose pull
docker compose up -d
docker compose ps
```

以上命令应在本目录执行。继续下一步前，确认 `backend`、`worker`、`frontend` 均为 `healthy`。

> Worker 无网络栈且不暴露 HTTP 端口，其健康检查探测 `worker` 进程本身（而非可选的 3001 美化 sidecar）；进程退出时容器会自动重启。

## 接入外部 Nginx 网关

推荐在网关自己的 Compose 中持久声明项目专用网络和只读配置挂载；下面的 `gateway`、容器路径及宿主机绝对路径应按实际部署替换，并保留网关原有网络：

```yaml
services:
  gateway:
    volumes:
      - /absolute/path/to/deploy/production/nginx-seewxapkg.conf:/etc/nginx/conf.d/seewxapkg.conf:ro
    networks:
      - seewxapkg-network

networks:
  seewxapkg-network:
    external: true
```

对于已经运行、尚未纳入 Compose 网络声明的网关，可先执行一次以下命令完成当前容器接入；随后仍应把网络写回网关 Compose，避免容器重建后丢失：

```bash
export SEEWX_GATEWAY_CONTAINER=your-nginx-container
export SEEWX_PUBLIC_DOMAIN=example.com
docker network connect seewxapkg-network "${SEEWX_GATEWAY_CONTAINER}"
```

网关和应用容器启动后，确认网络成员中同时出现网关、`seewxapkg-backend` 与 `seewxapkg-frontend`：

```bash
docker network inspect seewxapkg-network --format '{{range .Containers}}{{println .Name}}{{end}}'
```

配置文件必须位于 Nginx 的 `http` 上下文（官方镜像默认加载 `/etc/nginx/conf.d/*.conf`）。每次新增或修改配置都先确认网关确实加载了该文件，再检查语法；只有检查通过才重载：

```bash
docker exec "${SEEWX_GATEWAY_CONTAINER}" nginx -T 2>&1 | grep -F "${SEEWX_PUBLIC_DOMAIN}"
docker exec "${SEEWX_GATEWAY_CONTAINER}" nginx -t && \
  docker exec "${SEEWX_GATEWAY_CONTAINER}" nginx -s reload
curl -fsS "https://${SEEWX_PUBLIC_DOMAIN}/health"
```

配置使用 Docker DNS 动态解析容器地址，容器重建后无需靠手工重载刷新旧 IP。`nginx -t` 若报告解析器或 upstream 配置错误，通常表示网关未加入 `seewxapkg-network`，或 Nginx 版本过旧；不要在语法检查失败时重载。

最后用已获授权的 `.wxapkg` 完成一次“上传 → 反编译 → 下载”验收，并确认 ZIP 的所有条目都位于 `src/` 下。

## 从旧版本升级

旧版本曾生成包含其他顶层目录的 ZIP。升级镜像并确认队列为空后，先停止写入，再使用随镜像提供的一次性工具重建所有可下载包；迁移只读取现有 `result/src`，不会执行包内代码：

```bash
docker compose stop backend worker
docker compose run --rm --no-deps worker ./repack-src-only
docker compose up -d backend worker frontend
```

新版本启动时还会原子移除旧任务状态中的 AppID、原文件名和原大小，删除终态任务遗留的 input、fallback 与 raw，并把保留的任务树收紧为目录 `0700`、文件 `0600`。执行升级前创建的备份同样包含敏感数据，应加密、限制访问，并在回滚窗口结束后安全销毁。

## 数据与日志

- AppID 为独立 Worker 解密而短暂保存在 `0600` 的一次性凭据文件中，不进入任务状态、队列或报告，完成一次解密尝试或任务失败后立即删除。
- 为优先保护凭据，AppID 删除后的加密任务若因进程异常中断，需要重新提交；删除前异常遗留的凭据由保留期清理器兜底删除。
- 正常终态立即删除原始上传、fallback 和 raw 重复副本；异常遗留、任务状态、恢复源码、报告、失败队列记录和 ZIP 由 API 清理器按保留时长清理。服务停机时清理器不会运行。
- `docker compose down` 保留 named volumes；确认无需恢复后，`docker compose down -v` 才会删除它们。
- 网关访问日志关闭；严重基础设施错误日志仍可能包含请求上下文，必须按敏感数据限制访问与保留。任务 ID 仍应视为临时访问凭证。
