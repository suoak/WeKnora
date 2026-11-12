# P0 补证及 P1/P2 安全验收计划（2026-11-11 修正）

## 实施记录 R3：P1 测试已授权并执行

用户批准在打包测试环境使用独立账号/Key/合成知识开展正负向验证，生产变更仍禁止。本轮实际结果见 [30项验收](P1_TEST_RESULTS.md) 及 [P1报告](P1_REPORT_2026-11-11.md)；完整APP HTTP/TLS、真实Codex八工具、业务副作用和日志标记均有脱敏证据。未取得的真实隔离网、生产版本及外部日志证据明确 NOT_RUN。以下原计划、R1/R2及其当时“未运行/待批准”的状态原文保留；不把历史测试自动继承为本轮通过。

## 补证记录 R2：P0 B01–B04 已执行

2026-11-11 00:36（北京时间），用户提供用于本次生产只读核验的Key后，对原IP入口直接进行JSON-RPC请求（禁止redirect，凭据隐藏输入，仅内存使用）。[脱敏证据](PROTOCOL_EVIDENCE_2026-11-11.json)

| 用例 | 结果及范围 |
|---|---|
| B01 initialize | PASS：HTTP200，protocolVersion=2025-06-18，serverInfo=knowhub-personal/0.1.0 |
| B02 initialized通知 | PASS：HTTP202；initialize未返回session header |
| B03 tools/list | PASS：完整8工具，与核验main静态工具名一致；未包含ask或写工具 |
| B04 list_knowledge_bases | PASS：HTTP200，JSON-RPC无error，工具isError=false，JSON列表62项；未保存名称/ID/正文 |
| B05 检索及文档读取 | NOT_RUN：本次仅做最小无业务正文协议验证，没有发送关键词或读取文档 |
| B06 容器及制品归属 | NOT_VERIFIED：尚无生产只读管理证据，serverInfo不能证明源码SHA |

这是受控HTTP协议客户端实测，不表述为Codex产品端到端配置验收；没有执行P1负向测试、上传、ask或其他写工具。也没有验证生产内容日志、隔离网络、TLS/8443或其他员工权限。R1中Key/协议待测描述保留为先前状态，当前状态以本节为准。全部P1矩阵仍待批准后执行。

## 修正记录 R1：先验证现有 Personal MCP，再进入隔离访问改造

现有 `http://10.51.134.114/api/v1/mcp/personal` 为用户确认正在使用的入口。本轮不重新实现 Personal MCP/个人 Key/检索。下方 2026-11-10 的全部原计划保留作历史记录；以下更正优先。main 已有8工具注册、调用guard、1MiB、并发8、30秒context，测试应验证/复用这些现有行为，不把它们当作已缺失或已验收。

### R1.1 P0现有入口协议验证（尚未执行）

当前状态 **NOT_RUN_AUTH_REQUIRED**：未取得已授权测试Key文件/可用客户端会话；未通过普通401推断路由，不从用户私人配置/历史fixture/数据库提取凭据。本轮已请求受控文件路径/现有客户端访问方式，等待材料；不创建或轮换生产Key。

取得用于现有生产入口只读验证的明确授权后，使用已有MCP客户端或最小协议客户端，固定目标IP URL，不跟随重定向，不输出或保存请求/响应原文。凭据仅内存读取，不放命令行、配置明文或报告。执行以下步骤：

| P0 ID | 动作 | 留存证据和判定 |
|---|---|---|
| B01 | 用已授权测试Key发送 initialize；Content-Type application/json，Accept application/json,text/event-stream，版本依据目标已有mcp-go支持值协商 | HTTP状态、协商protocolVersion、serverInfo的有限字段、JSON-RPC是否成功；不保存clientInfo自由文本/Key |
| B02 | 依协商发送 notifications/initialized，必要时携带服务端返回的session/protocol header | 记录通知状态及会话是否需要，不持久化session凭据；失败不当成功 |
| B03 | tools/list，按协议处理分页 | 只记录工具名称，核对当前实际集；main静态预期8个，若生产不同则如实差异，不能自行修改生产补齐 |
| B04 | tools/call list_knowledge_bases，arguments={} | JSON-RPC无error且result.isError不为true；在内存校验正常结果形状，只记录成功/条数，不输出名称、ID或正文；即使无授权KB，工具成功仍与实际有知识可读分开说明 |
| B05 | 仅在授权范围给出获准关键词及可读测试文档时，调用search_knowledge和read_document | 记录工具成功/结果条数/引用是否安全，不保存关键词/文档正文；没有材料时明确NOT_RUN，不能编造检索成功 |
| B06 | 与只读运维取得的请求到达记录、upstream、容器image ID/制品来源对应 | 区分真实服务归属与镜像源码证明；serverInfo或成功initialize不能单独推出生产SHA |

这些POST是正常MCP查询交互，不是写入测试；原系统可能记录last-used/usage/查询日志，执行前需确认授权范围及现有日志控制。若未确认载荷日志安全，先仅使用无业务正文的initialize/list/list_knowledge_bases，关键词和文档读取继续待定，不能为了证明协议而把敏感知识送进不受控日志。

