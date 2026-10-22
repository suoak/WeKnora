# KnowHub Go MCP V1 合并执行清单

更新时间：2026-10-21（Asia/Shanghai）。执行工作树：`E:/code/WeKnora-worktrees/knowhub-mcp-b4-rc3-20261021`；分支 `audit/knowhub-mcp-b4-rc3-20261021`；Git HEAD `e62b17e2d305358467821aeff1cce63ce1d61254`。本清单合并用户的 V1 正式交付任务、MCP 页面优化补充和执行清单要求；**UI 补充不替换任何原交付任务**。

状态仅使用：`TODO` 待执行、`IN_PROGRESS` 正在实施/尚待当前源码验证、`PASS` 已完成且证据有效、`BLOCKED` 需明确外部条件。历史候选 PASS 不能代表后续改动已验证。文档后续更新不改变已冻结源码版本；最终报告必须区分实现指纹和含报告全树指纹。

## 授权与不变边界

- 仅在本独立工作树实施必要开发，并在开发收口后于打包服务器创建独立测试实例。未经另行批准不 commit、push、merge、release，不部署/迁移/写入生产。
- 保留第一至第七批未提交源码、00～58报告、统一审计报告、全部原始 FAIL 和快照。不清理历史资料，不覆盖其他工作树。
- 保留现有五只读工具、user_mcp/OwnerUserID、显式retrieve+enterprise_retrieve、Published动态公共document KB ∪ 当前Key授权私人知识、默认无限期新Key/旧Key不变、Generic JSON/Codex环境变量TOML。
- 保持左侧「MCP 接入」名称、位置、`/platform/mcp-access` 路由；不搬到设置，不新增并列个人入口。团队MCP原管理保持独立语义。
- 不做完整M3、Wiki/FAQ/Graph来源治理、ask/写工具、OA/HR实时在职、WorkMate会话专项、Windows权限改造或全量腾讯上游合并。
- >2048文档KB保持拒绝；未来新增私人tenant仍需明确更新Key范围；不静默扩权，不逐KB审批，不维护公共白名单，不下线旧Python。

## 依赖顺序

最新补充：用户已将“一次性 Secret”改为“本人可长期查看”。该要求覆盖此前一次性展示的产品约束，纳入原 V1，不取消其他任务。bfd6f877 为调整前候选，其已执行证据保留；当前新增加密保存和本人查看后必须重新冻结、构建和验证，不能沿用其 UI/凭证 PASS 为最终版本结论。

1. 保全与核对当前源码、只读检查打包服务器（完成）。
2. 必要链接修复、模型/检索适配、APP/Nginx/配置准备与页面优化并行推进；只执行开发期编译、单元、安全回归、race、vet、前端检查，不逐功能部署正式验收。
3. 收口所有必要源码与测试，审查并行改动，冻结统一候选；同一冻结源构建APP/前端、记录依赖/迁移/镜像/产物指纹。
4. 在授权服务器创建独立Compose项目/DB/Redis/网络/卷，先关闭状态完整启动，再仅对独立实例启用。
5. 集中执行真实服务业务、浏览器/截图、客户端、数据库迁移恢复、动态撤权、负载及回滚验收；外部条件不足如实BLOCKED，不以Mock代替真实认证。
6. 交付阶段一/阶段二完整报告与剩余上线门禁；停止于任何提交和生产操作之前。

## 任务及当前证据

