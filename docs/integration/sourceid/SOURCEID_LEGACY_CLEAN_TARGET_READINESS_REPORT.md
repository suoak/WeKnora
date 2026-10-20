# 原 WeKnora 干净候选库准备与验收

2026-10-16，打包测试机 `172.30.232.42`。结论：**CLEAN_TARGET_PREPARATION_PASS / NOT_SWITCHED**。

本轮完成新采集的原始快照恢复、独立候选库完整迁移及新版 APP 业务验收。没有切换默认 APP 数据库，没有启用企业身份绑定，也没有真实 SID 登录。正式生产服务器未访问。

## 1. 当前服务与执行边界

| 项目 | 收尾实测 |
| --- | --- |
| 默认 APP | `035f35e1d388a2f02615d35cf826bbdcc22e56b0`，healthy，容器 ID 未变 |
| APP image | `sha256:2d91336c3f5ba1ba330820801c7bf0573b1c9c45b9280994dd0862d016435286` |
| 默认 Frontend | `1ba24ef5f9a730bb98d9ade6a5a5d141ea49292b`，容器 ID 未变 |
| 默认数据库 | 3027，dirty=false；用户2、租户1、成员2、有效知识库0 |
| 原数据库 | 79，dirty=false；未在原库执行迁移或凭据清理 |
| Basic | 默认服务保持 `oauth_form` |
| 企业门禁 | protocol_only=true，contract_confirmed=false，email_binding=false，auto_binding=false，logout=false；Session 模式未设置，仍为默认 external |
| 其他现场开关 | 默认服务原有 OIDC=true、diagnostics=true、AUTO_MIGRATE=false，本轮保持原状 |
| 其他服务 | `weknora`、`knowhub-sourceid-business`、`weknora-segment-test` 的既有容器元数据与准备前一致 |

默认服务的 DB_HOST/DB_NAME、80/8080、Compose、PostgreSQL、Redis、DocReader、原数据卷均未修改。原库与当前验收库的账户没有按邮箱合并。候选验证不使用任何历史 Token、API Key 或真实 SID 身份。

## 2. 快照来源、容量与目标标识

准备前磁盘可用 255,459,758,080 字节、可用内存 28,893,732,864 字节；收尾磁盘可用 255,387,316,224 字节。容量检查通过。

旧的**原始干净快照** `knowhub-legacy79-rehearsal-20261016/private/database79.dump` 与当前源库的选定账户、内容、ACL、向量、资源及模型标识字段一致，旧 `files.tar` 与当前源文件一致。该比较不包含凭据及运行态表，不能据此宣称两个数据库全部字段完全相同。本轮仍重新采集原库和原文件，未使用带 Mock 身份的历史演练 pgdata。

新快照代次：`20261016-clean-v1`。

| 内容 | 标识 |
| --- | --- |
| 独立目录 | `/home/aqyfzx/ai/knowhub-legacy-clean-target-20261016` |
| 原库新备份 | `private/source79-clean.dump`，21,489,454 字节 |
| 同代文件归档 | `private/source79-files.tar`，2,570,240 字节；实际3个文件，共2,554,991字节 |
| 候选数据库 | `knowhub_legacy_clean` |
| 候选 PG 容器 | `knowhub-legacy-clean-20261016-pg`，已停止，restart=no |
| 独立 PG 卷 | `knowhub-legacy-clean-20261016-pgdata` |
| 独立文件卷 | `knowhub-legacy-clean-20261016-files` |
| 凭据清理后备份 | `private/sanitized-3027-clean.dump`，21,678,050 字节 |
| 业务验收收尾备份 | `private/ready-3027-clean.dump`，21,678,448 字节 |
| 候选配置 | `private/candidate.env`、`private/postgres.env`，含新随机配置，仅在受保护目录保存 |

原 APP 已停止，采集前后源库选定字段、计数及文件字节一致；数据库备份使用 pg_dump 的事务一致性快照。**没有实施全系统写入冻结，不能将数据库与文件采集描述为跨系统原子快照。**正式切库前必须再次确认源数据未变，否则在批准的冻结窗口重新采集成套快照并重验。

独立目录及 private 目录权限0700，private 下普通文件0600。备份含原测试数据，应限制访问并由负责人决定保留期；未上传 Git/CI。报告不保存账号邮箱、文档正文、向量、凭据或它们的指纹。

## 3. 隔离措施与实际连接验证

候选 PG 使用 `network=none`，不发布端口。临时 APP、Frontend、独立空 Redis 仅共享该无网络命名空间，通过 loopback 通信，不连接默认服务 Redis，也不继承旧队列。

候选使用原 APP 035f35e1d 和 Frontend 1ba24ef5f 的已验收不可变镜像，无重建或拉取。文件副本只读挂载 `/data/files`；原 `weknora_data-files` 仅由备份/比对助手只读挂载。未将原文件卷作为可写卷挂到候选 APP。

