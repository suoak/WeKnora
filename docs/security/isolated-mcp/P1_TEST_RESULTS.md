# P1：30 项安全验收结果

执行日期：2026-11-11（北京时间）。范围：获批打包主机的独立合成环境，完整 APP + PG + Redis + Nginx + 受控模型/APM 接收器。逐项对照原 [TEST_PLAN.md](TEST_PLAN.md)，不继承历史通过结论。

`PASS` 仅表示列出的测试范围通过；`PARTIAL` 表示已执行部分通过、仍有明确 NOT_RUN 子项；`NOT_RUN` 表示缺少对应环境/实现，未编造结果。生产和真实隔离网验收始终独立。

最终实际执行汇总和制品身份见证据 JSON：完整 APP HTTP/TLS 107 个检查、故障/撤权/回滚 25 个检查；Codex CLI 0.146.0 实际调用全部8工具；核心/embedding/fixture/APP构建/race/vet 六阶段；另有权限与服务定向测试。报告不把 30 项矩阵全部标为 PASS。

| ID | 结果 | 已执行证据及未执行范围 |
|---|---|---|
| T01 | PARTIAL | 专用 Nginx `-t`、实际TLS、RO证书挂载和环回8443通过；既有80/8080未改。生产Compose渲染/端口及正式挂载 NOT_RUN。 |
| T02 | PARTIAL | TLS1.2/1.3、测试CA信任/有效期、计划域名SNI通过；生产完整中间链、真实隔离区根信任和旧TLS负向探测 NOT_RUN。 |
| T03 | PASS | 未知CA、错主机名、过期叶证书均在TLS校验失败。 |
| T04 | PASS | 真实 initialize/initialized/tools/list，固定8工具；实际Codex CLI全部8调用完成，无Shell/文件操作。 |
| T05 | PASS | POST JSON、UTF8 charset及JSON/SSE双Accept查询成功；实际服务返回JSON，无跳转。 |
| T06 | PARTIAL | 无状态GET/DELETE返回405符合现有实现；重连及撤权后重新鉴权通过。主动cancel协议和真实SSE恢复 NOT_RUN。 |
| T07 | PASS | 同合成Key经HTTP/TLS身份与KB结果一致；A/C授权不同；选定KB、公共/个人范围及源撤销有真实服务与定向测试。 |
| T08 | PASS | 合成Key撤销/过期、用户/成员/工作区禁用即时拒绝；公共来源撤下不影响独立私人权限。 |
| T09 | PASS | 直接点名ask、add/update/delete、upload/http_proxy/Agent/未来工具，Go直连与TLS都拒绝；guard单测写handler计数0。 |
| T10 | PASS | 合成Key增加ingest/chat能力仍拒绝5类写工具；固定注册表不继承未来工具guard单测通过。未开放任何新增内网写能力。 |
| T11 | PASS | PDF/DOCX/XLSX/ZIP/代码multipart在Go与TLS均415；非法媒体单测拒绝，业务快照无文件或任务写入。 |
| T12 | PASS | JSON content/file/base64/path/url伪装参数拒绝；单测证明在dispatch前终止；合成正文不入日志。 |
| T13 | PASS | 覆盖导入、文本、Chunk、Wiki、FAQ、附件、管理/代理/对象存储族代表路径；Nginx只精确转发MCP，无通用REST转发；9表/文件/队列不变。完整路由分类见REST清单。 |
| T14 | PASS | 根路径、API/其他MCP/文件/健康及管理族默认拒绝，无401当作路径限制。 |
| T15 | PARTIAL | 尾斜线/大小写/重复斜线/点段/编码/query等负向路径通过，Go检查EscapedPath；绝对URI形式等未覆盖变体 NOT_RUN。 |
| T16 | PARTIAL | PUT/PATCH/HEAD/OPTIONS/TRACE/CONNECT拒绝，正常POST保留；WebSocket及所有方法覆盖Header变体 NOT_RUN。 |
| T17 | PASS | Go+TLS正好1MiB成功、+1拒绝；gzip拒绝；未知长度上限单测；CL+TE及冲突CL真实TLS400。 |
| T18 | PASS | 超限结构/未知方法、重复键、batch、非法UTF8及参数上限通过真实请求与边界单测；无写handler调度。 |
| T19 | PARTIAL | 环境Header不能扩权，Origin/Host拒绝、代理固定来源Header；冲突双认证头和真实多源XFF/NAT NOT_RUN。 |
| T20 | PARTIAL | 真实A/C越权文档/KB拒绝及既有授权定向测试通过；多用户并发全部排列和生产共享域 NOT_RUN。 |
| T21 | PASS | file:///、../、UNC作为文档ID均无权限/资源错误；字段白名单拒绝路径代理；无任意文件读取工具。 |
| T22 | PARTIAL | 八工具输出不含合成签名链接、file://和bare path标记，错误/panic不反射，文件族入口拒绝；生产S3/MinIO/匿名桶策略 NOT_RUN。 |
| T23 | PARTIAL | APP停止返回502无fallback，同Key恢复；配置明确proxy_next_upstream off。请求中途断线重放计数专测 NOT_RUN。 |
| T24 | NOT_RUN | 本服务无状态JSON，不产生主动SSE流；实际SSE首字节/持续流/慢读取/主动取消未运行。JSON30秒deadline和并发释放已在T25验证。 |
| T25 | PARTIAL | 60并发有429，4个慢查询达全局上限，内网仍可用，约30秒超时后释放；慢body约15秒断开。真实NAT多源容量 NOT_RUN。 |
| T26 | PARTIAL | 专用PG故障时鉴权fail-closed且恢复；真实共享限流后端故障/多实例降级 NOT_RUN。 |
| T27 | PARTIAL | 正常语义查询与拒绝请求注入合成标记；APP/SQL/LLMdebug/usage/Nginx/APM测试范围无标记或Key，非私有追踪正向对照有效；生产WAF/APM/模型端留存 NOT_RUN。 |
| T28 | NOT_RUN | 没有隔离区出口/NAT/IPv6和生产网络权限；本地环回监听不能替代隔离区80/8080/DB/Redis/存储不可达证据。 |
| T29 | PARTIAL | 本次未触及打包机既有服务；专用HTTP同Key回归及隔离满载仍可用。生产网站/研发客户端本轮回归 NOT_RUN。 |
| T30 | PARTIAL | 实际停止专用TLS代理后内网HTTP可用，重启TLS同Key恢复，9表/存储/队列不变；生产规则/镜像切换演练 NOT_RUN。 |

副作用证据采用内容摘要而非业务正文。元数据usage/last-used允许变化，测试不能证明任意未监测外部系统没有写入；本轮没有接入生产对象存储或真实隔离区。真实生产补证不阻塞本地开发，但以上未完成项必须在生产批准与验收时补齐。

证据入口：`fullapp-acceptance.json`、`faults.json`、`fullapp-side-effects.json`、`fault-side-effects.json`、`codex-result.json`、`build-results.json`、`service-security-result.json`、`final-runtime.json`、`final-log-proof.json`。均在 [evidence/p1-20261111](evidence/p1-20261111/)；未保存Key、原始JSON-RPC或知识正文。