P0生产验证禁止构造ask、上传、URL导入、manual、Chunk/Wiki/FAQ写入、恶意正文和拒绝边界攻击；这些负向动作仍只在P1批准测试环境。若使用现有真实客户端，由用户在本机使用已配置的同一Key执行上述只读步骤，提供脱敏协议摘要即可，不需要把Key交给审计报告。

### R1.2 对原P1矩阵的修正

* T04/T09/T10基于现有PersonalServer，隔离固定8工具是生产版本补证后的验收预期；不因旧workspace Server默认包含ask就宣称现有Personal MCP含ask。仍要在测试环境验证直接调用ask/写工具拒绝。
* T07使用同一测试个人Key，对照现有内网personal URL和隔离URL；默认工作区和dynamic_all遵循原PersonalMCPAuth，不额外强制客户端携带X-Tenant-ID。显式伪造工作区仍需拒绝越权。
* T17验证现有1MiB及所有外层wrapper顺序，不再先实现另一套限制。T24/T25沿用现有30秒/8槽作为基线，评估隔离并发预算，避免影响内网。
* T22验证Personal的既有纯文本投影和knowhub引用，不把workspace签名URL重写器引入Personal路径。T27覆盖main新增usage collector、client name/version、perftrace、通用Logger及下游检索/模型日志。
* T29/T30明确原IP URL、存量Key、授权及研发客户端配置全过程不变；回滚不能关闭原personal入口或撤回既有知识策略。
* 所有原有历史测试报告仅用于设计用例，不继承为本次main、生产或隔离8443的通过证据。本轮没有运行应用测试、协议调用或部署验证。

P0进入下一阶段的门槛为：当前部署版本/实际调用链、授权协议复测和隔离网络/TLS归属补证；然后评审最小改造并取得P1确认。若暂缺材料，交付明确开放项而不进入开发。

---

## 以下为 2026-11-10 原始测试计划（历史保留；按 R1 修正解释）

# P1 测试与 P2 安全验收计划

2026-11-10。所有项目均为 **待执行**；本轮只做 P0。本机 DNS/TCP/无认证 HTTP 和证书内存核验见审计报告，不作为下列通过证据。

## 1. 测试准备与证据要求

只在明确批准的测试 APP/数据库/桶/代理与网络执行负向写入测试，禁止对生产发送伪装上传或创建请求。使用合成文档、附件和专用测试个人 Key，测试中同一把 Key 覆盖内网及隔离入口；不读取真实生产员工 Key。测试后只清理专用环境中本次明确创建的数据。

证据只记录时间、环境、镜像 digest、测试 ID、协商协议、状态码/MCP isError、拒绝类别、授权范围标识和无载荷计数。Key、Cookie、附件、原始参数、原始 JSON-RPC、签名 URL、Key 派生摘要不得进入截图、命令行参数、日志或 CI artifact。凭据通过受控注入方式提供；不要用关闭证书校验换取连接成功。

负向写入测试以 handler 调用计数/受控 fake 验证“未调用写服务”，并在测试环境核对知识、Chunk、Wiki、附件、会话/消息、对象存储、队列没有业务写入。允许的元数据计数和 last-used 单列。只看 HTTP 200 或 403 不足以断言 MCP 安全：MCP 工具拒绝可以是 HTTP 200 且 result.isError=true。

## 2. 测试矩阵