候选内实际连接 `172.30.232.42:80`、`:8080` 及外部 `1.1.1.1:443` 均失败，形成运行态出口阻断证据。没有探测生产服务器。候选 OIDC=false、diagnostics=false、protocol_only=true、contract_confirmed=false、email_binding=false、auto_binding=false、logout=false、Session=external、AUTO_MIGRATE=false。没有 SID Secret、Mock CA、Mock SID 端点或真实授权事务。

候选 APP/Frontend 使用 Docker log-driver=none，应用日志目录位于临时 tmpfs；请求和业务响应只在受控进程内断言，不输出原文。收尾已删除临时 APP、Frontend、Redis，停止候选 PG，保留离线数据库和文件卷。

## 4. 79→3027 迁移与数据一致性

使用候选 APP 镜像内标准 migrate，从79执行完整45项 up迁移到3027，包括003025/003026/003027，未跳版本、未 force、未在原库或默认验收库执行迁移。

| 检查项 | 源库与候选结果 |
| --- | --- |
| schema | 原79 clean → 候选3027 clean |
| 用户 / 租户 / 空间成员 | 3 / 4 / 5，数量与选定原值一致 |
| 系统管理员、跨租户标记及原 user_id/home tenant | 一致；原账户未重建 |
| 组织租户成员 | 0，保持一致；原库没有 organization_members 表 |
| 知识库 | 总数2，有效1，原ID、所属租户、creator、模型引用一致 |
| 文档 | 3，原ID、解析状态、文件句柄及引用一致 |
| 分块 | 总历史记录19,786；有效非空分块2,717，内容及引用保持一致 |
| Embedding | 2,717，原向量及引用一致；缺失chunk/doc/KB引用均0 |
| 文件资源 / 资源绑定 | 3 / 3，句柄与物理引用一致，实际文件字节一致 |
| kb_shares | 0，未新增共享权限 |
| 索引 | 无 invalid/not-ready 索引 |
| 扩展 | vector 0.8.1、pg_search 0.22.2，与原环境兼容 |

完整性比对在受控进程内完成，公开证据仅保存各表一致/不一致结果。运行态认证、模型秘密和同步配置经过有意清理，不属于“所有字段原样保留”的声明。

首次候选凭据清理引用了实际不存在的 sandbox_config 列，事务整体回滚；按实际表结构修正后仅重做凭据清理事务，未重做迁移。另一次验收等待条件过早，以前端健康替代后端就绪，已改为等待实际后端及代理配置接口后重验。两次失败均保留安全摘要，最终 PASS；均未涉及原库或默认服务变更。

## 5. 旧凭据与外部配置处置

以下操作**只针对候选**：

- 历史 auth_tokens 共48条，原未撤销6条；清理后未撤销0。候选业务验收新增的临时令牌也全部撤销。
- tenant_api_keys 共6条，原未撤销1条；清理后未撤销0。MCP endpoint 禁用且清除 token_hash/token_hint；MCP OAuth token/client secret 清理。没有使用旧 Key 发请求。
- 原密码认证在候选禁用。验证原管理员业务能力时，仅在候选内存生成一次临时密码并写入其新密码哈希；验证结束后再次禁用。没有读取旧密码、变更邮箱或改变权限。
- 候选使用新随机 JWT/AES/数据库凭据，不继承旧签名密钥。保留记录不代表历史 JWT 或连接器凭据仍可用。
- 1条 data_source 禁用、取消计划、清除连接配置；MCP服务禁用；system_settings清除；租户credentials/parser配置清除；非本地存储禁用并清除配置。无旧同步任务自动恢复。
- 模型3条中原有2条加密API Key。模型ID、类型、状态和Embedding引用保留，秘密及额外请求配置不继承，网络基址改为候选不可用loopback。未尝试解密旧Key，也未调用真实模型。
- 候选 external_identities、绑定审核、SID企业会话均为0；没有预建真实或Mock SID绑定。

恢复清单：模型供应商/模型版本/向量维度及获批端点需要重新核对；2份模型密钥、解析器和连接器配置须通过受保护流程重新录入；API/MCP Key经批准后走正常业务重新签发；本地账号若需启用密码，走受控重置。同步服务逐项批准后恢复，不能从原快照整体回灌秘密配置。

## 6. 新版 APP 实际业务验收

验收使用真正的 APP/Frontend、候选数据库及文件副本。不是仅执行 SQL 或只检查HTTP200。

| 检查 | 实际断言 |
| --- | --- |
| 启动、代理、Portal静态页面 | health200、Portal200、代理OIDC配置200且enabled=false |
| 原管理员本地业务登录 | 新临时候选凭据登录成功；user_id、home tenant、系统管理员及跨租户标记保持原值 |
| auth/me | 200且用户ID一致 |
| 原知识库 | 原KB详情、文档列表读取200 |
| 原3份文档 | 每份详情200、下载200且内容非空；不导出正文 |
| 已有向量召回 | 通过实际hybrid-search API，使用内存中的原存储向量，返回3条原文档命中 |
| 本地退出 | logout200，原临时Token随后auth/me401 |

