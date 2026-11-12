# 新增来源再次进入 hold 的触发条件与修复

前一次生产操作只解除当时 4 个现存来源的 hold，未修改自动触发机制；不能据此保证以后新增/编辑不会再次触发保护。

## 已确认的代码链路

- `internal/handler/feishu_sync_admission.go`：仅在 `sync_source_protected` 且 `ProtectionState=hold` 时返回“数据源已保存，来源处于保护状态，请管理员核验恢复后同步”。全局平台停止使用不同的 503 提示。
- `internal/application/service/feishu_sync_admission.go`：同步前发现来源 hold 且没有 reconciliation 能力时产生上述错误。
- `internal/application/repository/datasource_repo.go`：飞书来源的配置、类型发生变化，会将来源置为 hold；同一空间内变更知识库也会被拒绝。此前仅豁免 unmanaged、paused、从未执行过的草稿。
- 前端资源发现先保存 paused 草稿，最终保存会启用来源。直接创建为 active，或最终保存后继续完成首次配置时，旧的 paused 限制会把未使用来源也当作历史来源。
- 已核对生产 d320291 与当前分支在该仓储和数据源服务路径没有差异。这里确认的是可触发的代码条件，尚未取得某次具体新增操作的请求序列，不声称已还原用户特定新增事件。

## 最小修复

允许 active 或 paused、访问状态 unmanaged 或 active 的来源完成首次配置，但必须同时满足：没有 LastSyncAt、游标、同步结果；没有任何同步记录（包括失败/运行记录）；没有来源资源或知识引用（包括已删除知识）。所有判断在现有来源行锁内完成，与同步准入沿用同一加锁顺序。

这样，启用状态本身不再被当成历史发布证据，多次保存首次选择不会误置 hold。已有执行、历史发布、导入游标、明确 hold/revoked 或状态不确定的来源仍使用原校验；配置变更仍会影响这些来源。该修复不自动解除已有 hold，不发放同步执行许可，也不修复生产同步平台全局停止故障。

## 验证与发布状态

扩展服务回归覆盖首次草稿、已启用新来源、多次首次保存、已有访问 active、失败同步、资源、已删除文档、历史时间戳、导入游标/结果、error 状态及 hold/revoked。新增仓储 SQLite/PostgreSQL 用例，验证真实持久化、多次配置保存、在途执行和历史资源的边界。

隔离测试环境中全部通过（`-race -count=1`）：仓储首次配置 SQLite/真实 PostgreSQL、仓储 CRUD、同步锁顺序及跨连接准入、服务层 12 种首次配置/历史状态、加密配置与计划修改回归。测试进程 exit code 0，无生产连接。PostgreSQL 使用临时测试 schema，测试后清理；服务层使用正式迁移的 SQLite schema 3046。测试结果及精确源文件 SHA 保存在本地 `E:/AIData/Codex/enterprise-mcp-20261110/source-setup-test-result.json`。

尚未部署生产。不能将本修复描述为“新增一定能同步”或“所有未来来源都不再进入保护”；生产同步故障和真实授权变化是独立事项。
