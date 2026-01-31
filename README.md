# <img src="https://img.shields.io/badge/SeeWxapkg-1.0.0-brightgreen" alt="Version"> <img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License"> <img src="https://img.shields.io/badge/docker-available-blue.svg" alt="Docker"> <img src="https://img.shields.io/badge/Go-1.23+-00ADD8.svg" alt="Go"> <img src="img.shields.io/badge/React-18+-61DAFB.svg" alt="React">

# See Wxapkg

<div align="center">

极简实用的微信小程序反编译 Web 工具

[功能特性](#-功能特性) • [快速开始](#-快速开始) • [部署](#-部署) • [API](#-api-文档) • [贡献](#贡献指南)

[在线演示](#) • [文档](#) • [更新日志](CHANGELOG.md)

</div>

---

## 项目简介

**See Wxapkg** 是一个极简实用的微信小程序反编译工具，理念是"极简实用，用完即走"。

无需安装任何客户端软件，通过浏览器即可完成 `.wxapkg` 文件的解密、解包和美化操作。

## 功能特性

- 🚀 **拖放上传** - 简单的文件拖放上传体验
- 🔐 **解密支持** - 支持 AppID 解密加密的 wxapkg 文件
- 📦 **并发解包** - 高效的并发文件提取
- 🎨 **代码美化** - 自动美化 JSON、JS、HTML 代码
- 📊 **实时进度** - SSE 实时进度推送
- 💾 **一键下载** - 打包为 ZIP 文件一键下载
- 💻 **科技感 UI** - 现代化的深色主题界面
- 🐳 **Docker 部署** - 一键部署，开箱即用

## 快速开始

### 使用 Docker Compose（推荐）

```bash
# 克隆仓库
git clone https://github.com/keepbuild/seewxapkg.git
cd seewxapkg

# 启动服务
docker-compose up -d

# 访问服务
open http://localhost:3004
```

### 本地开发

**后端：**
```bash
cd backend
go mod download
go run cmd/server/main.go
```

**前端：**
```bash
cd frontend
npm install
npm run dev
```

### Docker 单独构建

```bash
# 构建后端
cd backend && docker build -t seewxapkg-backend .

# 构建前端
cd frontend && docker build -t seewxapkg-frontend .
```

## 技术架构

```
┌─────────────────────────────────────────────────┐
│                    Nginx (80)                     │
│  ┌────────────────────────────────────────────┐  │
│  │  ┌────────┐  ┌─────────┐  ┌──────────┐ │  │
│  │  │ Frontend│  │  Nginx  │  │  Backend  │ │  │
│  │  │   :80   │──→│  :8080  │←─│   :8080   │ │  │
│  │  └────────┘  └─────────┘  └──────────┘ │  │
│  └────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────┘
```

- **前端**: React 18 + Vite + Tailwind CSS
- **后端**: Go 1.23 + Gin
- **部署**: Docker Compose
- **反向代理**: Nginx

## 部署

### Docker Compose 部署

```yaml
# docker-compose.yml
services:
  backend:    # Go API 服务
  frontend:   # React 静态文件
  nginx:      # 反向代理
```

### 环境变量

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `SERVER_HOST` | `0.0.0.0` | 服务监听地址 |
| `SERVER_PORT` | `8080` | 服务监听端口 |
| `MAX_UPLOAD_SIZE` | `52428800` | 最大上传大小(字节) |
| `TEMP_DIR` | `/tmp/seewxapkg` | 临时文件目录 |
| `OUTPUT_DIR` | `/output` | 输出文件目录 |

## API 文档

### POST /api/compile

上传并反编译 wxapkg 文件

**请求** (multipart/form-data):
| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| file | File | 是 | .wxapkg 文件 |
| appId | string | 否 | 小程序 AppID，用于解密 |
| beautify | boolean | 否 | 是否美化代码，默认 true |

**响应**:
```json
{
  "success": true,
  "taskId": "uuid",
  "message": "Task created"
}
```

### GET /api/events?taskId=xxx

SSE 进度推送事件

**事件类型**:
- `progress` - 进度更新
- `complete` - 处理完成
- `error` - 处理失败

### GET /api/download/:taskId

下载反编译结果 ZIP 文件

### GET /api/health

健康检查端点

## 使用说明

1. **导出 .wxapkg 文件**
   - macOS: `~/Library/Containers/com.tencent.xinWeChat/Data/Documents/app_data/radium/Applet/packages`
   - Windows: `C:\Users\{用户名}\Documents\WeChat Files\Applet\{AppID}\`

2. **上传文件** - 拖放到上传区域

3. **填写 AppID**（如需解密）

4. **等待处理** - 实时查看进度

5. **下载结果** - 点击下载按钮获取 ZIP 包

## 贡献指南

欢迎提交 Issue 和 Pull Request！

请查看 [CONTRIBUTING.md](CONTRIBUTING.md) 了解详情。

## 许可证

本项目采用 [MIT](LICENSE) 许可证开源。

## 免责声明

本项目仅用于学习和研究目的。请遵守相关法律法规，不得用于非法用途。

## 致谢

- 感始 wxapkg 解密算法参考了 [wux1an/wxapkg](https://github.com/wux1an/wxapkg) 项目
- UI 设计灵感来源于现代开发者工具

---

<div align="center">

**[⭐ Star](../../stargazers)** if this helps you!

Made with ❤️ by [KeepBuild](https://github.com/keepbuild)
