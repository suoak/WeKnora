# 三仓对照、定向修复与真实内容接口探测

2026-10-26。隔离站已部署：`2607fbec7b7ee63ee66d83fb0e199f02945074fb`。本报告区分“源码支持”“真实接口成功”“完整入库验收”，三者不能互相替代。自动更新仍有持久停止锁及 Bitable hold，尚不能正式发布。

后续用户已批准第 25 份恢复方案，实际执行再次停止，详见 [报告26](26_REPORT25_APPROVED_RECOVERY_RESULT.md)。本报告的原快照不代表恢复后的当前状态；最新两个来源均为 hold，新锁为 `audit_unavailable`。

## 1. 固定对照身份及 main 保护

| 仓库 | 本轮获取的 HEAD | 使用方式 |
| --- | --- | --- |
| [飞书官方 CLI](https://github.com/larksuite/cli/tree/017b19a793ad71794a02887ede04757f3d82e10d) | `017b19a793ad71794a02887ede04757f3d82e10d` | 独立目录只读源码对照；测试站不安装 CLI，不复制 CLI 凭据 |
| [腾讯 WeKnora](https://github.com/Tencent/WeKnora/tree/22a331a2eac6d9ca367cd2fd2cf295cf42c4526e) | `22a331a2eac6d9ca367cd2fd2cf295cf42c4526e` | 已 fetch upstream/main，未合并；交付前复核更新 |
| [KnowHub main](https://github.com/suoak/WeKnora/tree/080f7bb6a01fb6293344b438f1dd74ecb7a681ae) | `080f7bb6a01fb6293344b438f1dd74ecb7a681ae` | 已 fetch origin/main，未合并 |

[PR #50](https://github.com/suoak/WeKnora/pull/50) 已于 2026-10-26 19:29:36 北京时间合并，合并 SHA 为上述 main HEAD。main 相对共同祖先净变更 26 个文件，和本轮起始 V2 的交集恰为 PR #50 的五个文件。已按该 PR 原补丁保留在独立提交 `8a8fbe522`；原生解析器、候选图片回归及 VLM 三个相关文件与该 main 版本一致。其余 21 个 main 文件没有被本轮覆盖；这不代表 V2 已包含 main 的全部提交。

固定身份及差异证据：[three-repository-pins-and-pr50-20261026.json](evidence/three-repository-pins-and-pr50-20261026.json)。未合并 main，未部署生产。

22:27 交付前复核：CLI 与 KnowHub main 均未变化；腾讯从初次固定的 `97d7f0ad7b2b9fbd8bd717d737b9e79be6ace4c9` 前进两项助手交互提交（#4757/#4758）至表中 SHA。已取回并检查，飞书连接器目录无差异，以下固定源码引用和结论仍成立；没有为助手功能改变本次导入范围。

## 2. 三仓能力差异及引入优先级

| 能力 | 飞书 CLI | 腾讯上游 | KnowHub 当前与本轮处理 | 采用范围 / 风险 |
| --- | --- | --- | --- | --- |
| OAuth / Scope | 用户账号存储、刷新串行锁、auth status/check；保存的 scope 不是资源许可 | 连接器身份与配置 | 保留双身份、刷新代际和撤权保护；首授请求已有完整必要权限 | 借鉴诊断分层；不复制 CLI 凭据库或破坏性清空策略 |
| 资源权限 | `drive_permission_auth.go` 同身份查询成员 auth_result | 可读 API / 数据源配置 | 独立查询 view/export/copy；缺字段、Scope 错误不推断许可 | 已实现精确分类；不以编辑角色作为前提 |
| 导出 | 创建任务、轮询、导出文件下载；部分 Markdown 路径实际为 Docs AI | 导出及资源解析模式 | 保留 export-first、DocReader/AnyDoc、结构化 Sheet/Bitable | 不切换存量解析模式；保持现有原子发布 |
| 9499 / 限流 | 仅导出创建/轮询且明确 Too many request(s) 才判限流 | 一般永久 / 临时错误分类 | 按操作和标准消息区分；限流至少等待一分钟并复用既有预算 | 已定向实现；未知 9499 不假定限流、不换 API |
| Blocks | 部分文档内容能力与快捷命令 | 树、表格、图片、附件、嵌入对象 | 个人路径严格完整性拒绝；仅明确格式不支持且三项权限有效时可尝试现有受限转换器 | 不采用占位成功、截断成功或全局 Blocks 开关 |
| Docs AI Fetch | `docs +fetch`，DocxXML / Markdown，版本及块标识选项 | 不作为本轮基础导出链 | 新增只读能力探针；同个人身份、限定获准样本；尚未接正式发布 | 需要独立 XML 适配、资产清单与复杂内容完整性验收 |
| 文件下载 | Range、表示一致性、分片有限重试、长度校验 | 素材与附件关联 | 继续使用已验收下载 / staging；审查 CLI 下载思想 | 不直接引入 CLI 依赖；需要新增路径才逐项复用 |
| Cursor / checkpoint | CLI 不是持续同步引擎 | FetchStream / FullStream / FeishuCursor | 保留现有来源版本、执行代际、已接收 checkpoint、outbox | 不清 Cursor、不跳过未发布文档、不创建新 Knowledge ID |
| 停止控制 | 不提供 KnowHub 的多进程同步控制器 | 无相同全局安全门禁 | 修复审计上下文预算，增加停机阶段 / 耗时证据 | 不延长 permit，不自动清 trip，不放宽 fail-closed |
| 用户错误 | category/code/operation/retryable/action | 连接器错误 | 增加精确分类、简短提示与归属受限的高级诊断 | 仅失败时展示；不恢复常驻长声明 |

源码依据：CLI [资源权限](https://github.com/larksuite/cli/blob/017b19a793ad71794a02887ede04757f3d82e10d/shortcuts/common/drive_permission_auth.go)、[导出错误](https://github.com/larksuite/cli/blob/017b19a793ad71794a02887ede04757f3d82e10d/shortcuts/drive/drive_errors.go)、[Docs Fetch](https://github.com/larksuite/cli/blob/017b19a793ad71794a02887ede04757f3d82e10d/shortcuts/doc/docs_fetch_v2.go)、[下载](https://github.com/larksuite/cli/blob/017b19a793ad71794a02887ede04757f3d82e10d/extension/download/download.go)；腾讯 [shared.go](https://github.com/Tencent/WeKnora/blob/97d7f0ad7b2b9fbd8bd717d737b9e79be6ace4c9/internal/datasource/connector/feishu/core/shared.go)、[markdown.go](https://github.com/Tencent/WeKnora/blob/97d7f0ad7b2b9fbd8bd717d737b9e79be6ace4c9/internal/datasource/connector/feishu/core/markdown.go)。

## 3. 按资源的官方 API 能力及完整性门槛

权限分为四层：应用已发布 Scope、当前个人令牌获授 Scope、当前资源实际 view/export/copy、目标 KB 写入与发布授权。任一层均不能替代其他层。`docs:document:export` 与 `drive:export:readonly` 不假定等价；根据实际接口要求及飞书结构化错误核对，不通过批量增加高权限 Scope 解决问题。

| 资源 / 路径 | 合法适用范围与实际要求 | 完整性及本轮决策 |
| --- | --- | --- |
| Wiki | 使用个人身份解析为实际资源；Wiki 阅读不等于空间递归授权 | 只处理显式提交资源，沿用实际 Docx/Sheet/Bitable 策略 |
| Docx 导出 | 当前个人身份可读且源端允许导出，API 对应 Scope 有效；不要求编辑角色 | 保留原文件、AnyDoc 与原子发布，优先级最高 |
| Docx Blocks | 读取最新块的 API 阅读权限，不等于允许复制、持久化和再发布 | 当前仍需三项全 true；仅创建导出任务明确格式不支持 `1069918` 可尝试；未知403、Scope、429、超时不换接口 |
| Docs AI Fetch | CLI 使用 `/docs_ai/v1/documents/{token}/fetch`；`docx:document:readonly`，服务支持的身份与当前获授范围仍需核对 | XML 是 DocxXML，非 OOXML 文件；Markdown 非完整性凭证。仅限获准样本探测，未实现自动正式入库 |
| Sheet XLSX | 同个人身份及合法导出权限 | 保留现有工作表、表头 / 值配对、公式处理和现有解析验证；不改变已验收链路 |
| Sheet 工作表 API | 按接口核对 spreadsheet 范围、工作表/区域读取与资源权限 | 必须保留工作表 ID、范围、顺序、公式 / 显示值、合并语义和终止分页；尚未作为新 fallback |
| Bitable 导出 | 继续已验收稳定流程；不能因记录 API 可读而忽略字段级权限 | 保留字段 / 值配对、记录分页、版本保护；新值发布仍受 hold 和有效共享政策限制 |
| Bitable 官方记录 API | 表、字段、记录、关联及附件分别验证范围；高级权限可能额外限制字段 / 记录和素材鉴权 | 需证明全量及关系保真；不追踪未授权关联资源，不把缺字段结果当完整表 |
| 普通文件 | 官方同身份下载，资源读取 / 下载许可和有效 Scope | 获取原文件；长度、摘要、类型、安全解析和发布验证沿用原路径 |
| 只读且允许导出 | 角色不是判断依据 | 可沿用合法导出入库，无需编辑权限 |
| view=true/copy=true/export=false | 缺少正式依据证明持久化、Embedding、素材及 KB 发布均获准 | 继续拒绝；不因 Blocks/Fetch 返回正文就放行 |
| view=true/copy=false/export=false | 源端禁止复制 / 导出 | 拒绝正文入库，不读取 Blocks/Fetch 绕过；轻量链接引用也需单独获得元数据 / 可见范围授权，尚未实现 |

不能完整转换的图片、附件、嵌入对象、复杂表格不得标记成功。现有个人 Blocks 转换器尚不支持样本中的 type=34 容器及完整富资产交接；本轮不宣称实现了复杂文档 Blocks 保真。

官方接口对照补充（Scope 是 API 准入，仍须个人资源许可及 KB 发布授权）：

| 接口 | 本轮核实的只读 / 判断 Scope 示例 | 使用差异 |
| --- | --- | --- |
| `GET /drive/v1/permissions/:token/members/auth` | `docs:permission.member:auth`；另有官方列明的等价入口 Scope，不能从名称推定 | [官方说明](https://go.feishu.cn/s/6eXLOpxQk01)分别定义 view/export/copy，缺 auth_result 不放行 |
| `GET /sheets/v3/spreadsheets/:spreadsheet_token/sheets/query` | `sheets:spreadsheet:readonly` 等官方可选范围 | [工作表说明](https://go.feishu.cn/s/6bgRe7o640s)提供工作表身份 / 网格，不能代替完整值、公式及素材 |
| `GET /sheets/v2/spreadsheets/:spreadsheetToken/values/:range` | `sheets:spreadsheet:readonly` 或 `drive:drive:readonly` 等官方可选范围 | [单范围读取](https://go.feishu.cn/s/5_sDxkQ4g02)区分 Formula / FormattedValue / UnformattedValue；只取显示字符串会丢公式语义 |
| `GET /bitable/v1/apps/:app_token/tables/:table_id/fields` | `base:field:read` 或 `bitable:app:readonly` 等官方可选范围 | [字段读取](https://go.feishu.cn/s/62nuKkQlk03)提供字段 ID、类型、属性和分页；必须与记录值按同一字段身份匹配 |
| `GET /bitable/v1/apps/:app_token/tables/:table_id/records` | `bitable:app:readonly` 等官方可选范围 | [记录读取](https://go.feishu.cn/s/61Y-IrQk402)不能代替字段、视图 / 行权限、关联或附件的完整性证明 |

本轮官方文档检索未找到 Docs AI Fetch 对上述受限复制 / 导出组合的持久化许可说明。该接口的可用性结论来自已固定 CLI 源码及获准样本实测，不能提升为生产兼容性或合规放行结论。

## 4. 真实接口探测（2026-10-26 20:26，北京时间）

仅使用 `test` 当前个人 OAuth，限定原成功 Docx 和用户指定受限 Docx；不切换应用身份，不发布知识，不复制 Token。鉴权 API 已确认补授的 `docs:permission.member:auth` 有效。额外的权限设置详情 API 仍返回个人 Scope 不足 `99991679`，这是另一接口的诊断限制，不能等同于资源无阅读权限或此次成员鉴权失败。

| 脱敏资源 | 真实结果 | 能证明什么 |
| --- | --- | --- |
| 成功对照 Docx，Knowledge `070925df4104` | view/export/copy 全 true；官方导出 36,946 bytes 成功；总探测 10,935ms | 当前个人身份合法官方导出仍可用 |
| 同一 Docx，Docs AI XML | HTTP200 / code0；5,009 bytes；558ms；返回 revision | 该 API / 当前 Scope 可用，不证明富文档完整入库 |
| 同一 Docx，Docs AI Markdown | HTTP200 / code0；3,621 bytes；495ms；返回 revision | 同上；未对 Markdown 进行正式索引或发布 |
| 同一 Docx，Blocks | 完整终止分页，24 块；type1=1/type14=4/type2=16/type34=1/type5=2 | 块枚举可用；当前转换器并不支持全部类型，未宣称转换成功 |
| 用户指定只读 Docx，导入 `675195cf454e` | view=true/copy=false/export=false；创建导出任务 HTTP403 / code1069902；无正式 Knowledge | 是源端复制 / 导出限制，无需申请编辑权限；没有调用该文档的 Blocks/Fetch |

对照资源读取前后三项权限仍有效、源版本稳定；探针固定输出 `complete_import_proven=false`、`knowledge_writes=false`。不含正文或资源 URL 的证据将随报告保留。

21:33 最终 SHA 再次验证时 Access Token 已到期；既有 Refresh Token 自动合法刷新成功，三项权限鉴权仍有效，没有重新连接或切换身份。对照 Docx 仍导出成功（11,274ms），受限文档仍为相同 `403 / 1069902`（4,245ms）；未写知识或同步控制。见 [最终版本实测回执](evidence/official-content-paths-release-probe-20261026.json)。

## 5. P0：两次停止与 Bitable hold 分开诊断

### 时间线与确定事实

- 第一轮 `automatic_permit_consumer_lost` 是先前部署的消费者生命周期事件，不是本轮 Redis membership deadline；前轮批准的恢复已用过，不能沿用到第二次停止。
- 16:15 两个既有来源自然无变化同步分别 8.406s / 12.961s，原 Knowledge ID、向量 ID 和 Cursor 未重建。
- 16:30 已检测到用户真实改值；当时缺少覆盖新版本的持续发布授权，旧保护正确拒绝。原正式索引仍持久保留。
- 17:01:10.987937：Bitable 进入 hold。其对应 17:00 同步最终记录 `feishu_permanent_error`、provider_code `9499`；代码路径为永久条目 → `legacyItemNeedsHold` → `SetSourceUncertain`。历史消息未保留，不能证明该 9499 当时究竟是限流还是参数错误。
- 17:01:42.216：控制器持久停止 `automatic_membership_probe_context_deadline`，两轮同步随后取消。没有证据将它重新归因为消费者丢失。

### 已复现并修复的预算错误

`Controller.Run` 每 200ms 采样，整轮 Step 预算 500ms，而原审计 sink 使用独立 Background + 1s，HTTPS 请求未绑定控制器上下文。审计耗时可能吃完整轮预算，之后 membership GET 在已经过期的上下文执行，使错误归属不准确。

修复 `885179d7d`：审计沿用 Step 上下文；sink 即便迟到返回 nil 也检查 ctx.Err；HTTP 请求绑定同一上下文；独立的安全停止仍使用新上下文完成。审计记录增加固定 `probe_stage` 和 `step_elapsed_ms`。未扩大 500ms 预算、750ms permit 或任何限流。

回归证明迟到审计停止为 `audit_unavailable` / stage=audit，之后不再执行 membership/consumer/permit；trip、暂停队列和关闭 permit 全保留。取消的审计 HTTP 不发送请求。

只读 Redis 实测：80 次 membership GET，p50=0.126ms/p95=0.279ms/max=0.393ms；当前无 blocked/rejected clients。AOF delayed_fsync 累积 82，IO 压力当前约 2%，但历史 LATENCY/slowlog 没有保留足够样本。不能仅凭当前低延迟排除历史抖动，也不能凭累计 AOF 数字宣布其为根因。

### 尚保留的状态

| 来源 | 状态 / Cursor 摘要 | 已发布知识持久状态 |
| --- | --- | --- |
| Sheet `6d1698a6e0d9` | 未 hold；Cursor `4432c02be06179a9cb98afd64a1f50bc` | Knowledge `d0c4602a75ca`，24 chunks / 22 vectors，1024维 |
| Bitable `1dcf458346ba` | hold；Cursor `3e3d26e2874a8abf283252e8e1336fe1` | Knowledge `f48df03f6510`，30 chunks / 27 vectors，1024维 |

全局 trip 仍为第二次 deadline，sync queue 仍由 `feishu_safety_stop` 暂停，permit 不存在。两个持续发布策略均 absent：先前两项事务因 hold 拒绝并整体回滚。不能把用户点击一键导入当作新版本批准，更不能在 hold 时偷偷登记放行。

已在 `9371a9194` / `8047bb2cd` 准备管理员明确批准的单次全量对账机制：只允许获批日志的 DB generation 读取 / 发布，15 分钟到期，最终事务重验授权，成功后从持久控制项注册原 Cron。该机制没有现场登记或执行；普通任务仍不能恢复 hold。具体可审阅步骤见 [报告25](25_CONTROLLED_MEMBERSHIP_AND_HOLD_RECOVERY.md)，执行前需要本轮新批准。

当前真实个人 Go MCP 初始化返回403（personal MCP denied），Python 没有合法可检索测试用例完成；这是受保护来源 / KB 范围被门禁排除的真实现状。不能把历史检索通过写成本轮“新值已命中”。

## 6. 本轮已实现的权限与交互改进

- 导出失败后收集全部三项有效动作，不在 export=false 时提前丢失 copy 证据；任何请求失败仍不推断许可。
- 用户 Scope、可刷新 Token、失效 refresh、应用配置、源端阅读、复制 / 导出、KB 和发布批准分层，复用既有合法刷新。
- view=true/copy=false/export=false 的中文为：“当前账号可以阅读该文档，但没有复制或导出权限。请联系文档所有者开放相应权限，无需申请编辑权限。”
- 高级详情只向相同 owner/tenant/KB/import/attempt 返回固定操作、业务码和请求 ID 摘要；跨账号、跨租户或其他 attempt 不能读取。
- 只有需要补授或重新连接时才展示相应操作，不再对源端真实拒绝反复要求补授 OAuth。
- 默认弹窗保持极简，不恢复三条长说明，不隐式创建 KB，不推断共享批准。

## 7. 回归与发布门禁

定向本地回归：控制器 39 条 pass 事件（含 package）；核心 269 个测试 / 子测试通过；前端现有 4 项批量输入测试及 type-check 通过；SQLite 执行围栏套件通过，包含授权失效不释放 hold / 不结束原执行的事务回归；12 项只读与恢复工具用例通过，包含旧批准拒绝及队列范围检查。Linux 诊断构建 `b7fd8ea6c` 通过真实链接，并运行首授、失败详情归属和远端错误分类服务测试。最终产品 SHA 的五组 CI、部署和现场回执另存证据；在它们完成前不能用中间 SHA 的结果替代。

最终 SHA `2607fbec7b7ee63ee66d83fb0e199f02945074fb` 的 [Linux 回执](evidence/acquisition-linux-regression-summary-20261026.json)已核验 artifact 内 tested-sha：core 1,166 项测试 / 子测试、V2 service 69 项、PostgreSQL race 43 项、controller 39 项均无失败，其他权限 / 迁移组见回执。两项显式 LiveApplication Sheet/Table 用例未提供应用身份现场 fixture 而跳过；不把它们计作真实用户文档验收或用应用身份替代个人 OAuth。本轮单次对账及持续策略的 PostgreSQL 非 race / race 用例均确实运行并通过，无缺失或跳过。

恢复工具最终自测见 [临时 Redis 回执](evidence/membership-recovery-disposable-selftest-20261026.json)：network=none、existing_redis_mutations=false；旧审批 / 未获批队列 / 过期授权等离线测试在 `python -S` 下运行通过，实际 SSH 库仅在连接时加载。没有安装生产 CLI 或新增生产依赖。

正式发布仍需：受控停止恢复获新批准；Bitable 完整安全对账与合法恢复；两个来源明确有效的持续发布授权；自然调度改值和无变化对账；Portal/Go/Python MCP 最新值命中；权限撤销、hold、暂停、旧版本、限流和图片资产回归。没有这些证据不宣布自动更新闭环或可正式发布。

## 8. 最终部署与验收结果

五组同运行时 SHA 的 CI 均通过，见 [门禁回执](evidence/acquisition-five-gates-20261026.json)：[V2](https://github.com/suoak/WeKnora/actions/runs/40337052952)、[原生解析](https://github.com/suoak/WeKnora/actions/runs/40337088442)、[集成安全](https://github.com/suoak/WeKnora/actions/runs/40337092948)、[Portal](https://github.com/suoak/WeKnora/actions/runs/40337097441)、[Python MCP](https://github.com/suoak/WeKnora/actions/runs/40337101919)。原生完整应用组有 14,528 条 pass 事件（包含子测试），无失败；269 项按用例条件跳过，不计作真实现场验收。专门的真实 PostgreSQL / Redis / Asynq 和原生发布用例另组运行通过。[原生回执](evidence/acquisition-native-regression-summary-20261026.json)保留各组跳过清单，PR #50 四个父用例及其子用例确实通过。

隔离站 [http://172.30.232.42/](http://172.30.232.42/) 已升级，APP / worker2 healthy，前端、API 和未登录拒绝检查通过；`test` 及已有连接记录保留。升级前数据库备份和独立恢复验证成功，没有修改数据版本或轮换凭据。见 [部署](evidence/acquisition-test-site-deployment-20261026.json)、[冒烟](evidence/acquisition-test-site-smoke-20261026.json)。该地址仍是临时 HTTP 测试入口，未被描述或配置成 HTTPS，原到期门禁保留。

升级后逐项比较证明两来源状态、配置摘要、Cursor、Knowledge ID 完全一致；trip 仍为 `automatic_membership_probe_context_deadline`、sync 仍暂停、permit 不存在、Bitable hold 保留。[对账回执](evidence/acquisition-postupgrade-control-20261026.json)。Sheet 仍为 24 chunks / 22 vectors，Bitable 仍为 30 / 27；向量 ID 摘要与原基线一致，没有重复生成正式知识。

部署后个人 OAuth 实测：获准对照 Docx 官方导出成功（10,770ms）；受限 Docx 仍为 view=true/copy=false/export=false，在创建导出任务阶段返回 403 / 1069902（4,632ms），没有 Knowledge，也未读其 Blocks/Fetch。获准样本的 Fetch / Blocks 仍只作能力对比，不发布新版本。[实测](evidence/official-content-paths-deployed-probe-20261026.json)。

Go MCP 真实初始化仍返回403、Python没有可执行的合法查询案例、Portal 本轮新值查询未执行；均未计作新值命中。[MCP回执](evidence/acquisition-test-site-mcp-protected-20261026.json)。这符合当前来源保护状态，但自动更新业务验收尚未通过。

只读队列诊断发现两条残留投递，分别绑定两个原来源的 canceled 日志，generation=3、execution_claimed=false；没有新增处理中日志。[恢复前置回执](evidence/acquisition-recovery-preconditions-20261026.json)。最终独立 operator SHA 为 `80add81ddb9c2c235f7654dcab3464f83b94936a`，仅修改本机恢复脚本 / 离线测试，未替换上述应用镜像：不删除终态投递，逐条核实后保留，并对队列完整列表 CAS。13 项离线用例和临时 Redis 自测通过，增加 PENDING_CHANGED 拒绝及队列原样保留验证。

恢复未获本轮新批准，尚未执行。下一步只按 [报告25](25_CONTROLLED_MEMBERSHIP_AND_HOLD_RECOVERY.md)申请两个原来源的受控恢复；不要求员工重新提交链接、逐篇补登记版本，不合并 main，不部署生产。
