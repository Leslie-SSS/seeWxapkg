# 贡献指南

感谢您对 SeeWxapkg 的关注！我们欢迎任何形式的贡献。

## 开发环境搭建

### 前置要求
- Go 1.23+
- Node.js 18+
- Docker & Docker Compose
- Git

### 后端开发

```bash
cd backend
go mod download
go run cmd/server/main.go
```

### 前端开发

```bash
cd frontend
npm install
npm run dev
```

## 代码风格

### Go 代码
- 使用 `gofmt` 格式化代码
- 遵循 [Effective Go](https://go.dev/doc/effective_go) 指南
- 使用有意义的变量和函数名
- 添加必要的注释

### React/TypeScript 代码
- 使用函数组件
- 遵循 React Hooks 规则
- 使用 TypeScript 类型注解
- 组件文件使用 PascalCase
- 工具函数文件使用 camelCase

## 提交规范

我们使用 [Conventional Commits](https://www.conventionalcommits.org/) 规范：

- `feat:` 新功能
- `fix:` Bug 修复
- `docs:` 文档更新
- `style:` 代码格式调整（不影响功能）
- `refactor:` 代码重构
- `perf:` 性能优化
- `test:` 测试相关
- `chore:` 构建/工具链相关

示例：
```
feat: 添加批量上传功能
fix: 修复解密时的边界条件问题
docs: 更新 API 文档
```

## PR 流程

1. Fork 本仓库
2. 创建特性分支：`git checkout -b feature/xxx`
3. 提交更改：`git commit -m "feat: xxx"`
4. 推送分支：`git push origin feature/xxx`
5. 创建 Pull Request
6. 等待 Code Review
7. 根据反馈进行修改
8. 合并到主分支

## 代码质量

- 后端：确保 `go build` 成功
- 前端：确保 `npm run build` 成功
- Docker：确保 `docker-compose build` 成功

## 行为准则

请尊重所有贡献者，保持友好的交流。

有问题请随时提 Issue！
