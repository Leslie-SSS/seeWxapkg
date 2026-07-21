<div align="center">
  <h1>See Wxapkg</h1>
  <p><strong>开源、自托管的微信小程序 <code>.wxapkg</code> 反编译工具</strong></p>
  <p>上传文件，自动解密、解包、反编译和整理代码，完成后直接下载 <code>src/</code> 工程</p>
  <p>
    <a href="https://github.com/Leslie-SSS/seeWxapkg/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/Leslie-SSS/seeWxapkg/actions/workflows/ci.yml/badge.svg?branch=main" /></a>
    <a href="https://github.com/Leslie-SSS/seeWxapkg/stargazers"><img alt="GitHub Stars" src="https://img.shields.io/github/stars/Leslie-SSS/seeWxapkg?logo=github&color=14b8a6" /></a>
    <a href="#许可证"><img alt="许可证：MIT 与 GPL-3.0-or-later" src="https://img.shields.io/badge/license-MIT%20%2B%20GPL--3.0--or--later-22c55e" /></a>
  </p>
  <p>
    <a href="https://seewxapkg.keepbuild.cn/"><strong>在线体验</strong></a>
    · <a href="#快速开始">快速开始</a>
    · <a href="#支持范围与限制">支持范围</a>
    · <a href="#api">API</a>
    · <a href="#参与贡献">参与贡献</a>
  </p>
</div>

<p align="center">
  <a href="https://seewxapkg.keepbuild.cn/">
    <img src="./ScreenShot.png" alt="See Wxapkg V2 微信小程序反编译工具首页" width="100%" />
  </a>
</p>

See Wxapkg 可以直接在浏览器中反编译 `.wxapkg` 文件。它会自动识别包类型，在需要时使用 AppID 解密，并完成解包、反编译、排版整理和结果检查。

> [!IMPORTANT]
> 本项目所称“反编译”是对编译产物的**尽力静态恢复**，不是提取原始源码。结果可能缺少原变量名、注释、目录结构或运行时生成内容；“反编译结果已生成”也不代表与原工程完全一致或可以直接运行。

## 主要功能

- 支持普通包、加密包和常见微信 4.x 结构；加密包需要匹配的 AppID。
- 静态反编译，不通过 `eval`、`Function` 或 VM 执行包内 JavaScript。
- 自动反编译并整理 JSON、JavaScript、WXML 和 WXSS，无法确认的内容会如实标记。
- 提供实时进度、结果状态、静态质量分、检查提示和技术报告。
- 下载 ZIP 只包含 `src/`，原始解包依据不会混入交付文件。
- 支持在线使用与单机 Docker Compose 自托管，不依赖外部数据库、缓存或消息队列。

## 快速开始

### 在线使用