| ID | 测试 | 验收标准 |
|---|---|---|
| T01 | nginx -V、nginx -t、Compose 最终选择字段 | SSL 模块/版本可用，配置合法，80映射保留，8443只绑定批准地址，Key只读挂载；无秘密输出 |
| T02 | SNI knowhub.ruijie.com.cn 的 TLS 1.2/1.3 握手 | 域名、有效期、叶→中间链、隔离区根信任全部通过；旧 TLS 拒绝；无不安全绕过 |
| T03 | 错域名、错链、过期/未知根测试证书 | 客户端拒绝，代理不错误回退其他虚拟主机 |
| T04 | Codex 客户端 initialize、通知、ping、tools/list | 实际目标客户端正常连接；只出现固定 8 工具；无 ask/写工具；记录客户端版本及协议 |
| T05 | 正常 JSON POST、合法 charset、Accept 两种类型 | 查询成功；JSON或SSE正常，错误不被改写为HTML/跳转；真实客户端Content-Type差异完成验证 |
| T06 | GET、DELETE、重连、取消、协议版本 | 按实际无状态实现返回规范流/响应或允许的405，不泄露身份；重连仍鉴权，取消后资源释放 |
| T07 | 同 Key 双入口、不同工作区、选定KB | 同一身份复用；工作区/共享/KB范围不扩大；不存在端点机器身份替代 |
| T08 | Key失效/撤销、成员移除、用户禁用、工作区禁用、共享撤销 | 后续请求即时拒绝；使用旧session/连接不能继续越权 |
| T09 | tools/call直接点名 add/update/delete_document、ask、未知/未来工具 | 服务端拒绝且无副作用，即便客户端缓存或伪造tools/list结果 |
| T10 | 测试Key在内网具备 ingest、管理员打开端点全部工具 | 隔离入口仍固定8工具；内网原功能按原契约工作；改注册表默认值不改变隔离能力 |
| T11 | multipart ZIP/PDF/DOCX/XLSX、octet-stream、image、text/plain | Nginx和Go独立拒绝；不创建multipart临时文件/存储对象/后台任务 |
| T12 | JSON伪装 content/base64/url/path/file参数、未知字段 | schema或工具guard拒绝，无日志原文；不尝试URL下载/本地读取 |
| T13 | REST清单全部写族、普通JSON文本、URL导入、Chunk/Wiki/FAQ/skills/附件 | 8443不转发；Go隔离handler也不路由到REST；原服务无业务写入 |
| T14 | 非MCP路径：/、/api/、/mcp/*、/files、/r/*、管理/health | 默认拒绝，无静态页、附件或错误页回落；不以401冒充路径已经限制 |
| T15 | 尾斜线、大小写、重复斜线、点段、编码斜线/点、路径参数、query、绝对URI | 非标准精确路径全部拒绝；代理和Go没有不同解释导致旁路 |
| T16 | PUT/PATCH/HEAD/OPTIONS/TRACE/CONNECT、方法覆盖Header | 不启用未批准方法或WebSocket；正常POST继续可用 |
| T17 | 1MiB边界、+1字节、未知长度chunked、gzip、畸形/重复长度 | 超限/非法帧拒绝，无部分执行或请求走私；连接正确清理；精确上限内合法JSON正常 |
| T18 | 超长query/keyword/slug/ID、深JSON、数组batch、未知RPC方法 | 拒绝或限额，符合协商协议；无大对象内存/CPU失控 |
| T19 | X-Environment/X-Isolated/XFF/Host/Origin伪造、双认证头冲突 | 不改变只读身份；非法Origin/Host拒绝；XFF不能绕过限流和源ACL |
| T20 | 指定其他用户/租户/KB/文档/Chunk/Wiki ID、越界分页 | 严格授权且无存在性/内容泄漏；不同Key并发没有上下文串用 |
| T21 | 文件读取 file://、UNC、../、本地路径、HTTP内网URL | 不执行任意文件/网络读取；只读知识ID必须走KB授权 |
| T22 | 授权文档中含对象存储URL、签名URL、/r/token、恶意外链 | 工具不颁发/透传外部访问能力；8443拒绝文件族；无自动下载、PUT签名或匿名桶旁路 |
| T23 | 上游断开/超时、代理故障、MCP POST中断 | POST不自动重放；写handler始终未运行，查询重试只由明确客户端行为发起 |
| T24 | SSE首字节、连续读取、慢客户端、取消 | 无缓冲阻塞；超时和并发释放符合预算；长连接不会无限占资源 |
| T25 | 速率、并发、NAT多人、超慢body、重复初始化 | HTTP429或规范限额；限制不能靠伪造XFF/工具名绕过；内网HTTP和MCP保持可用 |
| T26 | 认证/授权存储或共享限流故障 | 鉴权失败时拒绝，不回退到匿名/full-access；限流降级策略明确且有上界 |
| T27 | 带合成唯一标记的拒绝payload与正常查询 | 在测试代理/Go/工具/APM/WAF/错误/trace中均找不到原文；只留批准元数据；不把检索输入写会话/缓存 |
| T28 | 从隔离区访问80/8080/其他MCP/DB/Redis/存储及IPv6/备用IP | 除批准8443外均按策略阻断；取得网络管理员规则命中和隔离区实测证据 |
| T29 | 80网站及内网MCP回归 | 原配置和同Key原权限正常，隔离负载下仍符合约定可用性/延迟 |
| T30 | 回滚演练 | 先关闭隔离入口及现有连接，再恢复配置/镜像；80及内网MCP正常，不需改Key或恢复数据库 |

## 3. 分层执行

* Go本地/测试构建：直接调用隔离handler，绕开Nginx重复T07–T22/T26/T27，证明Go独立执行策略。已有 `TestCallHiddenToolIsRefused`、个人Key实时成员关系测试、KB scope/资源绑定测试应按适配范围回归；新增只验证隔离边界的必要测试，不把原测试存在写成已通过。
* Nginx测试容器：使用明确无副作用的记录型upstream，验证路径/媒体类型/大小/重试/流式；确认被拒绝请求没有到上游，再连测试APP验证。
* 网络测试：必须分别从内网和隔离区执行；Windows Codex所在网段不是隔离区替代证据。
* Codex接入：P1执行前核对实际安装版本的官方连接/凭据配置方式；配置中只用安全占位符，人工在批准环境注入测试Key。本轮不写入或修改用户Codex配置。

## 4. 通过门槛

所有适用项目通过且无业务写入证据，生产拓扑与Personal MCP源码缺口补齐后，才能生成P2审批包。无法模拟的边界设备、NAT或客户端情形要明确标为未验证，不能记为PASS。生产验收只执行事先批准的非破坏检查；任何新负向载荷测试须使用专用测试资源和单独批准范围。
