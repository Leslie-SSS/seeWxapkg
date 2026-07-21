# SeeWxapkg Architecture

当前系统按 7 层组织：

1. Ingress: HTTP 上传、参数校验、任务创建
2. Classifier: 包类型识别与 4.x/分包特征判断
3. Normalizer: 解密、解包、统一转为 `NormalizedPackage`
4. Recovery: manifest/js/wxml/wxss 原生恢复，必要时 fallback
5. Safe Formatter: 在恢复结果上执行语义安全格式化，逐文件报告；启发式反混淆默认关闭
6. Verifier: manifest 闭环校验、页面成套率、方言感知的静态可解析率
7. Delivery: 报告生成、ZIP 打包、任务详情查询

关键设计约束：

- 解包成功不等于反编译成功
- 任务状态必须按阶段显式建模
- `app.json` 恢复必须来自 `ManifestIR`
- 深度恢复走 `native -> fallback -> safe-format -> verify`
- fallback 不执行待恢复包代码；动态内容无法静态确认时必须降级为 `partial`
- ZIP 只输出 `src/`；`reports/` 保留在服务端任务目录供在线查询，fallback 输入与工作副本在合并后立即删除
- 旧版本保留期内的可下载归档通过 `repack-src-only` 一次性迁移；迁移同时更新 ZIP、zip-manifest、恢复报告和任务归档大小，避免新旧页面文案与实际内容不一致

主要目录：

- `backend/internal/api/http`: HTTP 入口与 DTO
- `backend/internal/app`: 编排服务与任务查询
- `backend/internal/domain`: 任务与包的领域模型
- `backend/internal/pipeline`: classifier/decrypt/normalize/recover/verify
- `backend/internal/infra`: 仓储、事件、队列、进程、存储
- `backend/internal/report`: 汇总报告与 ZIP manifest
- `backend/tests`: unit/integration/golden/testutil/fixtures
