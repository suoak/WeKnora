# 自动更新停止锁与真实只读 Docx 联合修复验收

2026-10-26，独立分支 `feature/knowhub-feishu-access-v2`。仅处理隔离测试站，不合并 main、不部署生产、不修改 PR #50 或 24 项存量修复范围。

产品修复源码：`2a7f651e6ed078fcf5113905a9358a82084d7d17`。另有独立操作诊断工具提交 `100f56b7490159b99843d4eb70f9dc68fa382ebb`；该提交的 `internal/`、`frontend/`、`migrations/` 与产品修复提交完全相同。

## 自动更新：已确认根因，恢复尚需批准

只读证据：[控制器及消费者快照](evidence/joint-control-readonly-20261026.json)。快照同时读取 APP/worker2 日志、Redis 原始控制注册与 Asynq 心跳，不通过 Inspector 清理过期记录，也不修改锁或队列。

| 北京时间 | 真实事件 |
| --- | --- |
| 11:30:49 | APP 的 Asynq 消费者已启动，并获控制器接纳 |
| 11:31:05.224 | worker2 的 Scheduler 启动，其控制器已经进入共享注册表 |
| 11:31:05.271 | APP 记录首个 `automatic_permit_consumer_lost`，封锁许可并暂停 sync 队列 |
| 11:31:05.450 | worker2 观察到既有锁，记录 `automatic_permit_tripped` |
| 11:31:05.606 | worker2 的六个 Asynq 池开始处理；比首个误停晚约 335 毫秒 |

原判断把“已注册控制器但尚未被 Asynq 观察到的新消费者”当成“此前健康的消费者消失”。已接纳的 APP 不享有其他进程的启动宽限，因此提前触发永久停止。`automatic_permit_tripped` 是之后持续观察到首个停止锁的结果。随后出现的 Redis 超时不是第一次停止的原因。

14:53 的实际拓扑为两个进程、两个有效 controller lease、两个 active sync/low 消费池；各进程 maintenance concurrency=1，另有其他用途的池，共 12 个 Asynq 注册。两控制器的策略指纹一致，当前没有多余旧控制器成员。WorkerID 使用不同容器 hostname 与 PID=1 组合，不能把相同 PID 当作同一进程。两个 Scheduler 均属实际拓扑；已有数据库 `CreateIfNoRunning` 和确定 Asynq TaskID 去重继续保留。

修复在 `controller.go` / `automatic.go` / `diagnostics.go`：

- 首次加入有 Redis 时间限定的 30 秒期限；尚未接纳的新消费者短暂缺席时撤回 permit，保持出站关闭，不写 trip、不恢复暂停队列。
- 接纳记录绑定 policy fingerprint 与 incarnation；已经接纳的消费者消失、过期 lease 或同 ID 异常更换实例仍触发停止。
- 在外部 Asynq 快照前后核对成员修订号，过时快照不再虚构消费者丢失，仍撤回许可。
- 正常 drain 保留 fencing 与审计，处理自身成员及短期退出标记；不清理共享停止锁、不清空 Cursor、不重启 hold/paused/revoked 来源。

用原代码 overlay 重放新加入场景得到失败：`healthy joining consumer was treated as a consumer loss`；修复后加入、期限、过时快照、异常 incarnation 与既有控制测试通过。真实 Redis 的两个独立 Asynq 子进程回归也通过，验证等待新消费者和真正消费者丢失的不同结果。

### 隔离恢复方案（后续已获批准并执行）

下列为原提交确认的方案。用户随后明确批准仅恢复两个来源，2026-10-26 16:02 完成受控恢复；执行和自然同步验收见[报告 20](20_APPROVED_AUTOMATIC_RECOVERY.md)。此前各时间点的未恢复快照仍作为历史证据保留。

恢复仅适用于项目 `knowhub-feishu-v2-d6e99d894d3a`、原 namespace `legacy-feishu`、原两个来源 `6d1698a6e0d9` / `1dcf458346ba`。不放开其他来源。

