# Personal MCP 文件新增交付记录

开发分支：`feat/personal-mcp-content-write-20261112`，继续基于已有 `6ff79c87d` / `5dc9808f6`；未重做文本新增。线上发布必须先通过 PR 合并 main，再以 main 构建部署。本轮不部署生产，也不更改 8443 隔离配置。

## 本轮范围调整

用户明确要求暂不考虑员工在职状态和 ClamAV，以快速开发、验证为先。因此继续实时校验本地账号启用状态、成员关系、Key 有效期/撤销/新增能力、KB 和共享权限；没有把历史 OIDC 登录证明当作实时在职证明。未配置扫描地址时可上传；配置后扫描异常、非 clean 结果均拒绝。不能将本轮结果称为病毒扫描验收。

## 实现

- 云外新增 `upload_document`：传入 `filename`、规范 base64 文件内容、目标 KB 名称或 ID，以及 `idempotency_key`；不接受本地路径或 URL。
- `get_document_processing_status`：只返回本 Key 创建的文档处理回执，复核当前写权限，不返回正文或存储路径。文本和文件均可查询。
- 支持 PDF、DOCX、XLSX、UTF-8 TXT、Markdown；文件最多 8 MiB，云外 RPC 最多 12 MiB（含 base64 开销）。原只读请求及云内 1 MiB 限制保持。
- 扩展名与内容结构校验，拒绝目录路径、空/非法文本、危险 PDF、Office 宏/嵌入对象/外部关系、加密 ZIP、重复/越界 ZIP 路径、过量条目/解压大小/压缩比。
- 可选 ClamAV INSTREAM 接口只接收完整内容。实现依据 [ClamAV 协议](https://docs.clamav.net/manual/Usage/ClamdProtocol.html)，不把文件路径或命令交给扫描器。
- 使用有界内存接收文件，不建立长期暂存目录；multipart 资源在成功、失败及取消路径清理。写入前再次检查当前授权；文本和文件复用同一个 `authorize`。
- 通过仅服务端可构造的上下文将确定性 ID 交给 `CreateKnowledgeFromFile`，保留原解析、分块、Embedding、Asynq 流程。普通网页和飞书导入不接收此内部上下文。
- PostgreSQL advisory transaction lock 串行化同一 Key/幂等请求的存储及插入；重试复查当前权限与内容指纹，重复载荷返回同一 ID，不同载荷冲突。锁等待有超时。文件 hash 去重对普通入口保持；MCP 使用独立请求幂等，不更新其他已有文档时间。
- 状态查询与错误返回不回传原始解析异常中的服务器路径；审计记录操作者、Key、KB、文档及结果，排除 Secret 和文件正文。
- 云外注册表包含 12 工具；无新增能力的 Key 和云内注册表均保持原 8 个。直接伪造上传工具名和 Header 仍拒绝。
- 用户通过 `list_writable_knowledge_bases` 选择名称即可；重名时明确要求从列表选择，不猜测目标。前端说明云外文件类型/大小以及云内只读。

## 隔离测试环境

目标仅打包机 `172.30.232.42`，独立目录和容器前缀 `knowhub-personal-upload-r1-20261112`。候选是真实 `cmd/server` APP，使用独立 PostgreSQL、Redis、DocReader。模型调用是无外部请求的本地确定性测试适配器，真实执行分块、Embedding 调用及 PostgreSQL 向量写入，不宣称生产模型质量验证。

空合成库初始化至 3046；APP 显式 `AUTO_MIGRATE=false`；验收前后数据库版本均为 3046。没有连接或升级现有测试/生产业务库。

已用本机真实 Codex app-server MCP 客户端，通过仅本机 SSH 转发访问候选的云外及隔离后端入口；没有调用模型生成。客户端完成实际工具发现、可写列表、只读检索列表、新增文本和上传 TXT。此为实际后端只读边界验收，**不是生产域名 TLS/运维映射 8443 的现场验收**。

## 已观察到的验收结果

- 同 Key 云外发现 12 工具、隔离入口发现 8 工具，隔离入口拒绝新增、上传及状态工具。
- A 只能发现自己的、本人私人及有效跨空间 Editor KB；同空间 B 的 KB 和 Viewer share 不能写。
- Retrieve-only Key、固定范围外目标均拒绝。
- Markdown 文本和 PDF/DOCX/XLSX/TXT/Markdown 文件新增均成功。
- 先故意/实际遇到 DocReader 不可用，随后恢复解析依赖并通过现有 Asynq inspector 重试原来的 3 个任务，保留文档 ID；无重复文档。最终 10 个合成文档全部 completed，10 条 Chunk/向量生成。
- 关键词、语义、混合检索均通过 MCP 返回结果。
- 并发重复上传返回相同 ID；相同幂等键不同文件名/载荷拒绝。
- 共享 Editor 撤销、Key 撤销、本地账号停用后立即拒绝新写入；随后仅在合成库恢复测试状态。
- 上传操作审计包含真实合成操作者、Key ID 和 KB scope。
- Linux 定向文件创建与上传测试通过；本地 access/MCP 完整测试通过。前端相关 20 项测试通过。

首次失败的测试与修正后重跑证据均保留：测试存储目录默认落到 `/data` 导致权限拒绝，已在夹具中显式设为临时目录；Linux 已通过。服务启动遇到主机 glibc、Jieba、空密码 DSN 等环境问题，分别在现有 Bookworm 容器内运行、挂载只读字典、为仅合成实例提供非空测试密码后解决。没有为了通过测试改松权限断言。

候选 `921d92250` 的四组 GitHub CI 均成功：[Personal MCP 专项](https://github.com/suoak/WeKnora/actions/runs/43195877191)、[Portal Final Gate](https://github.com/suoak/WeKnora/actions/runs/43195877004)、[App](https://github.com/suoak/WeKnora/actions/runs/43195876968)、[Frontend](https://github.com/suoak/WeKnora/actions/runs/43195876810)。覆盖完整 internal 后端、相关包回归、race、vet、应用/前端镜像与启动检查。工作流见 `.github/workflows/personal-mcp-content.yml`，结构化结果见 `personal-mcp-upload-evidence/ci-candidate-921d92250.json`。

最后补充文件 INSERT 失败后的清理修复：只有明确的 `ErrKnowledgeNotFound` 才删除刚存储的文件；数据库读取结果不确定时保留文件，避免破坏可能已提交的文档。真实 SQLite 触发器强制 INSERT 失败，验证文件清理。该修复的 Linux 定向测试、实际 APP/验收工具编译均 exit 0；源码 SHA256 与远端逐项一致。最终二进制替换的只是本任务带独立 label 的 APP 容器，再次执行文本、五类文件、并发、索引检索和撤权验收全部通过，现有 18 份合成文档均 completed。最终提交的 CI 状态以 PR 检查页为准。

打包机完整包回归未全部成功，原始证据保留：无网络运行受 DNS 限制；恢复网络后 repository、handler、router、middleware、config 成功，service 的 `TestQueryTemplatesEnsureAndReplaceDoNotShareSingleflight` 在等待 provider 的 1 秒断言失败，整个包随后达到默认 10 分钟超时（当时执行到 Wiki 测试）。两次定向 race 均 exit 0。这些记录不计为全量通过，也没有改松沙箱/Wiki 测试；同候选在 GitHub 的全量后端及相关包回归成功。详见 `regression-results.json`、`regression-online-results.json` 与 `final-log-manifest.json`。

## 使用与回滚

先让 AI 列出可写知识库，按名称选定；客户端读取本地文件并将内容编码成 base64 调用 `upload_document`。同一请求重试必须重用原幂等键。返回 processing 后调用状态工具。后台失败由现有任务重试/授权运维恢复，不创建另一份文件来代替失败文档。

回滚只恢复上个 main 应用/前端镜像，保留 8443 代理和无 Docker socket 配置；本轮无新增数据库迁移。已新增的文档保留，不删除用户内容；旧程序会忽略新 metadata 字段。Key 的 `add_content` 不会转化成 `ingest` 或 FullAccess。

PR：[KnowHub Personal MCP 文件新增](https://github.com/suoak/WeKnora/pull/138)。仍待最终提交 CI、运维实际 8443 映射回归；员工实时在职及杀毒扫描按用户指令延期。本报告不构成生产部署授权。

## 主线兼容性补验

开发期间 main 合入 Personal MCP 调度修复至 `f71b51608`，已在本开发分支通过 `2657df745` 合并，未修改 main。保留主线按真实用户的查询限流、工具发现与 SSE 独立配额；新增操作在身份确认后进入独立写入通道。合并版 Linux access/MCP/config 全套、定向 race 和 APP 编译均成功。两入口的 8 个只读工具现在共用完整定义，新增逐项 schema 对照，包含 Wiki 的范围、offset 和说明，避免云外工具定义遗漏原有参数。

PR 触发的增量 lint 首次发现本次代码的行宽、导出注释和资源关闭返回值问题，已逐项修复，未禁用检查；最终以 PR 最新提交的 CI 为准。打包机重编译及合并版实际 MCP 复验记录会保存在同目录证据中。
