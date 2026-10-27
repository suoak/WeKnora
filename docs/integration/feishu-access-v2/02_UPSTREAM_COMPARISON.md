# 腾讯上游差异及移植结论

基线与 PR 实时状态见 [remote-baseline.json](evidence/remote-baseline.json)。对比使用本地 git 对象，不把 PR 描述当作已合并源码。以下“可移植”均不表示本轮已经移植。

| 能力 | 上游证据 | 本地差异与结论 |
| --- | --- | --- |
| 数据源 settings.parse_mode | main core/shared.go ParseFeishuConfig、DocxFetchInput.ParseMode、parse_mode_test.go；`973d9d051` #3497 | **需适配**。上游按源读取 blocks/export，环境变量仅兼容；本地 core 仍有既有环境配置和额外安全策略。必须保留源级选择与旧配置行为，PR-G 才处理 |
| Docx Blocks / Export | main core/blocks.go、markdown.go、shared.go FetchDocxWithBlocks | **需适配**。上游图片/画板 marker 转内联图，附件子项通过 metadata 关联；本地 application candidate、AssetsError、资源校验与版本发布不能被替代 |
| 原生表格嵌套文本 [#4400](https://github.com/Tencent/WeKnora/pull/4400) | 已合并 `b373374bf`；markdown.go + markdown_cell_nested_test.go | **算法和样本可移植，落地需适配**。嵌套 cell 保真不等于图片/附件完备；等待 #50 最终版本后补完整性和索引验收 |
| Wiki 快捷方式子文档 [#4565](https://github.com/Tencent/WeKnora/pull/4565) | 已合并 `f0e93abaf`；core/wiki_shortcuts.go，wiki/connector.go | **需适配**。上游保留 partial failures，并按 space/node 去重展开；本地还须保留 selection 边界、目录层级、deferral、generation 和 protected source 约束 |
| 多链接源 [#3312](https://github.com/Tencent/WeKnora/pull/3312) | **open 未合并**；head `524f03d4`，已 fetch 到 upstream/pr-3312；links/connector.go、core/url.go | **借鉴设计、需适配**。复用 core 流式 engine、Wiki 解包及 partial failure；仍用 ParseFeishuConfig 应用凭据，不提供个人 OAuth。URL 的真实对象模型本地 personal_resource.go 已有，不能重复写一套 |
| Sheet/Bitable 关联发现 [#4547](https://github.com/Tencent/WeKnora/pull/4547) | **open 未合并**；head `ae144680`，upstream/pr-4547；core/recursive*.go，drive/wiki recursive.go，datasource_recursive.go | **暂不直接采用**。默认关闭，深度默认 3、最大 10；会扩大源读取范围并跨越目录/权限边界。本地每个新目标须重新 ACL、审批、去重及限额；不能随正文导入自动追链 |
| Token 日志 [#4399](https://github.com/Tencent/WeKnora/pull/4399) | 已合并 `2c0d83843`；client.go、token_log_test.go | **回归测试可移植，生产日志修复无需重复**。本地已只打印 acquired/expire，无 Token 片段；今后覆盖用户 Token、导出下载和重试日志 |

不建议整文件覆盖上游 client/shared/engine，也不建议直接 cherry-pick 数据库迁移。`git diff ce699bb5 upstream/main -- internal/datasource/connector/feishu` 涉及 70 个文件，包含本地 application/user identity、policy、deferrals、限额、下载保护等上游没有的文件。大范围覆盖会删除本地安全能力。

PR-G 执行前重新 fetch main、读取 #50 最终 merge SHA 和实际改动，检查 legacy_staging、anydoc_reader、candidate tests、VLM 与 core 图片/附件引用链；保留 AssetsError 拒绝、旧版本保护及 index/Knowledge ID。不能恢复旧 Replace 逻辑，也不能借解析升级重建已有索引。