1. 保存原控制状态和审计证据，核对停止原因为已诊断的 `automatic_permit_consumer_lost`，目标范围与预算均未漂移。
2. 在原 trip 与队列暂停状态下升级经五组门禁验证的 APP、worker2 和前端；验证正常退出、数据库备份及恢复副本。发布代码不代表批准恢复同步。
3. 核对新实例两个 WorkerID、incarnation、策略指纹、lease 及真实 Asynq 消费者一致；旧成员必须有正常 drain 证据，异常成员不得静默抹去。
4. **获得本报告所述恢复批准后**，先正常 drain 并停止两个进程，再确认旧控制成员和 Asynq 消费者退出，待短期心跳过期。Guard 的 `Trip` 会永久关闭该进程本地门禁，因此仅删除 Redis 锁不足以恢复旧进程，必须经受控重启获得新 Guard；不增加运行中的“本地重新打开”后门。若退出证明或成员清理不完整则停止恢复，不静默删除旧成员。
5. 在两个进程已停止、共享成员为空、队列仍暂停时，按原锁值和已保存状态作条件比较后解除此次停止，记录恢复审计；保留 RPM/RPS/TPM、安全预算、来源 allowlist、任务、Cursor、版本审批。Guard 的 stop 与 controller trip 实际是同一个 `trip` key，不存在另一个可以随意删除的 stop key。随后启动新进程，只有两实例形成持续有效 permit、没有新停止事件且队列中不存在其他范围任务，才恢复原 sync 队列；不手工伪造 permit、不全局清 Redis、不换 namespace。任何拓扑、审计、预算或权限异常均保留停止。
6. 等待两个来源自然 cron，核查 sync_log、任务领取和无变化结果；与既有 Knowledge ID、Chunk、向量、BM25 及 Cursor 对账。
7. 受控字段修改通过原飞书有权账号执行；不申请更高 API 权限来代改文档。新版本仍遵守既有发布授权，缺少当前版本批准时应停止发布，不能伪造未来版本同意。之后验证新值和旧值在 Portal、Go/Python MCP 中的合法检索结果。

15:33 部署后只读核对：原 trip=`automatic_permit_consumer_lost`、sync queue pause=`feishu_safety_stop` 均保持，permit 不存在。新控制器仅有两个成员，策略指纹一致、lease 有效；原 APP/worker2 有正常 drain 记录，其标记已到期但未擅自删除。两个已有 Sheet/Bitable Cursor 均仍在，`sync_history=[]`，无自然同步成功记录。自然同步及修改后的自动更新尚未验收，不能以首次导入或 CI 成功代替。

## 只读 Docx：真实接口拒绝发生在创建导出任务

原批次 `441b55cf85f1`、条目 `7de6370f39d3`、attempt `83203481cc0b` 在 11:52:54 记录 `export_permission_required`，11:53:35 条目结束。没有正文 manifest、正式 Knowledge、Chunk 或索引。旧程序把多类权限错误合并且未保存原始 HTTP/code/request ID，所以不能伪造该次历史请求的精确回执；以下为 **15:09 的单独真实复查**。

[脱敏逐请求回执](evidence/joint-personal-export-20261026.json)。使用已存在的 `test` 个人连接、同一 AppID/OpenID、正常 `UserToken` 行锁刷新；没有 JWT 伪造、应用身份降级或目标发布。

| 项目 | 原成功 Docx 对照 | 新增只读 Docx |
| --- | --- | --- |
| 个人 access 到期后的合法刷新 | HTTP 200 / code 0，同一个刷新后的授权用于两篇 | 同左 |
| Wiki 节点解析 | HTTP 200 / code 0 | HTTP 200 / code 0 |
| 文档元数据 | HTTP 200 / code 0 | HTTP 200 / code 0 |
| 创建导出任务 | HTTP 200 / code 0 | **HTTP 403 / code 1069902** |
| 查询导出任务 | HTTP 200 / code 0 | 未进入 |
| 下载产物 | HTTP 206，36,947 字节 | 未进入 |
| 复查耗时 | 4,502 ms | 1,537 ms |
| 正式 Knowledge | 对照既有 `070925df4104`，未改写 | 不存在，未创建 |

新增文档导出拒绝的请求编号脱敏指纹为 `d7d09275b2ae`。这证明当前个人身份可以读取元数据，但飞书创建导出任务拒绝该资源。当前失败不属于 access 到期、refresh 到期或已证实的整份个人授权撤销；同一授权成功刷新且可完整导出对照文档。

15:34 在升级后再次用诊断工具复查，结果相同：[升级后同身份请求回执](evidence/joint-personal-export-after-upgrade.json)。对照 Docx 4,739 ms 完成；新增 Docx 1,406 ms 在 `create_export_task` 得到 HTTP 403/code 1069902，请求指纹 `ab79efa93dcc`，没有正式 Knowledge。此次读取刷新后的持久化 grant，仍包含 `docs:document:export`；access/refresh 均有效，没有要求用户重新连接。两个资源明确核对为同一个人 AppID/OpenID，未更换身份。

