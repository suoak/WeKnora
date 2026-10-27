# 飞书 V2 一键批量导入

本改造继续使用 `feature/knowhub-feishu-access-v2`，不合并 main，不部署生产，不修改 PR #50 的五个受保护文件。

## 用户入口

默认弹窗只展示个人连接状态、多行链接、私人保存位置和“一键导入”。可选文档浏览只展开当前层，勾选文档直接进入输入框；不创建临时来源，不推导整个文件夹或 Wiki 空间的授权。

已连接用户不选择身份。个人 OAuth 返回原页面，原有 sessionStorage 草稿保留。默认目标为真正私有的「我的飞书知识」，由现有知识库服务设置模型和存储默认值，再由仓储事务原子写入私人标记。首次创建以 PostgreSQL advisory lock 按租户与用户串行化；后续复用同一合规私人库。现有普通知识库不改变可见范围。

知识库列表页提供“导入飞书文档”入口；即使当前空间尚无任何知识库，也能打开同一弹窗。OAuth 返回列表页时恢复链接与弹窗，无需先手工创建普通库。

独立 `/platform/feishu/imports` 页面展示本人当前空间的历史，简化状态为排队中、导入中、已完成、需要处理。安全原因、共享审批、持续发现、来源暂停与人工操作仍保留在该页的高级详情。站内通知尚未实现，不报告通知已发出。

## API 与后台

`POST /api/v1/me/feishu/imports` 接收 `links`、可选 `kb_id` 与 `Idempotency-Key`，返回 HTTP 202 和现有 batch 视图，其中 `batch.id` 为持久化任务标识。未指定目标时自动选择上述私人库；指定共享目标仍需逐资源、逐版本的明确共享批准。

该接口不要求 preview token，不执行远程 metadata、下载、解析或向量生成。接收阶段校验请求体边界、当前个人连接引用、目标访问及链接语法，错误链接独立记为 blocked，不影响合法条目。后台才完成身份权限检查与规范化资源解析。Wiki 别名和直链在现有 canonical import 锁下合并，不能把去重当作入库成功。

已有 PostgreSQL batch/item、Asynq 文档任务、attempt、staging、outbox 和原子发布继续承担执行。批次接收、下载与索引均不依赖浏览器存在；过期 claim 可以由替代 Worker 恢复。提交重试复用幂等键，不重复创建正式 Knowledge。

仅已识别的暂时错误后台有限重试，批次最多增加三次重试，退避与重试计数持久化；客户端内部限流及安全预算继续生效。权限拒绝、撤销授权、人工保护和配置/身份变化不会切换为应用身份重试。

已验证索引的明确资源再通过现有 `EnableImportSource` 接管为单资源增量来源。自动接管单独使用 claim/lease，复核原批次连接与配置版本；保持 Knowledge ID、Cursor 和发布保护，不扫描其他资源，不恢复已有 paused/hold/revoked 来源。增量执行仍受部署停止门禁、来源准入和 Redis 预算控制。

## 上游参考与差异

