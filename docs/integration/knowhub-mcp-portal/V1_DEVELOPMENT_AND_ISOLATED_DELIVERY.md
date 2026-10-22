# Go MCP V1 开发及独立交付记录

状态：IN_PROGRESS。本文随执行更新；构建、部署和正式客户端尚未全部验收，不能据此启用生产。

**最新产品补充优先：**用户随后要求个人 MCP Secret 可长期查看，替代本文早期记录的一次性展示要求。当前开发候选已更新为 `e4d2826ee530191365552a6f37362e0ab017c42e1ab2a3d9647a975d7338c355`，6046文件，远端逐文件回读通过，正在重新执行开发质量检查。下文bfd候选的构建、浏览器和Codex结果属于调整前版本，不自动继承为最终版本PASS。

长期查看复用现有 `tenant_api_keys.api_key` 与AES-GCM，不新增数据库迁移。新建/轮换在具备有效加密配置时保存密文；本人JWT通过独立POST reveal获取Secret，重新核实账号启用、OwnerUserID、user_mcp类型及Key有效状态，响应no-store，审计不含密钥。列表/详情只提供末尾提示和可查看标志。浏览器仅持有当前弹窗内Secret，关闭/切换使延迟请求失效，不写入本地存储。旧哈希型Key无明文可恢复，保持有效且不自动修改，用户可主动轮换。没有有效加密配置时不落明文，保留哈希认证并明确提示无法长期查看。

## 版本与执行依据

统一执行依据为 [V1_DELIVERY_EXECUTION_CHECKLIST.md](V1_DELIVERY_EXECUTION_CHECKLIST.md)。UI 优化补充纳入同一 V1，不替代链接安全、模型适配、服务装配、迁移、构建和集中验收。

- 工作树：`E:/code/WeKnora-worktrees/knowhub-mcp-b4-rc3-20261021`。
- 分支：`audit/knowhub-mcp-b4-rc3-20261021`；HEAD：`e62b17e2d305358467821aeff1cce63ce1d61254`。
- 原开发成果业务指纹：`ca5c49a4545fec99f741559d76cfa2a9e2602e6bc4113bd4b20461567aad6d9a`。
- V1 冻结源码候选：`bfd6f8779de22ee9df9153a1c58c936287587c1e10fd3ab396cb13eb3c00c8cd`，6040 文件。源码包及逐文件 manifest 见 `evidence/v1/bfd6f8779de22ee9df9153a1c58c936287587c1e10fd3ab396cb13eb3c00c8cd.json` 所记录位置。
- 开发前完整归档8472文件，SHA256 `e4eb287ef6582edc707766062293387d663451b23f0caad547177377eb9158d5`；旧报告、原始失败及未提交源码全部保留。
- 后续文档与测试执行器调整单独记录，不冒充已冻结源码内的内容。冻结候选、Git HEAD、当前未提交全树是三个不同版本。

## 本轮必要改动

### 普通含链接正文

原 `SafeDocumentText` 对 URL 拒绝，含正常链接的已授权正文也会整段被拒绝。新增 `access/portal_text.go::SanitizePortalText`，仅在个人 Portal MCP 已核实权限的路径中将正文投影为安全纯文本：保留 Markdown 链接标签，移除目标、图片、HTML 标签、下载路径与 URI。进行有界实体/百分号解码，拒绝无法安全完成投影的输入，再执行原严格谓词。

`service/portal_mcp.go` 保留原始资源归属、版本及正文一致性验证，只对副本输出投影；模型调用和结果输出前的授权复验仍保留。查询投影也在外部 Embedding 之前完成。旧 M2/M3 严格读取器不放宽。没有引入文件 broker，没有声称保护未分类混合 KB 或匿名文件下载。

### 模型与索引

`service/portal_mcp_models.go` 增加模型有效状态及真实所属 Tenant 校验。`repository/retriever/postgres/repository.go::VectorRetrieve` 在精确授权候选范围内检查索引维度与缺失索引，发现混维/不匹配时明确失败，不回退到其他模型、租户或知识范围。

新增单元测试覆盖798/1024/3584维、非法向量、模型停用及跨 Tenant；真实 PostgreSQL 测试 `portal_v1_postgres_test.go` 覆盖 BM25、向量、Hybrid、grep、链接投影、旧维度及缺索引。后者编译已通过，执行结果须由集中验收补充。

### 页面与上游对比

参考上游固定版本的端点卡片、详情抽屉、一次性 Secret 和模板切换；未复制团队端点的 KB/工具/Agent 选择流程。参考源码、下载失败及 API 重试记录见 `evidence/v1/upstream-*`。

保留原侧栏、`/platform/mcp-access` 和页面名称。`MCPAccessKeys.vue` 改为统一地址、创建个人 Key、连接配置和凭证管理的主线。创建只需名称与有效期，默认永不过期。固定个人身份和两项只读检索能力仍由现有服务端核实。

`personalMcpPresentation.ts` 依据真实 `scope_type` 与能力分类个人/历史接入；客户端名称不能代表权限。历史 Key 原权限和日期不变；显式增加私人空间保留旧 selected 范围。详情采用抽屉；旧向导仅用于兼容管理，不再作为普通用户新建流程。团队管理保留在原有位置，没有新建员工顶层入口。