复查还分别调用官方公共权限设置查询：两篇均返回 **HTTP 400 / 99991679（用户授权 Scope 不足）**，最小相关权限为 `docs:permission.setting:read`；该错误与新增文档的 **403 / 1069902** 是两个不同请求，不得混为同一个根因。未取得文档公共设置，因此不能宣称已经证明具体是文档、父文件夹、Wiki 空间或企业密级策略禁止导出，也不能宣称只读角色必然无法导出。

当前实际个人 grant 含 `docs:document:export`、`docx:document:readonly`、`drive:file:readonly`、元数据、Wiki、Sheet、Bitable 及 `offline_access` 等权限，不含 `drive:export:readonly`。同一 grant 的 Docx 对照导出成功是当前接口链路可用的实证；不把两种命名当作等价权限，也不为排错自动增加编辑/管理或更宽泛权限。

15:19 另执行了独立的[应用配置 Scope 审计](evidence/joint-published-scope-audit-20261026.json)：官方 `/application/v6/scopes` 返回 HTTP 200/code 0，共 128 个 tenant 和 76 个 user 项。`drive:export:readonly` 两种类型均存在；`docs:document:export` 出现在 tenant 项，未出现在本次 user 清单；`docs:permission.setting:read` / `readonly` 也仅见 tenant 项。个人 grant 的历史授权名称与当前应用清单的名称存在差异，不能擅自断定两者是别名。当前官方 CLI 的普通导出声明仍包含 `docs:document:export`，而且本次对照真实导出成功，因此也不能仅凭名称差异断定所有个人导出都不可用。源端策略及历史授权差异仍需进一步证据，不能把 1069902 精确归因为某个尚未读取到的策略开关。

该审计仅使用配置管理员的应用身份读取应用自己的 Scope 清单，`document_requests=0`；它与个人文档诊断是两个独立进程和目的。所有真实文档读取/导出均保持个人 OAuth，拒绝后没有应用 Token 文档回退。

员工不需要编辑权限作为 KnowHub 导入条件。具备合法读取、源端允许导出且有目标发布授权即可；对于本篇，需要所有者/管理员核对当前阅读者的下载/导出策略。管理员可先从源端权限设置确认，避免为普通导入扩大授权。若继续通过个人 API 定位策略，则需审核并发布 **user 身份** 的最小权限 `docs:permission.setting:read`，在专用诊断授权流程明确请求并由用户补充授权；现有 tenant 权限不能冒充 user 授权，默认“连接飞书”也不会自动请求该额外权限。这仅用于读取权限设置，不能替代源端导出授权。当前不自动修改 Scope 或引导普通用户进入配置向导。

### 已实现的错误和刷新改造

- 应用 Scope（99991672）、用户 Scope（99991676/99991679）、认证失败、refresh 到期、refresh 被拒和 OAuth 应用配置错误分别分类。
- 元数据拒绝、导出创建拒绝、任务查询拒绝、下载拒绝按实际 API 操作分类，新增 `export_create_denied` 等原因；不再统一猜测成缺少导出权限。
- 每个失败请求保存不含正文、URL、Token 的结构化审计：operation、HTTP、业务码、错误类别及请求编号指纹。原旧失败记录不会被不实“补写”成新的真实回执。
- 用户 access 被飞书拒绝时，携带被拒 token 代际的不可逆指纹，交由原 owner-bound、事务行锁 `UserToken` 合法刷新一次；其他进程已刷新时直接使用新代际，防止重复轮换。权限拒绝或 Scope 不足不会触发刷新或切换应用身份。
- 临时 OAuth 网络/429/5xx 保留暂时错误分类；真正 refresh 到期/拒绝才进入需要重新连接或管理员处理。
- 普通界面只显示短中文原因，技术回执在历史高级详情/管理员审计；三段长提示、身份下拉与隐式共享批准没有恢复，不创建私人 KB。

## 门禁、部署及未完成验收

五组产品源码相同的完整回归全部成功：

