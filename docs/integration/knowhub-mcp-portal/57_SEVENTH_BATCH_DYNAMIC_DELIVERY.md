# 第七批动态 Portal MCP 交付

## 当前结论

Portal Published 直接决定企业公共范围的源码、动态检索、个人 Key 页面及实际网络入口已实现。取消逐 KB 首次批准、独立员工 Provider 和公共 KB 静态名单。生产配置继续关闭，没有生产写入、迁移、发布、凭据轮换或 Git 提交。

当前不能宣布整个第七批或完整 M3 通过。WorkMate 会话工具装载尚待实际工具调用验证；真实远端 Embedding 模型、生产网络及生产负载没有验收。详见 [56 验收矩阵](56_DYNAMIC_PORTAL_ACCEPTANCE.md)，现行业务规则以 [55](55_DYNAMIC_PORTAL_PUBLIC_REQUIREMENTS.md) 为准。

## 已验证

| 项目 | 实际结果与证据 |
| --- | --- |
| 无批准记录的动态 Published 范围 | 真实 PostgreSQL/ParadeDB PASS，新空间 Draft→Published、新 KB、索引就绪文档自动发现 |
| 公共与私有联合读取 | 两个 Published 空间及独立私有 KB PASS；无需加入公共空间或建立团队 MCP |
| 动态收权 | Portal 取消发布、空间停用、KB 移出/删除、禁止例外、旧 target/历史 Chunk、Key 撤销 PASS；独立私有权限保留 |
| 大 KB | 128/257/512/864/1001 文档，关键词、向量、Hybrid、grep、多 KB、并发及取消 PASS，普通和 race 均有真实日志 |
| 模型输入隔离 | 真实 HTTP Embedding/Rerank/LLM spy，未授权 canary 未进入模型请求；向量为受控 1024 维合成值 |
| 飞书入口 | 现有 Import→Recovery→持久化/dispatch→受控解析→真实索引→非成员读取 PASS；飞书远端与解析输出为模拟 |
| Portal 与个人页面 | 真实 JWT/API 的动态范围展示、Owner KB 例外/CAS 恢复、个人 Key 创建和五工具连接检查 PASS；SID 登录仪式未测 |
| 双入口同范围 | 同 Key 经 APP 与精确只读 Nginx 路径的实际 KB 列表及 Hybrid 结果完全一致；非 MCP 路径、未注册写工具及伪造身份拒绝 PASS |
| Codex | 实际五工具、公共/私有联合、另一员工隔离、Portal 撤销与 Key 撤销 HTTP 401 PASS；SDK 原始负向退出 1 如实保留 |
| Go/Python | core、race、vet、Python 完整套件、旧 Python 实际 REST/SDK、service 和 Agent 完整包分别有退出 0 的最终结果 |
| 前端 | 当前输入指纹绑定的 40 项测试、vue-tsc、Vite build 退出 0 |

## 候选和原始命令

基线仍为 `e62b17e2d305358467821aeff1cce63ce1d61254`，分支 `audit/knowhub-mcp-b4-rc3-20261021`。未提交源码必须用逐文件 SHA256 标识，不能只引用 HEAD。

最终源码、测试及前端输入集合指纹为 `e7ae506eb744b5630fe2cc21734fb6853f259ca8fb99c05c3485cd4015fc2c9c`，共 4164 个文件；全候选逐文件清单见 [final-source-manifest.json](evidence/batch7/published/final-source-manifest.json)。先前完整包的应用源码与当前一致；手工 clients tag 夹具仅把时限从 15 改为 30 分钟、合成 Key 从 20 改为 40 分钟，该调整已由最新真实夹具启动/停止验证，未更改授权断言。

| 候选 | 阶段 | 结论 |
| --- | --- | --- |
| `a59c5bf118c539b3…` | integration | 动态真实 PG 与普通规模测试全部退出 0 |
| `50d88df3cad2e405…` | all | PG race、飞书入口、core/race/vet/Python/旧 SDK/Agent 退出 0；service 总套件退出 1，缺隔离 Redis，保留 FAIL |
| `e393014e31e8a6bb…` | service | 补独立 Redis 和 internal 网络，未删断言；service 与 Agent 完整包最终均退出 0 |
| `3c24a26d469f2e03…` | clients | 页面、双入口、Codex 场景通过；WorkMate 模型不支持，整体客户端门禁 FAIL；夹具超时 FAIL 保留 |
| `e8de673df9739529…` | clients | 保留 WorkMate 会话失败与夹具时限失败；不能用连接成功代替工具调用 |
| `42aa0eaf572df78c…` | clients | 30 分钟有界手工夹具按停止文件正常退出 0；本地 WorkMate 实际会话工具仍 FAIL，不能将夹具退出 0 当作客户端 PASS |

入口脚本为 `tools/batch7c_upload.py start all`、`start service`、`start clients`、`tools/batch7c_local.py frontend`、`tools/batch7c_browser_flow.cjs`、`tools/batch7c_workmate_local.cjs`。日志内 `results.json` 保留完整实际 argv、开始结束时间、退出码；每项有 `.log` 和 `.exit.txt`。不是 `go test ./...` 全仓覆盖声明。