| ID | 工作 / 状态 | 对应源码或材料 | 证据及剩余问题 |
| --- | --- | --- | --- |
| U05 | **IN_PROGRESS** 个人 Key 长期查看补充 | tenant_api_key service/repository/type、user_mcp_api_key handler、MCPAccessKeys.vue | 复用 api_key 字段及 AES-GCM；新增本人 JWT 专用 reveal，列表不返回 Secret，CAS 轮换同时更新密文，撤销/停用/过期拒绝；历史哈希 Key 不自动修改。新测试及候选重建进行中。 |
| S01 | **PASS** 核对基线并完整保全 | `tools/v1_prepare.py`、`evidence/v1/preservation.json` | 6012文件业务manifest与ca5c49a4545fec99f741559d76cfa2a9e2602e6bc4113bd4b20461567aad6d9a一致；8472文件完整本地归档逐项回读。归档SHA e4eb287ef6582edc707766062293387d663451b23f0caad547177377eb9158d5 |
| S02 | **PASS** 服务器只读预检 | `tools/v1_host.py`、`evidence/v1/host-preflight.json` | aqyfzx10，现有服务/端口/目录/网络已检查；Docker Root在/mnt/sata0，根盘当时可用48G。没有停止既有RC3/Python或修改共享配置 |
| U01 | **PASS** 核对入口、最新页面与上游差异 | router/index.ts、config/navigation.ts、MCPAccessKeys.vue、上游McpServerIntegrationSettings/mcpServerIntegration/IntegrationSettingsSection | 路由与导航未修改；上游固定9110cb2...参考文件SHA见evidence/v1/upstream-ui-review-api.json。Raw首次网络失败保留，API读取成功；不复制旧main覆盖 |
| U02 | **PASS** 普通员工主页面优化 | MCPAccessKeys.vue、personalMcpPresentation.ts、personalMcpConfig.ts | 已实现统一URL、单主创建入口、只填名称/有效期、三步引导、个人卡片/详情、历史接入分区、Secret一次展示、Codex/Generic切换、窄屏样式；最终类型/构建及真实浏览器待完成 |
| U03 | **IN_PROGRESS** 历史凭证与私人范围安全管理 | personalMcpPresentation.ts、既有mcpAccessLifecycle/Editor/Scope模块 | 新增测试：客户端名称不代表能力、机器凭据不伪装、主动加新空间保留旧selected限制；不自动更新历史Key。用户提及截图所示8凭证没有可读取截图/后端清单，真实类型UNKNOWN；不擅自读生产凭据 |
| U04 | **TODO** UI浏览器业务与实际截图 | 最终独立APP/前端/Nginx、实际`/platform/mcp-access` | 在集中验收阶段测正常桌面与窄屏、原导航、创建/复制/轮换/停用/历史兼容、错误/空状态；不截图一次性Secret或含明文配置 |
| P01 | **PASS** 普通链接文档最小安全修复 | access/portal_text.go、access/portal_mcp.go、service/portal_mcp.go | 链接目标/图片/HTML等投影为安全纯文本，保留正文/标签；只在个人面启用，原SafeDocumentText最终检查及M2/M3严格路径保留；授权/canonical比较/撤权不变。初始候选463aaf...单元和模型边界测试通过；后续调整须最终重验 |
| P02 | **IN_PROGRESS** 模型与索引适配 | service/portal_mcp_models.go、PG repository.go、portal_v1_models_test.go、portal_v1_postgres_test.go | 新增模型active/tenant核验、维度/非有限值拒绝；真实候选SQL统计发现混维/缺索引时明确失败，无其他模型/tenant回退。1024与798/3584测试待最终执行；不改生产向量 |
| P03 | **IN_PROGRESS** 正式服务/代理装配 | personal.go/config、container/router、frontend/nginx.conf | 保留APP8080同进程/stateless/Bearer/五工具；增加Nginx精确个人路径，允许POST、关闭buffer/cache/retry。最终完整APP启动与正常代理验证待部署阶段 |
| P04 | **IN_PROGRESS** 迁移、发布配置与恢复工具 | config/config.yaml、3034/3035正式双库迁移、tools/v1_test.compose.yml、v1_deploy_linux.py | 配置显式默认关闭、ReviewEnabled=false；不新增审批。P4历史正式链证据保留；当前最终候选的3033副本→3035与恢复集中重验，禁止把down作为默认回滚 |
| P05 | **PASS** 并行修改语义审查（审查本身） | evidence/v1/parallel-review.json | 采用原主工作树两份共享MCP撤权测试，不整体覆盖；OA JIT用户名新分配属无关独立修改，审查后保留原工作树、不混入身份关键路径。团队UI选库修改不覆盖个人页 |
| Q01 | **PASS** 开发期Go单元/安全/race/vet/编译 | tools/v1_development_linux.py | `evidence/v1/development-c2f5d84b033dcbb6/`: core/service-security/race/vet/app-build/postgres-test-build exit 0. Exact runtime/config/frontend/test input equality to frozen bfd6f877 verified in quality-equivalence artifact; original c2 execution retained. Windows linker failure remains separate. |
| Q02 | **PASS** 当前前端相关测试/类型/构建 | tools/v1_local.py、personalMcpPresentation.test.ts、原生命周期/范围/模板测试 | `evidence/v1/local/20261021-180115-frontend-results.json`：44 tests/0FAIL/0SKIP、类型检查和构建均exit0；逐输入复核无差异。不是全前端或真实浏览器通过；若再改前端必须重新执行 |
| Q03 | **IN_PROGRESS** 最终候选集中回归 | 当前完整Go/配置/迁移、Python/服务兼容、必要原生AnyDoc | 所有实际命令、exit、输入SHA、SKIP保留。最终单次相关全包/集成门禁不可被聚焦PASS替代 |
| F01 | **PASS** 统一源码/依赖/迁移manifest及Secret扫描 | tools/v1_freeze.py、evidence/v1/<sha>.json | Frozen candidate `bfd6f8779de22ee9df9153a1c58c936287587c1e10fd3ab396cb13eb3c00c8cd`, 6040 files; per-file SHA256, source archive, secret scan and remote readback verified. Later checklist/test-runner artifacts are recorded separately. |
| F02 | **PASS** 最终APP/前端/完整交付包 | tools/v1_build_linux.py、冻结源码包/产物/镜像manifest | 同份冻结源构建，不从GitHub main克隆旧代码。复用已验证且当前源码一致的AnyDoc静态依赖SHA d5543b...；需实际构建、镜像ID与产物核对，脚本LF规范化记录 |
| D01 | **PASS** 打包服务器独立部署 | `/home/aqyfzx/ai/WeKnora-release-build/knowhub-mcp-v1-20261021/<candidate>`、v1_test.compose.yml | 仅在必要开发和质量门禁后执行；独立project/DB/Redis/internal网络/卷，loopback新端口；不覆盖RC3、不停Python、不复制生产数据/密钥 |
| D02 | **PASS** 先关闭启动、后显式启用 | v1_deploy_linux.py、runtime-config.yaml（仅独立目录） | 真实APP/前端/Nginx健康、schema3035、关闭403、启用未认证401、只读工具列表；ReviewEnabled始终false |
| A01 | **TODO** Portal动态与多私人空间验收 | 正式独立APP/API、合成已发布/未发布KB、两个以上私人空间 | 发布/取消发布、新KB、新文档索引后可见、旧引用失效、公共禁止例外、私人权不受取消发布影响；明确夹具索引与真实异步Worker覆盖差别 |
| A02 | **TODO** 普通链接/索引/模型/安全集中验证 | portal_v1_postgres_test.go、真实APP+模型网络 | 真实SQLBM25/vector/Hybrid/grep、1024、旧维度不混用、URL安全输出、模型spy、机器/直接ID/空集/越权/写工具拒绝；spy只证明合成边界 |
| A03 | **BLOCKED** 正式远端Embedding业务验收条件 | 现有模型registry适配；未获提供独立测试模型地址/凭据 | 不复制其他服务或生产模型密钥。先执行受控模型安全/维度与真实SQL；正式模型调用/相关性必须另有真实条件，不能Mock PASS。该缺口不阻断独立部署与其他测试 |
| A04 | **BLOCKED** 真实普通员工SID登录联合验收条件 | 现有SID/OA登录，非新HR接口 | 独立实例无已确认SID回调/凭据；不更改系统网络和共享SID服务。先用明确合成用户真实本地登录/API完成可执行测试，真实SID待获准正式环境 |
| A05 | **TODO** 最新Codex模板实际CLI调用 | personalMcpConfig.ts、实际Codex CLI、独立网络入口 | 复制最新页面TOML/环境变量，真实五工具、联合检索、撤销有界错误；原第七批PASS不代替最新版本。WorkMate会话专项不做 |
| A06 | **TODO** 迁移副本、备份/恢复及回滚 | 正式迁移器、独立合成3033/3035数据库/备份 | 旧Key/成员/Portal/知识不变、无approved回填/扩权、失败恢复；关功能、保留schema/策略、旧APP/前端恢复及再恢复候选；只操作本project |
| A07 | **TODO** 规模与错误/性能 | 大KB/跨空间合成负载及真实PG | >256适配至现有支持规模，记录Chunk分布/模型耗时/P50/P95/并发；>2048仍HOLD，不以race耗时充当SLA |
| A08 | **TODO** Python/OA/飞书兼容回归 | 原Python55测试/实际HTTP-SSE、OA及Feishu现有服务包 | 当前冻结输入重验；单Worker与Mock/真实云端分别标记；不修改生产Python或恢复暂停的旧来源同步 |
| R01 | **IN_PROGRESS** 阶段一开发交付 | V1源码变化/安全边界/测试/指纹/构建产物/独立部署方案报告 | UI完成不能单独宣布V1完成；全部必要开发、开发质量与统一构建收口后汇报 |
| R02 | **TODO** 阶段二独立部署与集中验收交付 | 实例/容器/端口/镜像/schema/客户端/模型/权限/性能/回滚报告 | 真实缺口保留BLOCKED/NOT_TESTED，明确是否可进入正式测试/生产评审；不自动批准生产上线 |

