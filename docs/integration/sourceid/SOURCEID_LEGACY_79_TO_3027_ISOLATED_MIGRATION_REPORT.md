# 原 WeKnora 79→3027 隔离迁移与业务兼容性验收

日期：2026-10-16，北京时间约08:16–08:35。目标仅为打包机 `172.30.232.42`。

结论：**FULL_MIGRATION_PASS / ORIGINAL_ACCOUNT_AND_FILES_PASS / SYNTHETIC_ADMIN_LOGIN_PASS / STORED_VECTOR_API_RECALL_PASS**。原库与当前默认验收库均未迁移或切换；未发起真实 SID 登录。

## 1. 预检查与一致性快照

初始空闲磁盘256,683,839,488字节，可用内存28,887,576,576字节。记录现有默认/旧项目容器、镜像、挂载和端口，固定原 PostgreSQL17镜像 `sha256:d6fb8f50e25ea880d20e3acad6862931f97cb23abf4efa9c6256d2e112ed8de4`。[预检](evidence/legacy-clone-preflight-20261016.json)。

独立目录：`/home/aqyfzx/ai/knowhub-legacy79-rehearsal-20261016`。原 APP/Frontend 已停止，本轮未停止其仍运行的 PostgreSQL、MCP、DocReader、Redis，也未停止分段测试项目。

- 从原 PostgreSQL 制作 `pg_dump -Fc --no-owner --no-acl` 事务一致快照，21,489,454字节。
- 原 `weknora_data-files` 仅只读挂载到无网络备份进程；快照3个文件，共2,554,991字节。
- 文件清单和文件内容在快照前后、复制后均在受控进程内比较，结果一致；原数据计数和身份/权限基线在前后比较一致。未输出文件名、正文、邮箱、员工标识或私有比对摘要。
- 数据库与文件未做全局写冻结，不能将其表述为跨系统原子快照；本轮原 APP 停止、文件前后稳定且恢复后的3份文档均实测可读。未来若原系统有并发文件写入，应另安排一致快照窗口。

受保护快照位于 `private/database79.dump`、`private/files.tar`；目录0700，文件0600。数据库副本在独立 `pgdata`，文件在独立 `files`。未使用原可写卷作为副本存储。

## 2. 完整迁移

在独立 PostgreSQL 恢复schema79，固定现有431226c3b镜像内的标准 migrate，执行完整 `up`：80–96的17步、3000–3027的28步，共45个脚本。未 force、未手动改版本、未跳过003025/003026，也未对原库或默认验收库执行迁移。

结果 `3027|false`。旧表原行数迁移前后相同：

| 对象 | 原始与迁移后副本 |
| --- | ---: |
| users | 3 |
| tenants | 4 |
| tenant_members | 5 |
| organization_tenant_members | 0 |
| knowledge_bases（含已删除） | 2 |
| knowledges | 3 |
| chunks（含历史无效行） | 19786 |
| embeddings | 2717 |
| kb_shares | 0 |

有效内容仍为1个知识库、3份完成解析文档、2717条非空有效分块。迁移后无 invalid/not-ready 索引；2717条 embedding 的 chunk、knowledge、KB 引用缺失数均为0。

实际扩展为 `pg_search=0.22.2`、`vector=0.8.1`，未擅自升级扩展或系统库。003007对不可用的0.22.6升级可安全跳过，故不能把schema3027等同于扩展0.22.6。

首次恢复碰到预装 schema 对象冲突，调整隔离恢复命令为 `--clean --if-exists` 后成功；未更改原数据库。初次失败保留在进度证据，最终结果见 [恢复收据](evidence/legacy-clone-restore-retry-20261016.json)、[完整迁移收据](evidence/legacy-clone-full-migration-20261016.json)。

## 3. 隔离与凭据失效

专用 PostgreSQL 使用 `--network none`。候选 APP、Frontend、Mock SID、空白 Redis 仅共享该无外部接口网络命名空间，以loopback通信，无发布端口；未使用可能通过网关触及现有服务的普通bridge网络。实际socket探测到 `172.30.232.42:80`、`:8080`、`1.1.1.1:443` 均不可达；没有测试或访问生产服务器。

在任何候选 APP 启动前，仅在副本：

- 全部旧 auth_tokens 撤销，旧 tenant_api_keys 的 revoked_at 设置生效。
- 旧密码哈希替换为不可本地登录值；生成全新独立 JWT/AES 密钥，未读取旧 JWT/Token/API Key 或密钥原值。
- 模型参数中 api_key/app_secret 去除；数据源设 disabled、清空同步计划和连接配置，清除复制的系统设置，关闭OIDC与诊断。
- 使用全新空Redis，不带入原任务队列；关闭遥测、沙箱和非必要外部能力。没有全局后台任务关闭开关的部分仍可能初始化空闲worker，但无原队列、禁用数据源且网络无出口，不能执行原外部同步；没有宣称所有后台goroutine均不存在。
- Docker日志驱动none，应用临时日志位于tmpfs，移除测试容器后消失。没有读取或导出完整HTTP/业务日志。

之后仅为Mock启用副本的合成OIDC参数、本地会话策略及合成CA/loopback白名单。合成策略引用和2m期限只属于测试夹具，不代表真实会话审批，未传播至默认服务。参见[准备收据](evidence/legacy-clone-app-preparation-20261016.json)、[Mock启用收据](evidence/legacy-clone-mock-setup-20261016.json)。

## 4. 原账号、权限与实际业务访问

