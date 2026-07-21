# Task State Machine

主状态：

- `queued`
- `classifying`
- `decrypting`
- `unpacking`
- `normalizing`
- `recovering_manifest`
- `recovering_js`
- `recovering_wxml`
- `recovering_wxss`
- `fallback_recovering`
- `formatting`（请求安全格式化时）
- `verifying`
- `packaging`
- `completed`
- `partial`
- `failed`

收敛规则：

- `completed`: manifest 校验通过；如果请求深度恢复，则 JS/WXML 核心产物存在且 artifact verify 通过；WXSS 可选
- `partial`: 基础解包成功，但深度恢复仍有缺口，或阶段依赖 fallback/占位生成
- `failed`: 核心阶段失败，或 manifest 闭环未达标

阶段报告：

- 每个阶段写入 `StageResult`
- 阶段包含 `success/partial/status/message/metrics/diagnostics`
- 前端 SSE 与 `/api/tasks/:id` 都基于同一任务模型