## 当前明确限制与阻塞处理

- 正式模型和SID条件缺失影响对应真实验收及生产评审，不阻断其余开发、构建、独立合成测试。不得借用生产或其他项目密钥。
- Windows链接器与symlink失败不通过删断言解决；本版服务部署目标为Linux，分别保留证据。
- 截图8凭证真实类型UNKNOWN，不因此推迟UI开发；实际页面只按当前API返回scope_type/capabilities分类，不能凭WorkMate/WorkBuddy名称判定新权限。
- 完整M3/匿名资源broker未闭环，本版只返回授权纯文本与不可直接下载的受控知识引用；链接投影失败仍拒绝。
- 页面引用/布局变化后必须重跑对应前端测试/类型/构建，随后在最终独立实例完成浏览器与截图，不以组件测试代替业务验收。

## 进展记录（追加）

- 已完成：原ca5业务源码核对、8472文件保全回读、服务器只读预检、并行修改审查；初始开发检查PASS记录仅对应463aaf...。
- 正在执行：链接/模型/SQL边界最终回归，页面普通/历史分区及私人范围管理、发布工具与统一候选准备。尚未部署任何V1实例。
- 下一步：完成当前UI/源码检查并更新本清单 → 冻结最终候选 → 同源构建 → 独立部署与集中验收。
- 当前UI的44项相关测试、类型检查、构建已完成并绑定输入；真实页面截图仍等待统一独立实例。c2f5d84b...当前Go core与服务安全检查exit0，新增798/1024/3584模型拒绝/绑定测试及链接/模型输入/撤权测试均实际执行；race/vet/最终编译继续执行。首次原生构建的只读嵌套挂载失败exit125保留，已修正构建工具，未进入部署。