Redis 重跑只使用新建 internal Docker 网络、合成队列与独立 Redis，模拟重启满足既有断言；没有接入生产 Redis。前一轮缺 Redis 的四项 service FAIL 保留，环境补齐后重跑整个包。部分 Agent 测试因缺少可离线安装的 DuckDB 扩展仍按原测试显示 SKIP，不改写为 PASS。

## WorkMate 的已知结果

安装版本 2.4.0。连接测试真实 initialize/tools/list 成功，发现五个只读工具。已尝试隔离 profile 的 Codex 后端与用户授权的本地已配置 profile、现有模型 Provider，未修改客户端源码或模型密钥。

Codex 后端存在模型目录/选择不一致；本地内置模型可正常回复，但会话工具发现尚未证明能加载 KnowHub 工具。`proceed_once` 卡片与 `allow` 授权层需使用兼容的一次性响应形状。每次失败、超时、测试 Key 撤销和临时 MCP 项清理均单独记录。

用户明确授权使用本地已配置 WorkMate 后，实际使用 `C:/Users/admin/AppData/Roaming/CSBU WorkMate` profile、现有启用的 Provider 和 `glm-5.2`，没有复制或修改模型密钥。最终 attempt 6～8 仍未执行知识工具；即使明确要求直接调用非 deferred 工具，模型也报告 schema 中不存在这些工具。实际协议证据 `client-protocol-42aa0eaf572df78c.jsonl` 中 tools/call 为 0，最后一次 Key 撤销与临时配置项删除均为 HTTP 200。当前定位到客户端“连接发现→会话工具暴露”的边界，尚不能推定更底层根因，不修改 KnowHub 授权来迁就客户端。

`workmate-local-configured-attempt2.json` 曾由测试脚本把 ToolSearch 参数中的工具名称误认作实际 MCP 调用，错误输出 exit 0。**该记录不予通过**：服务端无对应 tools/call。后续脚本改为匹配实际工具名称，必须再结合服务端协议确认；原记录保留，不删除错误证据。

人工核对后的机器可读结论为 `evidence/batch7/published/client-acceptance-reviewed.json`：Codex 四个场景 PASS，WorkMate 会话知识工具 FAIL，整体客户端门禁 HOLD。

## 性能边界

普通 PostgreSQL 的合成 1024 维、每文档一个 Chunk、6 样本测量：864 文档 Hybrid P95 为 1761.598 ms，1001 文档为 1960.756 ms；1001 文档三 KB/两空间联合检索及四并发有独立原始数据。完整指标在 `ordinary-postgres-metrics.json`。race 数据只用于并发安全验证，不能当作正式性能基准。上述数据不是生产 SLA 或真实远端模型质量结论。

## 尚未通过或尚未测试

- WorkMate 对话内五个实际知识工具的闭环；连接成功不等于此项通过。
- 真实远端 Embedding 输出维度、质量、多语言相关性及生产规模并发。当前 DB 维度和受控模型测试不能代替。
- 真实飞书云端同步与实际解析 Worker；本批只证明既有导入边界和索引后的动态可见性。旧 legacy Feishu sync 的暂停不解除。
- 生产 80/443 与实际隔离网络通道没有开通或测试。
- 当前单 KB 超过 2048 文档的授权快照仍明确 HOLD，不截断或放宽。非 PostgreSQL 的公共安全检索驱动未验收。
- 完整混合文档 ACL、Wiki/FAQ/Graph 来源治理、签名文件/CDN 撤权属于原 M3 HOLD，未以本批普通 document KB 流程代替。
- Windows Python symlink 权限 FAIL 和历史迁移/SSE 原始 FAIL 均保留；不覆盖历史记录。

## 部署形态、操作与回滚

用户从“我的 MCP”创建个人 Key，默认配置当前可授权的个人空间范围与明确 `enterprise_retrieve`。一个地址同时检索动态 Portal 公共知识和该 Key 范围内的独立私有知识。旧 Key 不自动扩权。普通新文档无需重新审批；发布操作没有额外全员可检索弹窗或免责声明。

实际路径 `/api/v1/mcp/personal`，复用现有 APP:8080，经现有 Web 80/443/Nginx 入口访问；不增加 Go MCP 服务或 Docker Compose 服务。隔离 Nginx 仅允许精确 MCP 路径，正常允许 POST，复用同一后端与 Key。生产 Compose/Nginx 未修改。

`personal_mcp.enabled` 缺省 false，缺 PolicyReference 或依赖也不启用。回滚先关闭该配置，按 manifest 逐文件回退；禁止整体 reset/clean 或覆盖旧工作树。保留 3034/3035 和旧 Python MCP。生产未发生写入，所以无生产数据恢复操作。

先前快照的 6,677 文件完整回读校验通过，前六批 461 份报告及原始证据未变。新证据在 `evidence/batch7/published/`。`final-validation.json` 是证据一致性检查，不是整体验收 PASS；其中任何 FAIL/HOLD 保持原状态。
