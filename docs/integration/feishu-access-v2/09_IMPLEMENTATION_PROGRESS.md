# 飞书 V2 开发进展与验收边界（2026-10-25）

本文件是持续更新的开发记录，不是测试站交付或生产上线批准。

## 已提交的功能

- 双身份独立 capability、身份版本与凭据引用；另一身份的开关变化不再取消无关的 OAuth state 或任务。
- 个人 Drive/Wiki 分页浏览、类型筛选、受限子树选择、多链接预检与持久批次。
- 只属于当前员工的私有知识库，以及绑定源文档版本的明确共享发布批准。
- 批次幂等、逐项状态、显式有限重试和权限协作记录；报告“已处理”不会授权、发布或自动重试。
- 已完成索引的文档可接管为现有安全增量同步来源，保留 Knowledge ID、原索引及既有游标。接管事务重新核验索引回执与 Wiki 完成状态。
- 来源支持暂停、显式同步、重新绑定当前连接与重新检查后恢复；hold/revoked 不会自动恢复。
- 私有标记与新知识库同事务写入。文件读取、Wiki 投影、检索候选及请求输出复核独立保护私有范围。
- Wiki 快捷方式只在用户打开或选择子树时解析，限制深度与循环；分页目标发生变化时要求重新浏览。

新功能默认关闭。没有修改 PR #50 的五个文件，没有修改生产配置、生产数据库或现有业务文件。

## 精确版本的测试证据

| 来源版本 | 检查 | 结果与边界 |
| --- | --- | --- |
| `3494c23017cfc94a1f92e8b71568e9cab4ab70f5` | [独立 V2 CI](https://github.com/suoak/WeKnora/actions/runs/40163479337) | PASS；首轮定向 PostgreSQL/竞态与前端门禁，不能替代后续改动验证 |
| `75b737f046b1003fd9fe9ae68d84e586e1efc2a1` | [独立 V2 CI](https://github.com/suoak/WeKnora/actions/runs/40169067578) | PASS；service 10 条 PASS 事件、race 6 条 PASS 事件，均无 SKIP/FAIL；私有文件、迁移、前端也通过 |
| `e0b76c09ed88dd792cf0fe2a079ba1f23c85fc6c` | [原生引擎 CI](https://github.com/suoak/WeKnora/actions/runs/40168561236) | PASS；源码构建 AnyDoc、cargo audit、解析器测试、带引擎的服务构建；不包含真实个人 OAuth |
| `75b737f046b1003fd9fe9ae68d84e586e1efc2a1` | [原生 PG 门禁首轮](https://github.com/suoak/WeKnora/actions/runs/40169067622) | FAIL；隔离数据库名称不符合已有测试安全夹具的要求。保留拒绝检查，改用夹具规定的临时数据库名称重跑 |
| 后续提交 | 全量/原生/权限回归 | 持续执行中；不得沿用前一个 SHA 的 PASS |

本地 types、资源分页/快捷方式、引用版本与游标、SQLite 迁移检查及前端类型检查有通过记录。Windows 本地无法完成的原生链接测试由 Linux 门禁验证，不标记为本地 PASS。

## HTTP 回调兼容性与临时方案

用户确认：当前隔离测试应用后台已成功新增 `http://172.30.232.42/feishu/callback`，已有重定向地址保留。

官方[重定向配置说明](https://go.feishu.cn/s/5_sDxnxR402)包含 HTTP 示例；官方站点的[IP 回调开发示例](https://open.feishu.cn/community/articles/7317091221654224898)也使用 HTTP IP。当前[授权码接口](https://go.feishu.cn/s/6llKQ-vAI02)要求回调地址在应用列表中。后台保存成功证明配置兼容，尚不证明授权、回跳和 Token 兑换完成。

未登录的真实授权端点探测返回 HTTP 200 登录页，没有执行用户授权、没有生成或兑换授权码，结果为 NOT_CONFIRMED，不能记为业务验收 PASS。

临时实现默认关闭，只允许：

1. `KNOWHUB_ENVIRONMENT=isolated-test`。
2. `FEISHU_TEST_HTTP_CALLBACK_ENABLED=1`。
3. `FEISHU_TEST_HTTP_CALLBACK_APP_ID` 与当前测试应用完全相同。
4. `FEISHU_TEST_HTTP_CALLBACK_URL` 与唯一允许的完整 URL 相同，包括 IP、端口和路径；必须为字面 RFC1918 IPv4，路径固定 `/feishu/callback`，无 userinfo/query/fragment/转义路径。
5. `FEISHU_TEST_HTTP_CALLBACK_EXPIRES_AT` 为有效 RFC3339 时间，尚未到期且距离当前不超过 24 小时。

每次发起授权、回调及用户 Token 操作重新核验。移除开关或到期会拒绝个人身份操作，应用身份不受无关回调开关影响。普通部署仍要求 HTTPS，历史 loopback 开发行为保留。没有启用此例外，没有更改生产的安全策略。

HTTP 无法保护网络传输：授权码、KnowHub 会话、请求和页面内容可能被同网段攻击者窃听或篡改。state、一次性授权码及服务端 Secret 不能消除会话窃取风险。只允许受控测试账号和测试文档；无法保持可信隔离网络时改用 HTTPS 反向代理，后端 8080 可保持 HTTP。测试入口应禁用带 query 的访问日志并发送 `Referrer-Policy: no-referrer`；回调页面已经立即清除地址栏参数。

## 打包服务器与切换授权

- 用户已授权覆盖打包服务器现有 80/8080 服务；这不授权复用或修改现有数据库与业务文件。
- 这两个端口当前属于同一个入口容器。已记录入口镜像 ID、端口与只读配置挂载路径，尚未停止或替换它。
- 历史构建目录所在分区剩余约 1.4GB，Docker 所在 `/mnt/sata0` 约 1.2TB 可用。后续使用该挂载点的新目录。
- 已核对已有构建镜像 ID 和原生库摘要。旧库源码指纹未与当前源确认一致，不将旧库直接充当当前版本的构建产物。
- 部署前仍需独立 Compose 项目、PostgreSQL/ParadeDB、Redis/Asynq、卷、网络与索引，验证迁移、备份恢复、应用启动和服务回滚后才能切换入口。

## 尚未完成

- 文件夹/Wiki 当前支持受限选择和首次批次，其后逐文档可启用受控更新；目录新增文档的持续发现还未完成，不能称为完整目录持续同步。
- 尚需真实索引接管后的变更/不变/失败保留旧版本端到端测试，以及完整 Portal、Go MCP、Python MCP 与多 Worker 回归。
- PR #50 仍 open，解析质量扩展等待其最终集成条件，不能修改其正在处理的文件。
- 测试站尚未部署，单个真实飞书账号尚未完成 OAuth、Docx/Sheet/Bitable/Wiki/Drive、修改更新、网页和 MCP 验收。
- 真实跨员工场景按用户调整记为 NOT_TESTED，使用隔离 KnowHub 用户、模拟 OpenID 与真实数据库补充安全测试。

任务继续推进至测试站交付；不得自动合并 main 或部署生产。
