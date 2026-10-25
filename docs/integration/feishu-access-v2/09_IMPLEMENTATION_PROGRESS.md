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
- 目录持续发现已加入实现：明确选择 Drive 文件夹或 Wiki 子树，每次领取一页，完成一轮后间隔 15 分钟重新观察；页位置、目录路径与导入批次同事务保存。个人目录只允许目标为本人私人 KB。暂停发现不会隐式暂停/恢复已接纳的批次或逐篇更新来源，界面对此明确说明。
- 共享发布批准支持本人查看与撤回，撤回阻止后续接纳/发布；已发布知识仍需通过既有权限和下架操作管理。

新功能默认关闭。没有修改 PR #50 的五个文件，没有修改生产配置、生产数据库或现有业务文件。

## 精确版本的测试证据

| 来源版本 | 检查 | 结果与边界 |
| --- | --- | --- |
| `3494c23017cfc94a1f92e8b71568e9cab4ab70f5` | [独立 V2 CI](https://github.com/suoak/WeKnora/actions/runs/40163479337) | PASS；首轮定向 PostgreSQL/竞态与前端门禁，不能替代后续改动验证 |
| `75b737f046b1003fd9fe9ae68d84e586e1efc2a1` | [独立 V2 CI](https://github.com/suoak/WeKnora/actions/runs/40169067578) | PASS；service 10 条 PASS 事件、race 6 条 PASS 事件，均无 SKIP/FAIL；私有文件、迁移、前端也通过 |
| `e0b76c09ed88dd792cf0fe2a079ba1f23c85fc6c` | [原生引擎 CI](https://github.com/suoak/WeKnora/actions/runs/40168561236) | PASS；源码构建 AnyDoc、cargo audit、解析器测试、带引擎的服务构建；不包含真实个人 OAuth |
| `75b737f046b1003fd9fe9ae68d84e586e1efc2a1` | [原生 PG 门禁首轮](https://github.com/suoak/WeKnora/actions/runs/40169067622) | FAIL；隔离数据库名称不符合已有测试安全夹具的要求。保留拒绝检查，改用夹具规定的临时数据库名称重跑 |
| `532a3a047e925bc698614603db4ca6e53404cb4b` | [独立 V2 门禁](https://github.com/suoak/WeKnora/actions/runs/40172482785) | PASS；lint、真实 PostgreSQL、竞态、迁移、前端完整测试/类型/构建均通过；后续修改须独立验证 |
| `5a49b4e9054f607e823ced0119751ed2c8147d51` | [Portal 全套门禁](https://github.com/suoak/WeKnora/actions/runs/40172043924) | PASS；Portal contracts/RBAC/race、迁移新建与升级、完整 internal 后端与 MCP 兼容性 job；不代表全部可选外部集成测试已执行 |
| `9a2c1267909c3cb1fc40a27ae401bf9035b5a4ed` | [Python MCP](https://github.com/suoak/WeKnora/actions/runs/40171577746) | PASS；Python 3.10/3.11/3.12/3.13 测试与 wheel，发布步骤 SKIPPED；不是实际业务检索验收 |
| `cf1e56f853f0139f09efe2b85427c79d5a3eb7be` | [独立 V2 门禁](https://github.com/suoak/WeKnora/actions/runs/40175081099) | PASS；含批准撤回与跨用户拒绝、Portal/MCP 私有别名过滤、真实 PG/竞态、迁移、前端；不包含后续新增的目录持续发现 |
| `b3e7e3f43bd8ebc26dd107295a5d8363eb80d843` | [原生完整回归及构建](https://github.com/suoak/WeKnora/actions/runs/40174957284) | PASS；定向 PG 20 条 PASS 事件、旧身份/Asynq 恢复 166 条 PASS 和 1 条真实 Docx 缺配置 SKIP；全量 14441 条 PASS、238 条 SKIP、无 FAIL。条数包含子测试；可选外部集成的 SKIP 不算覆盖 |
| `282df329a79dc357fc88ee446faad010e5ef06ab` | [原生定向 PG 门禁](https://github.com/suoak/WeKnora/actions/runs/40172962188) | 定向步骤 PASS：真实解析/向量发布、新引用来源的变更/不变/失败保留版本/断连拒绝、接管恢复。飞书与模型响应为合成数据；完整原生应用回归仍在执行。该 SHA 的独立门禁 lint 有一项 QF1008，已在后续提交修复，不能标为整套 PASS |
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

浏览器还有单独的兼容性限制：`crypto.randomUUID()` 需要安全上下文，不能假定 HTTP 内网地址可用。批次幂等键已改为 128 位 `getRandomValues()` 随机值；缺少安全随机源时拒绝提交，不回退到 `Math.random()`。定向测试两条通过，类型检查通过。Nginx 的精确回调页面现发送 `no-referrer` 与 `Cache-Control: no-store`；上线前仍需在实际隔离入口检查响应头。

## 打包服务器与切换授权

- 用户已授权覆盖打包服务器现有 80/8080 服务；这不授权复用或修改现有数据库与业务文件。
- 这两个端口当前属于同一个入口容器。已记录入口镜像 ID、端口与只读配置挂载路径，尚未停止或替换它。
- 历史构建目录所在分区剩余约 1.4GB，Docker 所在 `/mnt/sata0` 约 1.2TB 可用。后续使用该挂载点的新目录。
- 已核对已有构建镜像 ID 和原生库摘要。旧库源码指纹未与当前源确认一致，不将旧库直接充当当前版本的构建产物。
- 再次以 LF 规范化后比较：Cargo.toml、build.rs、Rust 源码和 C 头文件一致，但 Cargo.lock 和原生构建脚本仍不同。当前构建脚本包含 ZIP 依赖安全修复，因此必须按当前锁文件重建，不能直接复用旧库。
- 部署前仍需独立 Compose 项目、PostgreSQL/ParadeDB、Redis/Asynq、卷、网络与索引，验证迁移、备份恢复、应用启动和服务回滚后才能切换入口。

## 尚未完成

- 目录持续发现实现待当前 SHA 的 Linux PG/竞态验证。已有本地单页遍历、重启序列、快捷方式循环/目标变化及 SQLite 迁移回滚证据。内容更新仍逐文档明确启用；目录权限失败不切换身份，达到容量/深度限制显示不完整并要求复核。
- 后续发现已经交给来源管理的文档时只返回“已有知识”，不再分配首次创建 attempt。来源的暂停、保护和增量版本仍由原引擎处理。单轮发现最多 1000 个条目、1000 页、10 层；瞬时错误保留当前页并有限重试三次，耗尽后保持 HOLD。上述新边界正在当前 SHA 回归，不能沿用旧 SHA 的 PASS。
- 新增迁移 3041 只创建目录发现及路径审计表，历史来源游标、租约与索引不改；有目录状态或审计数据时拒绝向下删除这些表。最新版本断言与治理清单同步更新。
- 新凭据引用到真实解析、向量及发布链路已有合成接口/真实 PG 的变更、不变、失败保留旧版本与断连测试。真实首次入库到接管再到增量更新的完整串联，以及实际网页、Portal、Go MCP、Python MCP 业务检索仍待验收。
- Portal/MCP 目录与计数使用的表别名已补私有范围过滤；策略快照也在事务开始时检查私人归属。新增 SQLite/PG 回归已提交，当前 SHA 的完整门禁仍在执行。
- PR #50 仍 open，解析质量扩展等待其最终集成条件，不能修改其正在处理的文件。
- 测试站尚未部署，单个真实飞书账号尚未完成 OAuth、Docx/Sheet/Bitable/Wiki/Drive、修改更新、网页和 MCP 验收。
- 真实跨员工场景按用户调整记为 NOT_TESTED，使用隔离 KnowHub 用户、模拟 OpenID 与真实数据库补充安全测试。

任务继续推进至测试站交付；不得自动合并 main 或部署生产。
