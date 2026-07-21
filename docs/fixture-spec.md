# Fixture Spec

`backend/tests/fixtures/` 保存 golden 期望结果，不直接提交大体积二进制样本。

设计原则：

- `expected/profile.json`: 断言包画像
- `expected/manifest.json`: 断言规范化 manifest 的关键字段
- `expected/assertions.json`: 断言最终状态或额外检查

当前样本：

- `standard-unencrypted`
- `wechat4x-main`
- `page-frame-html`
- `broken`

输入 `wxapkg` 在测试中动态生成，原因：

- 保持仓库体积可控
- 保证 CI 中样本可重复生成
- 便于直接在 Go 测试里构造边界包和损坏包