向量验收没有生成新Embedding，没有调用外部模型、重排或LLM，没有向外传输原文。**BM25实际查询、外部Embedding生成、重排、完整RAG及流式回答尚未验证。**后续需要批准模型、网络目的地、费用和原文使用范围，并核对原Embedding空间；不能以此次向量召回替代完整RAG验收。

跨空间对照使用一次性兄弟库 `knowhub_legacy_validation`，从清理后的候选快照恢复，不修改干净主候选：

1. 通过已验证单账号准备器先dry-run再创建合成viewer，加入原内容空间，无管理员或跨租户权限。
2. 通过业务API建立另一合成空间及真实存在的知识库，管理员能读取200，viewer在该空间无成员关系，知识库无共享授权。
3. viewer登录及auth/me200，可访问获授权原KB200；访问对照KB403，切换对照租户403。
4. 一次性库已删除。最终干净候选仍是原3用户、4租户、5成员，没有新增合成邮箱、空间或知识库，没有改变原管理员邮箱或权限。

## 7. 正式切库操作方案——尚未执行

### 切换前准备

1. 单独批准维护窗口、负责人及写入冻结，确认所有实际入口和写入方，不能仅停止APP就宣称原MCP或其他客户端已冻结。
2. 再次核对原79数据及文件与 `20261016-clean-v1` 是否一致；有变化则在冻结窗口重采成对快照、重做候选验证。
3. 制作当前默认3027数据库、对应文件卷、受保护配置及完整Compose/pin顺序的最新成套回滚备份，保留当前镜像与全部原卷。不要将默认viewer与原管理员按邮箱合并。
4. 候选管理PG凭据不直接作为长期APP凭据：准备限定候选DB的最小权限应用角色及仅APP可达的私有网络，禁止发布候选PG到主机。当前network=none仅适于隔离验收，尚未配置正式切换网络。
5. 准备仅覆盖APP的DB连接、独立Redis/命名空间及文件挂载配置。DB必须是 `knowhub_legacy_clean` 对应新PG卷，文件必须是本代 `knowhub-legacy-clean-20261016-files`；不得挂原WeKnora卷或默认验收卷冒充候选文件。
6. 受控准备本地登录重置/管理员恢复通道；旧密码已禁用，不能切换后期待原密码直接可用。模型/连接器按批准清单恢复，未批准的同步继续关闭。

### 切换、验证与解冻

冻结默认服务写入并备份 → 启动受限候选依赖 → 使用已核准镜像和受保护新配置仅重建APP → 校验实际DB/文件配对、3027clean、原用户/权限、文档下载、auth/me、跨空间拒绝 → 经负责人确认后解冻。

保持oauth_form及全部企业身份门禁。本次候选准备不构成SID自动绑定或local_approved的启用批准。允许文件写入时仅给新候选文件卷写权限，原卷继续保留。

### 回滚及新增写入

失败先冻结写入、保存故障候选DB和文件的成套快照及安全日志。恢复切换前默认APP受保护配置、原DB目标和**对应文件卷**，继续使用当前035f35e1d镜像及oauth_form；不执行数据库降级、不删除候选、原库、默认库或数据卷。

切换后已发生新写入时，不能直接丢弃候选或只恢复DB。按user/tenant/KB/doc/resource ID、修改时间和文件代次形成差异清单；由负责人批准导出回填、保留候选只读或放弃测试写入。身份绑定、审核和会话审计定向撤销及保留，不能盲目重放已撤销凭据或跨库合并账号。

## 8. 下一次真实 OA 业务验收前仍需满足

- 独立批准正式切库以及所需本地账号/模型配置恢复；当前候选已验证兼容，但没有替换80/8080的数据库。
- SID确认停用传播上限D、已有SSO再次authorize是否重新检查准入、未消费授权码寿命及停用失效边界C。
- 审批local_approved短会话、无长期Refresh、全角色自动绑定政策和流量隔离；RJGH稳定不回收、RJEMAIL可信、SID授权阶段拒绝无效员工已经确认，不再重复列为未知。
- 安全加载当期有效SID凭据，核对测试人员与原账户归属；不能使用历史Token/Key或Mock身份替代真实验收。
- 单独授权一次新员工事务，验证原user_id/tenant/空间角色/知识库ACL、Portal、auth/me、退出及撤销。保持RS256/Issuer/Audience/nonce严格校验，偶发签名失败仍独立跟踪。

当前结论仅为“干净候选准备和隔离兼容性验收完成”。真实OA业务放行、完整RAG、正式切库均未完成；候选已停止并保留，等待后续授权。

## 9. 可复核证据

- [准备前现场与容量](evidence/clean-target-preflight-20261016.json)
- [快照、迁移与凭据处理](evidence/clean-target-preparation-20261016.json)
- [首次清理失败事务摘要](evidence/clean-target-preparation-attempt1-20261016.json)
- [候选业务及跨空间验收](evidence/clean-target-acceptance-20261016.json)
- [现场不变、权限及停机收尾](evidence/clean-target-closeout-20261016.json)
- 程序：`business-test/clean-target-*.py`。仅安全结构化结果进入仓库；原始数据库、文件备份和配置均留在服务器受保护目录。