参考 [Tencent/WeKnora PR #3312](https://github.com/Tencent/WeKnora/pull/3312)，核查时仍为 OPEN，head 为 `524f03d4754ec5585fb08c1e74298a3af51b94a9`。已核查该 head 的 `DataSourceEditorDialog.vue`、`core/url.go`、`links/connector.go`，借鉴批量输入、规范化身份以及 FetchStream/Cursor 引擎复用。

保留本分支严格 HTTPS、飞书域名、端口、用户信息和资源路径限制，以及实际下载 SSRF 防护。未采用上游忽略 host 的 URL 解析；未采用点击解析时先持久化 paused DataSource 的步骤。当前个人授权仅支持已配置的飞书区域，没有声称新增 Lark 个人授权。

## 验证记录与限制

- 本地链接归一化与 HTTP 环境幂等键测试：4 项通过。
- 本地前端类型检查通过。
- 本地全量前端：1588 通过、3 失败、1 跳过；失败为 Windows 5173 端口 EACCES、两项缺少 POSIX/Bash，Linux 完整前端另行作为发布门禁。
- 实际 Vue 弹窗 + Chromium、合成 HTTP：10 篇混合 Docx/Sheet/Bitable/Wiki 一次提交、去重、逐条错误提示、无 preview 调用、1280px/390px 布局通过。该证据不等同于真实 OAuth 或真实飞书文档入库。
- `b2501d544` 原生完整回归 `40192242896` 通过，包含真实 PostgreSQL 首次私人库并发创建、批次幂等与重启恢复、暂时故障重试和权限拒绝检查。后续修改必须以对应精确 SHA 的最新五项门禁为准。
- 最终候选的原生、隔离、集成安全、Portal、Python MCP 门禁、镜像、部署与真实账号结果在完成后追加。未执行的真实业务验收不得标为 PASS。

## 隔离站升级

测试入口为 `http://172.30.232.42/`，按现有单客户端 IP 限制访问。HTTP 例外保持测试环境、默认关闭、精确回调和有效期门禁，UI 保留可见的明文传输风险提示。

升级工具只操作现有隔离 Compose 项目：验证候选 SHA 的五项门禁及镜像；停两路 Worker；备份当前测试数据库并在 template0 新库实际恢复，对比用户、连接、私人库、导入和 Knowledge 数量；保留文件、Redis、JWT/AES 和连接数据；替换应用/前端后重新检查健康与两路 Worker。旧应用配置、镜像和原始入口均保留，回滚不清空数据，不重置 Cursor。备份和凭据始终保存在私有目录。

真实验收待用户以唯一现有飞书账号完成连接并选择受控资料。跨用户范围使用合成 KnowHub 用户与真实 PostgreSQL/Redis/Asynq 证明，不能声称已经完成真实跨员工验证。图片/高级解析仍遵循 PR #50 依赖，不干预其开发。

## 最终隔离站交付

实际应用与前端部署源码为 `bf5980b281244b7ce87d61d9af2bd69b411f3b82`。后续证据/工具文档提交不代表替换该已验证应用版本。

该 SHA 的五项完整门禁均为 SUCCESS：

- [V2 独立门禁：前端全量、Chromium、lint、PostgreSQL、race、迁移](https://github.com/suoak/WeKnora/actions/runs/40197678810)
- [原生解析、V2 真实管线与完整应用回归](https://github.com/suoak/WeKnora/actions/runs/40197679651)
- [集成安全、Redis/PostgreSQL、旧版本保护与完整后端回归](https://github.com/suoak/WeKnora/actions/runs/40197682703)
- [Portal、MCP、权限与升级治理](https://github.com/suoak/WeKnora/actions/runs/40197685431)
- [Python MCP 四个版本测试与 wheel 构建，发布跳过](https://github.com/suoak/WeKnora/actions/runs/40197688555)

最终构建与部署指纹：

| 项目 | SHA-256 / 状态 |
| --- | --- |
| LF 源码归档 | `33e28ed68e90e05a20932e26d04cf3d36c862fb6ee6458b23eee0c0b06eba10f` |
| 后端二进制 | `dde607cf3c0fd818a456e51993e559fabb1f79bebd7c0be1c3967a66fe5d2f22` |
| 后端镜像 | `fe07752b1b8617b6e4cfcfa89466bcc4ef1eb6334c4320ce241070387747c0df` |
| 前端镜像 | `1db0a0185dde734766048484dabe29819a747ca13721921aa0fc95b832740b8e` |
| 升级前当前业务库备份 | `7d6dfb3d2ace8268e1d15e789d4775edf631aa8137cd5c7b0dea58f87f5fd3eb` |
| 业务库独立恢复验证 | PASS；用户、连接、私人库、导入及 Knowledge 数量一致 |
| Worker | app / worker2 均 healthy；六类池各 2 个实例 |
| 账号保留 | `test` 仍存在、启用，原飞书连接记录仍存在 |

公开测试入口首页、8080 健康检查及回调页面均返回 200；匿名飞书与个人 MCP 接口返回 401；回调保持 `no-store` 与 `no-referrer`。后端完整提交号和前端显示提交号 `bf5980b` 已实际核对。

部署后合成 HTTP/Asynq 验证通过：私人库中的原有手工 canary 由合法所有者 API 重新入队后完成后台处理，真实测试网关生成 1 行 1024 维向量，BM25 命中且本人混合检索可找到文档。同空间其他管理员对私人库、文档和检索均返回 404，公开正向对照返回 200。真实个人 Go MCP 请求验证本人混合检索，以及其他管理员的目录隔离和直接文档读取拒绝。该合成证明不冒充真实飞书文档导入或真实跨员工飞书 ACL 验证。

补齐本机 Bash 环境后，本地前端额外验证为 1589 PASS / 1 SKIP / 0 FAIL；此运行排除了 Windows 无法绑定 5173 的两项 Vite MCP 代理测试，这两项在最终 Linux 完整前端门禁中覆盖。

用户已提供账号名 `test`，尚未提供允许导入的业务链接。本次未代用户登录、未伪造其 JWT，未把连接记录存在当作真实文档权限通过。最终业务步骤是用户登录测试站，在知识库列表点击“导入飞书文档”，粘贴受控链接并点击“一键导入”；任务提交后可关闭页面，在独立导入历史查看结果。高级图片能力仍受 PR #50 依赖约束，不声称已完成真实图片资产验收。

脱敏证据位于 [quick-import-bf5980b28](./evidence/quick-import-bf5980b28/)。备份、还原数据库、回滚配置和实际凭据均保留在服务器私有目录，不进入仓库。
