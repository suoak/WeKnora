# 验证与回滚

> 本文件是 PR-A 审计时的验证/回滚快照，不是当前运行时实现清单。当前精确 SHA、Linux 门禁、HTTP 临时回调方案及尚未完成的验收见 [09_IMPLEMENTATION_PROGRESS.md](09_IMPLEMENTATION_PROGRESS.md)。真实验收已按用户要求调整为单个飞书账号；不把真实跨员工场景标为 PASS。

本轮仅新增身份契约和离线默认的开发探针，不接入产品请求、worker、数据库迁移或 UI。测试记录详见 evidence/validation.txt；真实账号、生产运行版本复核和 Linux 集成不能从本地单测推导。

用户提供应用凭据后，真实获取应用 Token、读取应用根目录元数据和 Wiki 首批空间列表已通过。详见 03 与 evidence/live-application-probe.json。未执行具体文档正文、导出、员工 OAuth 或业务导入。

## 必需门禁

| 测试 | 本轮覆盖 / 后续要求 |
| --- | --- |
| 身份契约 | 默认关闭、双独立开关所有组合、缺失/未知模式拒绝、不 fallback、跨用户 owner 拒绝、user/app grant 不混用、版本拒绝、私有字段不序列化 |
| 探针安全 | 默认不联网/缺凭据 BLOCKED、GET-only、固定域名、拒绝 redirect、15秒超时、20项/1MiB限制、body/Token/open_id不入报告、供应商业务错误、预期ACL错误码、身份不匹配停止 |
| 旧应用模式 | 原配置/旧 payload 完整兼容；全链 first-create、allowlist、engine、版本栅栏；本轮未改运行时 |
| 真实 OAuth/Token | A/B/C 独立授权、刷新轮换、撤权/断连与 callback/worker 竞争、多 worker 同时刷新、错误 owner/app/open_id 拒绝；BLOCKED |
| 权限 | 文档有权/无权/失权、KB 写权限、私人 KB、共享文档发布批准、Portal/Go/Python MCP 同一可见范围；BLOCKED |
| 批次 | Wiki+Docx 同对象、跨用户重复、重启/outbox交接、多worker claim/lease、限流与有界重试、部分失败不成功；未实现 |
| 内容/索引 | Docx/Sheet/Bitable 内容与行列真值；父子 Chunk/Embedding/BM25/Wiki 一致，原 Knowledge ID/index 不重建；未执行 |
| 安全回归 | 受保护/paused/hold/revoked 不恢复；generation/claim/lease、AssetsError、图片资产完整性、旧版本保护；V2 发布前须运行既有全套门禁 |
| Linux 集成 | 真 PostgreSQL/ParadeDB、Redis、Asynq、多 worker、迁移 up/down 和重启恢复；本轮无专用栈，BLOCKED |

本地 SQLite/Mock 测试仅证明本地单元边界；不能替代上述集成验收。仓库已有 syncdb 测试通过 SYNC_EXECUTION_POSTGRES_DSN 指定专用 schema，缺 DSN 可能 skip；集成 gate 必须设置 REQUIRE_SYNC_EXECUTION_POSTGRES=1 并审查 skip，不能把绿色进程退出码当 PostgreSQL 执行证据。不使用 DATABASE_URL 或生产连接作为 fallback。

## 复现

```powershell
go test ./internal/types -run 'TestFeishu(Access|Credential)' -count=1
python -m unittest discover -s docs/integration/feishu-access-v2/poc -v
python docs/integration/feishu-access-v2/poc/readonly_probe.py docs/integration/feishu-access-v2/poc/manifest.example.json --output docs/integration/feishu-access-v2/poc/preflight-result.json
```

探针预检没有 --execute，因此预期退出码2、status BLOCKED，不是测试失败，也不是实际 API PASS。真实执行须使用仓库外的批准 fixture 和凭据；三类身份分别留脱敏证据。

## 回滚

本轮代码未注册到任何运行路径、未生成新数据库结构或业务任务，回滚只需移除本轮新增身份契约/开发资产；无需修改生产配置/数据库/队列/索引。原脏工作树没有清理或 stash。

未来 V2：先关闭新 admission 与 scheduler，保留已领取任务及其 fencing/outbox 状态；不能清队列/游标强行恢复。旧 reader 只消费其认识的 payload；新增表列保留以便审计，确认无活动引用前不做 destructive down。批准失效/Token撤权即拒绝后续读取或发布，失败保留原知识和 ID，按既有安全版本协议回退；不调用旧 Replace 或重建正式索引。

放行要求：A/B/C 真账号证据、隔离 Linux 集成、KB/Portal/MCP权限一致、#50最终安全回归、独立 review 与明确发布授权全部满足。当前结果不允许发布 V2。