- V1 update: frozen bfd6f877 candidate is building native APP and frontend. Development quality gates completed on identical c2 runtime inputs. Original native mount failure exit125 retained. Acceptance Redis runner corrected separately with its own SHA; frozen source and assertions unchanged. No V1 instance deployed yet.

- Build update: bfd6f877 native APP + frontend images completed. `evidence/v1/build-bfd6f8779de22ee9/images.json` and artifacts/context manifests identify the exact outputs. SheetJS TLS failure and Docker metadata resolution failures retained; verified cache and local image-ID guards recovered them without changing application inputs. Independent default-off deployment and credential-free handoff packaging started. UI implementation/unit/type/build and link projection development gates PASS; real browser/business gates remain separate pending items.

- Independent deployment PASS (scope: synthetic test only): project `knowhub-v1-bfd6f8779de2`, build-host loopback `http://127.0.0.1:60675`, APP internal8080; default-off403, schema3035, nginx-t exit0, synthetic seed exit0, enabled unauthenticated401. Evidence: `deploy-bfd6f8779de22ee9`. Earlier address-pool, executable-bit, dictionary and empty-password/connection-string assembly failures retained; final deployment artifacts have separate hashes. Concentrated acceptance is now running, not yet PASS.

- Latest supplement: new e4d2826ee530191365552a6f37362e0ab017c42e1ab2a3d9647a975d7338c355 frozen and remote readback verified. Long-term encrypted personal Secret viewing implementation is IN_PROGRESS pending new Go/HTTP/browser verification. bfd actual Codex five tools PASS, real SPA creation/rotation and legacy compatibility PASS retained as prior-candidate evidence only. Default-off production boundary unchanged.
