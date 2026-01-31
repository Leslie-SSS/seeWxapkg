# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- 初始版本发布
- .wxapkg 文件解密支持（PBKDF2 + AES + XOR）
- .wxapkg 文件解包功能
- 并发文件提取
- 代码美化（JSON、JS、HTML）
- SSE 实时进度推送
- Docker 一键部署
- 科技感 Web UI

### Security
- 文件上传大小限制（100MB）
- 路径遍历攻击防护
- AppID 格式验证
- CORS 安全配置
- CSP 安全头