Codex 使用真实 `/api/v1/mcp/personal` 和 `KNOWHUB_MCP_KEY` 环境变量；Generic JSON 同一路径。Secret 只在创建/轮换显示，关闭即清除。配置、鉴权/工具发现不等于客户端会话通过。WorkMate 会话未通过的边界保留。

截图中8项既有凭证的真实类型 UNKNOWN：本次没有读取生产凭证清单，也没有自动升级、撤销或删除。浏览器截图和真实操作结果须在独立实例完成后补充。

### 装配与发布

`config/config.yaml` 显式默认关闭个人 MCP，`review_enabled=false`。Nginx 精确个人 MCP 路径继续代理到 APP:8080，允许正常 POST；禁用代理缓存、缓冲和 POST 重试，保留 Bearer Header。没有新增 Go MCP 服务或后端端口。

3034/3035 沿用现有正式迁移，不补造3032，不 Force Version，不修改生产。回滚首选关闭功能、恢复旧 APP/前端并保留新增 Schema；不默认 down 数据库。

并行修改审查见 `evidence/v1/parallel-review.json`：仅语义采用共享 MCP 撤权测试；独立 OA JIT 用户名分配改动保留原工作树、不混入本轮身份关键路径。

## 开发期质量证据

| 检查 | 当前结论 | 证据与边界 |
| --- | --- | --- |
| Go core、安全回归、race、vet、APP编译 | PASS | `evidence/v1/development-c2f5d84b033dcbb6/` 各命令 exit0 |
| 新增真实PG测试编译 | PASS | 同目录 `postgres-test-build` exit0；不代表执行通过 |
| 冻结候选质量关联 | PASS | `quality-equivalence-bfd6f8779de22ee9.json` 核对4257个运行源码/配置/测试输入完全一致；真实执行候选为c2f5d84b，不虚称bfd重新运行 |
| 前端相关44测试、类型、构建 | PASS | `evidence/v1/local/20261021-180115-frontend-results.json`，0失败/0跳过，各命令exit0；非全前端或浏览器验收 |
| Windows Go链接 | FAIL | MinGW `__stack_chk_fail`；Linux真实测试结果单列，不删断言 |
| 首次原生构建 | FAIL，保留 | c2候选构建挂载exit125，修正为独立只读 `/native` 路径；新候选构建结果另列 |
| 原生APP/前端镜像 | PASS（构建） | `evidence/v1/build-bfd6f8779de22ee9/`：两镜像及产物manifest已生成；非部署验收通过 |
| 真实SID、正式远端Embedding | BLOCKED | 当前独立实例未配置获准测试接口/凭据，不复制生产或其他项目密钥，不以合成通过替代 |

## 独立部署与集中验收方案

实际构建镜像：APP `knowhub-v1/app:bfd6f8779de22ee9`，Image ID `sha256:1bcdc22f7261de300ac0c3a8925c18b1469ab888a4552504db84b1d68bf0ef41`；前端 `knowhub-v1/frontend:bfd6f8779de22ee9`，Image ID `sha256:03bead38d3058f707085a2e42c5e22a622489c37b25bd79aa55a086711ba9ae2`。两者源码标签均为完整bfd候选。仅本机构建、未推送镜像仓库，所以 RepoDigest 为空，不能伪造 registry digest。

构建失败分别保留：SheetJS CDN TLS错误、APP裸Image ID被BuildKit解析为远端镜像、Nginx digest元数据请求受网络限制。恢复方法为lockfile SHA512验证的缓存包，以及核实本地基础镜像完整Image ID后使用本地标签。没有关闭TLS、修改lockfile、改写业务源码或复用旧APP二进制。恢复执行器作为独立SHA制品记录于 `build-harness-bfd6f8779de22ee9.json`。

仅使用打包服务器 `/home/aqyfzx/ai/WeKnora-release-build/knowhub-mcp-v1-20261021/bfd6f8779de22ee9`，独立 Compose 项目、DB、Redis、internal 网络与卷；Web 只监听新的 loopback 端口，经临时 SSH 转发测试。现有 RC3、Python MCP、系统网络与防火墙保持不变。

源码包逐文件远端回读校验后，从同一冻结源构建原生 APP 与前端。AnyDoc 复用源码逐项一致且SHA固定的本机静态依赖，不复用旧 APP 二进制。先默认关闭启动验证，再仅在独立实例显式启用 Portal Published 模式。

集中验证真实 APP/前端/Nginx、合成普通用户登录和 Key 生命周期、公共/两个私人空间、动态发布与撤权、真实 PG/ParadeDB SQL、受控1024维模型、模型安全 spy、浏览器与截图、复制模板后的实际 Codex CLI、迁移恢复与功能关闭/旧镜像恢复。受控模型仅证明协议/维度/安全边界，不证明正式模型业务准确性；本地合成登录不代表SID认证。

## 当前停止门禁

尚未达到 V1 整体验收或生产启用条件。必要开发和质量检查先收口，再完成一次集中部署验收；其余待办继续按统一清单推进。正式模型、SID、生产17空间48KB实际可见范围仍需获准环境证据。>2048文档KB、完整M3、Wiki/FAQ/Graph、WorkMate专项均不扩展本轮范围。
