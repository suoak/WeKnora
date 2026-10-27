# 资源浏览与批量 API 设计

状态：设计，尚未注册路由或创建表。第一期只做首次批量导入，个人增量另立任务。

## API 契约

沿用 /api/v1、JWT user 和现有 KB editor guard；不声明 API key 权限即保持原拒绝策略。后台仍重建实时调用者/成员/KB grant。

| API | 入参 / 返回 | 权限与边界 |
| --- | --- | --- |
| GET /me/feishu/connections | 本人的 opaque id、kind、status、可用能力、过期提示 | 不返回 Token/open_id；应用引用仅按当前用户可用能力展示，不暴露其他用户连接 |
| GET /me/feishu/resources | connection_id、kind=drive/wiki、parent_id、page_token、page_size≤50 | 一次一层一页；先取本人 root token；凭据绑定当前用户；不默认递归；provider pagination 用签名 opaque cursor 绑定 owner/connection/parent/expiry |
| POST /feishu/resources/preview | connection_id、auth_mode、最多100个 links、目标 kb_id | 验证 KB write 与个人可见性/批准门禁；Wiki 解包成真实对象后去重；返回逐项 identity/permission/status，不返回正文 |
| POST /knowledge-bases/:id/feishu/batches | connection_id、auth_mode、preview_id、Idempotency-Key | 最多100项，body≤256KiB，预检版本与 owner/KB/connection 绑定；真正执行前再 ACL 验证 |
| GET /feishu/batches/:id | summary、逐项分页结果、page_token | owner + 实时目标 KB 权限；不可凭猜测 UUID 越权；默认50项 |
| POST /feishu/batches/:id/retry | 最多100个失败 item id、Idempotency-Key | 仅本人和当前 KB write；仅可恢复类别，最多3次；先重检授权、版本和来源状态 |

越权返回现有 access 错误语义；任务超限 413/422，幂等 key 同 payload 返回原批次、异 payload 返回409，过期 preview 返回409并要求重新预检。失效连接 409 reconnect_required；403 不能自动切换身份。

批次 admission 只提交耐久元数据及调度 outbox，不在 HTTP 请求里无界循环导入。Preview 限时30秒、并发≤2、总请求≤200，达到上限返回 partial/truncated 和续检游标；只有已完成预检的明确子集才能确认导入，不能把 truncated 当全量成功。资源名称/URL 也受敏感信息可见性限制。

## 表与去重

`feishu_import_batches`：id、owner_id、caller_tenant_id、kb_id、credential reference、idempotency_key、payload_hash、status、计数、created/updated、版本。唯一 `(owner_id,kb_id,idempotency_key)`。

`feishu_import_batch_items`：id、batch_id、输入 ordinal/标准化 URL、真实 region/app/type/token、permission 分类、existing import_id/attempt_id、status、retry_count、last_error_category、next_retry_at。唯一 `(batch_id,region,app_id,real_type,real_token)`；未解包项先按标准化 URL 哈希去重，成功解包后事务合并别名。

批次级 canonical key 必须包含 region/app 与真实 type/token；同一 Wiki 和 Docx URL 只能指向同一 item。跨批次首次创建继续调用现有 repository 的 canonical 资源 admission，保留现有 import/Knowledge ID；不单靠批次 unique index 解决跨用户或跨来源重复。已由其他用户导入的资源按现有规则返回 existing/conflict，不接管其凭据。失败/不可见条目不泄露其他用户来源信息。

## 调度与恢复

batch accepted → running → succeeded / partial_failed / failed；item pending_precheck → permission_blocked / ready → dispatched → indexing → succeeded / failed / review_required。accepted/dispatched/index_pending 均不能计为入库成功。

复用已有 Asynq、安全队列、dispatch/outbox、PersonalRecovery.prepare 与 first-create。批次任务只负责有界领取 item、再次预检和提交既有 attempt；每次领取上限20、并发2，后续从 durable DB 游标继续，worker 失败不靠内存 Set 恢复。事务锁或 SKIP LOCKED + claim/lease fencing 防止多 worker 重复领取；外部 API 不放数据库事务中。lease 到期只使调度条目可重新领取，不能跳过原 attempt 的 generation/claim 校验。

同 item 重启时先查已持久化 import/attempt/outbox，再决定继续观察还是重新 prepare；不得因超时重复生成知识。批次计数从 item 权威状态计算或在受控事务中更新。429/5xx/网络短暂失败有退避，最多3次；权限/scope/禁止下载/人工保护不自动重试，重新授权后必须显式再次预检。部分失败始终保留并呈现，不标记整批成功。

## Wiki / Drive / Links 统一来源

目录发现复用既有 connector/engine，保留源类型、选中根、父子目录、真实对象及原始别名。Wiki shortcut 依据 space/node 与真实对象双重 visited，避免循环和重复；Drive shortcut 先确认选中目录成员关系，再读取目标。限制深度≤10、每轮发现≤1000、页数/时间/请求总量均有限；达到上限标记 partial 并 checkpoint，不静默丢失。

资源 canonical key 用于内容去重，source+root+parent path 用于显示，不能为去重丢掉合法目录位置。目录可列出不意味着内容可读；两级预检分别统计 discovered、permission_failed、imported、unsupported、deferred 和 truncated。不会为了恢复新增来源而清空旧 cursor，或自动唤醒 paused/hold/revoked。多链接持续同步及个人增量分别在后续 PR-E 和独立增量任务评审后接入原调度。