候选 APP：`035f35e1d`，image `sha256:2d91336c3f5ba1ba330820801c7bf0573b1c9c45b9280994dd0862d016435286`。Frontend保持现有1ba24ef5f，真实Nginx→APP代理；二者revision不同且兼容本轮已测API。

原管理员的真实邮箱未用于Mock：只将**副本**管理员邮箱替换为合成邮箱，以合成RJGH/sub进行认证，原库邮箱完全不变。账号user_id、tenant_id、系统及跨租户管理标记和原成员/ACL均保留。副本另通过受控tenantless准备器创建一个合成viewer加入原内容空间，没有创建个人owner空间；这不是当前验收库已批准员工viewer的登录，也没有创建真实员工绑定。

| 真实业务HTTP断言（全部Mock SID） | 结果 |
| --- | --- |
| authorize→callback/state→Token→RS256/nonce/UserInfo→原管理员首次自动关联 | PASS |
| 创建local_approved Session，auth/me 200 | PASS |
| 原管理员user_id、home tenant及系统管理标志 | 保持 |
| 第二次RJGH登录同一原账号 | PASS |
| 原知识库详情和文档列表 | 200 |
| 3份原文档逐份通过业务下载接口读取 | 各200、内容非空；未导出原文 |
| 普通合成viewer登录与原内容空间KB访问 | 200 |
| 新建另一空间的真实KB，管理员先确认存在，viewer无成员和共享授权 | viewer读取403、跨空间切换拒绝 |
| 本地退出后旧Access访问auth/me | 401 |
| Mock无效员工在授权阶段拒绝 | provider_error；不兑换Token |
| 主验收4笔合成授权，其中3笔成功 | Token3次、UserInfo3次，每个新code单次兑换 |

无Refresh指不允许续期，并非“不返回任何refresh字段”：当前实现仍签发不超过会话到期的predecessor token供SwitchTenant安全换签使用，`/auth/refresh`实际返回401；未修改该既有会话实现。不能宣称响应没有refresh字段。

原文件采用resource://句柄，需要通过resources物理路径解析，3份原文件均能定位到副本并读取。初始直接将句柄当文件路径的夹具检查失败，修正为资源目录解析后通过，没有修改数据库文件路径或降低路径安全检查。

HTTP夹具初次错误断言“refresh字段不存在”失败，后改为验证实际续期拒绝；仅重置了副本中合成SID审核/绑定/session夹具。原测试用户、默认库及真实身份没有受影响。早期失败与最终成功分别保存：[初次HTTP验收](evidence/legacy-clone-http-acceptance-20261016.json)、[最终HTTP验收](evidence/legacy-clone-http-acceptance-final-20261016.json)。

## 5. 索引与实际检索的准确边界

- 数据库已有向量近邻查询返回3条，未导出向量。
- 在真实APP `/knowledge-bases/:id/hybrid-search` 使用库内已有向量、关闭keyword路径，HTTP200且3条结果均指向原文档。
- 此操作未调用外部embedding、rerank或LLM，未输出原文本或向量。证明实际APP的已有向量召回及原文档引用可用。
- 未使用未获批准的模型密钥；**新自然语言查询的embedding、关键词BM25路径、重排、完整RAG答案及质量未验证**，不能写成全部检索能力已验收。后续只在明确获批模型配置后追加。
- 首次检索夹具因账号最近活跃空间切至禁止资源空间而403；按当前登录返回的active tenant正确SwitchTenant后200。未改权限、未绕过403。[初次检索](evidence/legacy-clone-retrieval-20261016.json)、[最终检索](evidence/legacy-clone-retrieval-final-20261016.json)。

## 6. 最终一致性与收尾

在受控进程内逐项比较原行与副本相同ID的白名单权限列，结果均一致：users、tenant_members、organization_tenant_members、knowledge_bases、kb_shares、resources、resource_bindings。organization_members原库不存在，标为NOT_PRESENT，不能写成抽样验证了不存在的权限数据。原记录之外新增的合成viewer、对照空间/KB不计入“原记录完全未变化”的范围。

关闭候选APP、Frontend、Mock、Redis；在副本撤销全部Access/Refresh及企业Session，active计数均0，停止并移除专用PG容器。5个临时容器全部移除；快照、离线PG副本与只读文件副本保留受控目录。离线副本留有两条合成身份和审计，**不作为可直接上线的干净业务库**，后续正式复用应从快照恢复并重做安全清理。

08:35核查默认现场仍为：APP431226c3b、Frontend1ba24ef5f、DB3027 clean；默认库用户2、企业身份/审核/Session均0。原库仍79 clean，原users3/tenants4及内容计数不变。默认/原项目容器ID、镜像、运行状态、端口和挂载集合均前后相同；分段测试项目未被停止。

`oauth_form`、`PROTOCOL_ONLY=true`、`CONTRACT_CONFIRMED=false`、email/auto绑定false、默认external和AUTO_MIGRATE=false保持；OIDC及诊断现场true亦原样保留，未自行开关。

证据：[权限与收尾](evidence/legacy-clone-closeout-20261016.json)、[现有服务核查](evidence/allroles-live-final-readonly-20261016.json)、[容器对比](evidence/allroles-live-container-comparison-20261016.json)、[私有权限](evidence/allroles-private-artifact-permissions-20261016.json)。

兼容性结论：候选APP能够在79完整迁移的隔离副本中，以合成OA自动关联原管理员，保留原账号/权限并访问原文档、执行已有向量召回；这不是默认服务数据库切换、全模型RAG或真实OA放行验收。