打开 [seewxapkg.keepbuild.cn](https://seewxapkg.keepbuild.cn/)，选择或拖入 `.wxapkg` 文件即可。代码整理和深度反编译默认开启；加密包还需要填写对应的 AppID。

> 公开体验站会在处理期间暂存上传内容；正常终态会立即删除原始上传，异常遗留及结果数据最长按 72 小时策略清理。敏感、私有或未获授权的软件包请勿上传，建议在受控环境中自托管处理。

### Docker Compose（推荐）

只需要 Docker Engine 与 Docker Compose：

```bash
git clone https://github.com/Leslie-SSS/seeWxapkg.git
cd seeWxapkg
docker compose up -d --build
```

启动后可访问：

- Web 界面：<http://localhost:3004>
- 健康检查：<http://localhost:9090/api/health>
- API 基地址：`http://localhost:9090/api`

默认 Compose 使用本地文件任务仓库、文件队列和独立 Worker，任务数据只保存在配置的本机卷中。这是单机部署方案，不依赖数据库、缓存或消息队列。停止服务时运行：

```bash
docker compose down
```

Named volumes 会保留任务状态与文件，已进入文件队列的普通未完成任务可在下次启动后继续；服务停机期间自动清理不会运行。为避免长期保存凭据，AppID 在一次解密尝试后即销毁，因此此后若进程意外中断，加密任务需要重新提交。如需立即删除所有上传文件、任务记录和产物，可确认不再需要这些数据后运行 `docker compose down -v`。

## 使用方法

1. 选择 `.wxapkg` 文件；加密包填写 18 位 AppID（`wx` + 16 位小写十六进制字符）。
2. 点击“开始反编译”，等待系统自动处理。
3. 查看结果提示并下载只含 `src/` 的反编译结果。

<details>
<summary><strong>在哪里查找 wxapkg 文件？</strong></summary>

不同微信版本的目录可能变化，下面仅作为常见位置参考：

- macOS：`~/Library/Containers/com.tencent.xinWeChat/Data/Documents/app_data/radium/users/{一串值}/applet/packages`
- Windows：`C:\Users\{用户名}\Documents\WeChat Files\Applet\{AppID}`

</details>

## 反编译结果

下载包是面向继续阅读和分析的干净工程：

```text
wxapkg-result.zip
└── src/
    ├── app.json
    ├── app.js
    ├── app.wxss
    └── ...
```

`reports/` 技术报告通过结果页或任务 API 查询，不会进入下载 ZIP。处理过程中产生的 fallback 临时副本会在合并后立即删除，不长期保留重复源码。

| 结果状态                                | 含义                                     | 建议                                 |
| --------------------------------------- | ---------------------------------------- | ------------------------------------ |
| 反编译结果已生成（`completed`）         | 核心结果存在且静态检查通过               | 可以继续分析，但不代表与原始源码一致 |
| 结果已生成，部分内容需检查（`partial`） | 已生成可下载结果，但部分内容无法完全确认 | 先阅读检查提示和报告                 |
| 反编译未完成（`failed`）                | 核心步骤无法安全完成                     | 检查 AppID、文件完整性和错误提示     |

结果评分衡量结构完整度和静态可解析性，不表示原始源码复现程度，也不保证运行正确性。

## 支持范围与限制

| 包类型或结构      | 当前支持情况                                                 |
| ----------------- | ------------------------------------------------------------ |
| 标准包            | 支持识别、解包和静态反编译                                   |
| 加密包            | 支持，但必须提供与目标包匹配的 AppID                         |
| 微信 4.x 聚合结构 | 支持常见结构，不承诺覆盖所有客户端版本和编译形态             |
| 独立分包          | 可以识别；缺少主包运行时时会跳过不可靠处理并标记为 `partial` |
| 小游戏包          | 当前仅做分类识别，不承诺完整反编译                           |

反编译无法重新生成编译时已经丢失的注释、原始变量名、源码目录和构建配置，也无法静态确定所有动态生成、运行时注入或强混淆内容。输出工程不保证可直接重新编译或运行；fallback、生成内容和推断结果会保留来源，不会伪装成原始源码。

## 隐私与部署安全

- 项目不接入外部数据库、缓存或消息队列；任务状态、队列和产物只保存在配置的本地目录或 Docker 卷中。
- 首页 Star 数由 API 服务端向固定的 GitHub 仓库接口查询并短时缓存；浏览器不会直接联系 GitHub，也不会向 GitHub 转发访客 IP、任务信息或上传内容。反编译 Worker 仍保持无外网权限。
- 为让独立 Worker 完成解密，AppID 会短暂写入权限为 `0600` 的一次性凭据文件，不进入任务状态、队列或报告；完成一次解密尝试或任务失败后立即删除。若删除前进程意外退出，凭据最迟随任务目录按保留策略清理；若删除后任务中断，加密包需重新提交。原始文件名和文件大小也不会写入任务记录，升级时还会自动清理旧任务状态中的遗留敏感字段。
- 任务目录权限为 `0700`，任务状态、队列文件和产物权限为 `0600`；生产 Worker 默认禁用外部网络。
- 应用请求日志不记录客户端 IP、查询参数或真实任务 ID，生产网关也默认关闭访问日志。极少量严重基础设施错误日志仍应按敏感数据保护。任务 ID 仍应视为临时访问凭证，不要公开分享。
- 解包与打包会检查路径穿越、绝对路径、重复 ZIP 条目和符号链接。
- Node 辅助进程具有执行超时、输出上限和 V8 old-space 限制；严格的内存隔离仍应依赖容器资源限制。
- API 没有内置用户认证和按用户划分的任务权限。公网部署必须在前置网关增加 TLS、身份认证和限流，并隔离 Worker 网络。
- 正常终态会立即删除原始上传、AppID、fallback 工作区和 raw 重复副本；异常中断遗留、任务记录、恢复源码、报告、失败队列记录及 ZIP 按 `RETAIN_ARTIFACTS_HOURS` 周期清理。代码默认值为 24 小时，公开体验站当前为 72 小时。

## 开发与部署

<details>
<summary><strong>本地开发</strong></summary>

推荐使用 Node.js 24 和 Go 1.26；Go 模块声明的最低版本为 1.25。以下命令适用于 Bash：

```bash
npm ci --prefix backend/internal/beautify
npm ci --prefix backend/internal/beautify/wxappUnpacker

mkdir -p /tmp/seewxapkg-tasks /tmp/seewxapkg-output
cd backend
TEMP_DIR=/tmp/seewxapkg-tasks \
OUTPUT_DIR=/tmp/seewxapkg-output \
go run ./cmd/server
```

另开一个终端：

```bash
cd frontend
npm ci
npm run dev
```

前端位于 <http://localhost:5180>，并将 `/api` 代理到后端 `9090` 端口。直接运行服务默认使用 `memory + inmem`，只适合短时开发，重启会丢失任务状态与队列；持久运行请使用 Compose 的 `file + file`。

</details>

[`deploy/production/`](./deploy/production/) 提供了单机生产部署参考，但不包含身份认证。公网使用前必须替换镜像、域名和证书，并在网关接入身份认证。

## API

<details>
<summary><strong>查看 HTTP API</strong></summary>

创建反编译任务：

```bash
curl -fsS -X POST http://localhost:9090/api/compile \
  -F 'file=@/path/to/__APP__.wxapkg' \
  -F 'beautify=true' \
  -F 'decompile=true'
```

加密包不要把真实 AppID 直接写进命令行或 shell 历史；请使用下文的隐私安全验收脚本，它会隐藏输入并通过权限为 `0600` 的临时配置发送。直接调用 API 时，未提供的 `beautify` 和 `decompile` 均为 `false`。

| 方法           | 路径                             | 用途                     |
| -------------- | -------------------------------- | ------------------------ |
| `GET`          | `/api/health`                    | 健康状态、版本和运行能力 |
| `POST`         | `/api/compile`                   | 上传文件并创建任务       |
| `GET`          | `/api/events?taskId=<id>`        | SSE 实时进度             |
| `GET`          | `/api/tasks/:taskId`             | 权威任务状态、阶段和评分 |
| `GET`          | `/api/tasks/:taskId/report`      | 综合或具名技术报告       |
| `GET`          | `/api/tasks/:taskId/diagnostics` | 已脱敏的检查提示         |
| `GET`          | `/api/tasks/:taskId/artifacts`   | 产物清单与来源           |
| `GET` / `HEAD` | `/api/download/:taskId`          | 下载 ZIP 或检查是否就绪  |

`GET /api/tasks/:taskId` 响应中的 `status` 是唯一权威终态。具名报告包括 `package-profile`、各类 `*-recovery-report`、`format-report` 和 `zip-manifest`，实际集合取决于请求选项和任务进度。

</details>

## 配置

<details>
<summary><strong>查看常用环境变量</strong></summary>

下表是直接运行 Go 服务时的默认值；Compose 会按 API 与 Worker 的职责覆盖部分配置。

| 变量                                                  |                       默认值 | 用途                             |
| ----------------------------------------------------- | ---------------------------: | -------------------------------- |
| `SERVER_PORT`                                         |                       `9090` | API 监听端口                     |
| `MAX_UPLOAD_SIZE`                                     |                   `52428800` | 最大上传大小（50 MiB）           |
| `TEMP_DIR` / `OUTPUT_DIR`                             | `/tmp/seewxapkg` / `/output` | 工作目录与 ZIP 目录              |
| `TASK_REPO_DRIVER`                                    |                     `memory` | `memory` 或 `file`               |
| `QUEUE_DRIVER`                                        |                      `inmem` | `inmem` 或 `file`                |
| `BEAUTIFY_ENABLED`                                    |                       `true` | 是否整理代码                     |
| `DEOBFUSCATE_ENABLED`                                 |                      `false` | 是否启用启发式可读性变换         |
| `NATIVE_RECOVER_ENABLED` / `FALLBACK_RECOVER_ENABLED` |              `true` / `true` | 两条反编译路径开关               |
| `VERIFICATION_ENABLED` / `REPORT_ENABLED`             |              `true` / `true` | 结果检查与报告开关               |
| `NODE_EXEC_TIMEOUT_SECONDS` / `NODE_EXEC_MEMORY_MB`   |                 `60` / `512` | Node 超时与 V8 old-space 上限    |
| `MAX_CONCURRENT_TASKS`                                |                          `4` | Worker 并发数                    |
| `RETAIN_ARTIFACTS_HOURS`                              |                         `24` | 文件保留时间；`0` 表示不自动清理 |

完整校验规则见 [`backend/internal/config/config.go`](./backend/internal/config/config.go)。

</details>

## 测试

<details>
<summary><strong>运行质量门禁</strong></summary>

```bash
npm ci --prefix backend/internal/beautify
npm ci --prefix backend/internal/beautify/wxappUnpacker
npm test --prefix backend/internal/beautify

cd backend
go test -race ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...
```

前端在 `frontend/` 下运行 `npm ci && npm test && npm run build && npm audit`。CI 还会构建镜像、执行 Compose 健康检查并验证生产配置；真实 `.wxapkg` 不会进入公开 CI。

授权样本可使用：

```bash
WXAPKG_PATH='/absolute/path/to/__APP__.wxapkg'
./scripts/manual_validate.sh "$WXAPKG_PATH" http://127.0.0.1:9090 completed
```

脚本会安全提示输入 AppID（普通包可留空），默认使用权限为 `0700` 的临时目录并在结束时删除。最后一个参数可改为 `partial`；省略时不限定这两种状态，但质量门禁仍须全部通过，`failed` 始终验收失败。

</details>

## 文档与贡献

- [架构与设计约束](./docs/architecture.md)
- [任务状态机](./docs/task-state-machine.md)
- [测试 Fixture 规范](./docs/fixture-spec.md)
- [反编译报告规范](./docs/recovery-report-spec.md)
- [第三方组件与许可证说明](./THIRD_PARTY_NOTICES.md)

## 参与贡献

Issue 和 Pull Request 都欢迎。较大的功能变化请先提交 [Feature Request](https://github.com/Leslie-SSS/seeWxapkg/issues/new?template=feature_request.md)；Bug 请使用 [Bug Report](https://github.com/Leslie-SSS/seeWxapkg/issues/new?template=bug_report.md)，并提供最小复现和已脱敏日志。不要提交无权分发的真实包；反编译逻辑和第三方代码变更必须补充测试、来源及许可证信息。

## Star 增长趋势

<p align="center">
  <a href="https://github.com/Leslie-SSS/seeWxapkg/stargazers">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="./docs/assets/star-history-dark.svg" />
      <source media="(prefers-color-scheme: light)" srcset="./docs/assets/star-history-light.svg" />
      <img src="./docs/assets/star-history-light.svg" alt="See Wxapkg GitHub Star 增长趋势" width="800" />
    </picture>
  </a>
</p>

趋势图由[仓库自动化](./.github/workflows/update-star-history.yml)通过 GitHub API 更新，只保存按 UTC 日期聚合的数量，不保存 Stargazer 用户名。

## 许可证

项目自主代码以 [MIT License](./LICENSE) 发布。兼容 fallback 位于 `backend/internal/beautify/wxappUnpacker/`，源自 wxappUnpacker，并按 **GPL-3.0-or-later** 发布；其源码、修改和许可证随仓库一同提供。再分发或修改前请阅读 [THIRD_PARTY_NOTICES.md](./THIRD_PARTY_NOTICES.md) 及该目录内的许可证。

## 免责声明

本项目仅用于对已获授权的小程序包进行学习、研究、兼容性分析和故障排查。使用者应自行确认其获取、处理、存储和使用软件包及反编译结果的合法权利，并遵守适用法律、平台规则和第三方许可证。项目按许可证“原样”提供，不对反编译完整性、可编译性、运行结果或特定用途适用性作出保证。

<div align="center">
  <sub>让反编译结果更易读，也让不确定性更诚实。</sub>
</div>
