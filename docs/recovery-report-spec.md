# Recovery Report Spec

报告通过任务报告接口提供，不再写入只包含 `src/` 的下载 ZIP。服务端任务记录中的 `reports/` 包含：

- `recovery-report.json`: 任务总报告
- `manifest-recovery-report.json`: manifest 来源追踪与恢复详情
- `js-recovery-report.json`: JS 原生恢复详情
- `wxml-recovery-report.json`: WXML 原生恢复详情
- `wxss-recovery-report.json`: WXSS 原生恢复详情
- `diagnostics.json`: 所有阶段的诊断信息
- `package-profile.json`: 包画像
- `artifacts.json`: 产物清单与来源
- `zip-manifest.json`: ZIP 内 `src/` 文件清单，可通过命名报告接口获取
- `format-report.json`: 请求格式化时的逐文件状态、格式化器、输入/输出哈希与耗时

关键字段：

- `status`: `completed | partial | failed`
- `profile`: 包类型画像
- `stages`: 阶段结果数组
- `score`: manifest/js/wxml/wxss/overall 百分比
- `diagnostics`: 诊断列表
- `artifacts`: 下载、报告、产物清单入口

来源标记：

- `native`
- `native-generated`
- `fallback`
- `manifest`