| 组 | CI run |
| --- | --- |
| V2 PostgreSQL、真实 Redis/Asynq 双进程、race、lint、前端 | [40283408844：PASS](https://github.com/suoak/WeKnora/actions/runs/40283408844) |
| Native parser、完整应用、真实索引发布 | [40283443294：PASS](https://github.com/suoak/WeKnora/actions/runs/40283443294) |
| 集成安全预算、自动停止、版本保护、恢复与限流 | [40283447179：PASS](https://github.com/suoak/WeKnora/actions/runs/40283447179) |
| Portal/RBAC、Go MCP、完整后端、数据库治理 | [40283450832：PASS](https://github.com/suoak/WeKnora/actions/runs/40283450832) |
| Python MCP | [40283454462：PASS](https://github.com/suoak/WeKnora/actions/runs/40283454462) |

精确源码及结论见[五组门禁](evidence/joint-five-gates.json)。[V2 统计](evidence/joint-linux-test-summary.json)中 control 37、OAuth 6、PostgreSQL batch 66、claims race 41 项通过，均 0 fail/0 skip；连接器等 core 1,143 pass/2 skip。[Native 完整应用](evidence/joint-native-test-summary.json)14,497 pass/267 skip/0 fail，另有明确启用的 native-source 74 pass/0 skip；[集成统计](evidence/joint-integrated-test-summary.json)含真实自动停止、版本保护及图片资产回归。条件跳过没有被当作真实验收成功，必需的真实 Redis/Asynq/PG 用例由专门门禁验证。

隔离访问地址继续为 `http://172.30.232.42/`，它仍是 HTTP 测试入口。已部署产品源码 `2a7f651e6ed078fcf5113905a9358a82084d7d17`：APP/worker2 镜像 `sha256:315bc552506ec0fead2777f811221b2b6d355968210e23e5b1662fe82db14bfe`，CI 前端镜像 `sha256:d2634715cc2f5c367244526a3d07138f9df7fd3757fc2ba23f497027a44c6f0d`。原项目升级、停 Worker 备份和独立恢复副本核对通过，业务表记录数一致；schema 保持 3042:false，不恢复业务库、不重置 Cursor、不轮换凭据、不创建/移动/删除既有 KB。

[部署状态](evidence/joint-upgrade-running.json)、[启动检查](evidence/joint-smoke.json)、[部署后控制状态](evidence/joint-control-after-upgrade.json)和[真实只读库存](evidence/joint-final-readonly-inventory.json)已保存。APP/worker2 健康，前后端 SHA 匹配，回调 no-store/no-referrer 及匿名 API 401 验证通过，`test` 账号和连接记录保留。

| 既有资源 | Knowledge 指纹 | Chunk | 向量（1024 维） | BM25 匹配行 |
| --- | --- | --- | --- | --- |
| Docx 对照 | 070925df4104 | 6 | 6 | 5 |
| Sheet | d0c4602a75ca | 24 | 22 | 21 |
| Bitable | f48df03f6510 | 30 | 27 | 26 |

[部署后真实 MCP](evidence/joint-live-mcp-after-upgrade.json)：Go 的 keyword/semantic/hybrid 共 21 项通过，Python 实际适配器 hybrid 共 7 项通过，包含 Sheet/Bitable 表头和对应行值；另 1 个越出凭据范围的库保持隔离。没有向报告输出正文、检索词或敏感 URL。Portal 的用户真实命中沿用上一轮确认，本轮完整 Portal/RBAC CI 通过，但没有冒充 `test` 网页会话执行新一轮手动 Portal 检索。

用户可刷新测试站，进入 `test001` 后粘贴链接一键提交，或从导入历史查看失败原因。无需编辑权限、无需预检向导；缺少发布授权或源端导出能力的资源仍显示需要处理。原只读 Docx 历史错误码不伪造回填，重试后的新失败才产生新的分阶段审计。

本篇成稿时未通过的业务项：停止锁恢复后的自然无变化调度；受控字段修改后的安全发布、Knowledge ID 保持与 Portal/Go/Python MCP 新值对账；新增 Docx 的源端导出授权与可核验的“只读且允许导出”真实正例。**后续更新：用户已批准限定恢复，16:15 两个来源自然无变化同步均成功，执行证据见[报告 20](20_APPROVED_AUTOMATIC_RECOVERY.md)**。真实改值和新增只读 Docx 正例仍未完成，不进入最终发布验收。PR #50 仍 OPEN，head=`2a532132eb038e0efd5c5564f2f35693577986b6`，保护文件与 origin/main 对比无差异。

官方依据：[飞书错误分类源码](https://github.com/larksuite/cli/blob/main/internal/output/lark_errors.go)、[云文档安全默认权限](https://www.feishu.cn/hc/zh-CN/articles/265017155035)、[创建导出任务](https://open.feishu.cn/document/server-docs/docs/drive-v1/export_task/create)。源端复制/下载设置与阅读/编辑角色是不同维度，本报告的具体 403/code/阶段依据来自以上真实请求回执。
