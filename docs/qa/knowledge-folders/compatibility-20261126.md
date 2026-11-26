# 目录功能：已验证兼容项

本结论与[飞书停止独立诊断](sync-incident-20261126.md)分开。目录验收不要求新增飞书来源或管理员凭据，不使用生产 Canary 数据。

## 交付收尾

- 功能 PR #272 已合入；真实测试站点运行 `642bbe7f1`，APP、Worker2、Frontend 同版，Schema 3052、clean。
- 文档 [PR #273](https://github.com/suoak/WeKnora/pull/273) 已于北京时间 2026-11-26 22:05:10 合入，提交 `f3573b6f52ca11cdb6eb41cd70623faaf630db3f`。5 项 CI 成功、3 项按条件跳过，无失败；[最终 CI 回执](production-prep-evidence/pr273-final-ci.json)。
- `642bbe7f1..f3573b6f5` 的 `internal/`、`frontend/`、`migrations/` 差异为空，因此文档合入没有改变已验收的功能代码。[来源审查](production-prep-evidence/source-review.json)。
- 部署、数据、35 项真实鉴权 API、页面及选择器截图、备份恢复均见[完整验收记录](acceptance-20261126.md)。该记录中的 642bbe7f1 是实际测试运行版本，不应被改写为尚未部署的新 main。

## 已验证兼容项

| 项目 | 已验证内容 | 范围 / 限制 |
| --- | --- | --- |
| 空目录持久化 | 三级空目录真实创建、刷新、重命名、移动、合并、叶子删除；0 文档 KB 可创建；`moved_count=0` 成功 | 真实 PostgreSQL、页面、API 均有证据 |
| 选择器 | 新建目录后实际移动 QA 文档；移回后空目录仍保留 | 真实页面，QA 自有合成数据 |
| 权限 | 未登录 401、跨空间 403；retrieve-only Key 读成功后，三类目录写请求均 403 | 未新增 MCP 工具或 Portal 写入口；原权限配置不变 |
| 既有数据 | 6 份原业务文档及全部分块、向量、来源、模型、原用户、KB、飞书导入记录原字段摘要一致 | QA 文档的移动及时间变化单独核对，不混入原业务数据证明 |
| 检索 | 升级前后关键词、向量检索均命中同一 QA 基线文档 | 没有运行新的真实飞书同步轮次 |
| 飞书目录移动 | `TestKnowledgeDerivedWritePostgres/folder-and-deletion-fences-survive` 将受管文档置 HOLD，读前拒绝；创建持久目录、移动、重命名成功；非空删除拒绝；移动后仍拒绝正文读取和无授权正文写入 | 真实 PostgreSQL，正式迁移链及 003048/003051 约束；[已通过 CI](https://github.com/suoak/WeKnora/actions/runs/45820742860) |
| 历史上传 | 保留 `knowledges.folder_path`；未引入 `existing_folder_anchor`、source CAS、替换/重解析协议；普通后台 Save 不覆盖用户移动后的位置 | 现有上传、替换、目录及受管写入回归通过 |
| 迁移保全 | 正式站点 3042→3052 副本与停写备份恢复已验证；新增目录表 5 行独立恢复一致 | 这是测试站点证明，不代替生产切换前的新备份 |
| 生产实际路径 | 生产只读确认 3051、clean，无 `knowledge_folders`，原 `folder_path` 为 varchar(1024)；合成 3051 全库备份恢复、3052 升级及目录独立恢复通过 | [演练回执](production-prep-evidence/synthetic-3051-to-3052-rehearsal.json)；未复制生产数据、Canary 或任何来源记录，未启动 Worker |

## 尚未验证的在线同步项

以下没有运行，也没有记为成功：

1. 测试站点既有受权飞书来源在当前停止状态下，重新读取真实正文、增量更新、发布和游标推进的完整一轮。
2. 新生产候选替换 Worker 后，现有生产来源的下一轮自然同步、真实远端权限复核、租约续期及发布。
3. 生产候选在原隔离 MCP HTTPS/TLS/白名单挂载下的上线后访问拒绝与回归。

第一项属于原测试站点故障；不把它直接归因于目录功能，也不再索要新的测试来源或管理员凭据。第二、三项保留为[限定发布方案](production-readiness-20261126.md)的观察项与回滚条件，须由用户决定是否接受剩余风险。
