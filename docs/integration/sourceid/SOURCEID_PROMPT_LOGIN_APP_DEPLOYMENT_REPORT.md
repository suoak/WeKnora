# SourceID prompt=login APP-only 部署报告

2026-10-16，打包测试机172.30.232.42，SSH用户aqyfzx。结论：**DEPLOYMENT_PASS / REAL_SID_NOT_EXECUTED**。

## 实际部署

| 项目 | 结果 |
| --- | --- |
| APP升级 | 035f35e1d → `479416c9086d91de4484dbbd2cc5f912cdcdb697` |
| 实际image ID | `sha256:85f9d6c052298e7f52e7d7c4533e131ce19326d461521378e90c6df3b01fa49c` |
| 新APP容器ID | `44cffa11c288c5f7b8a22cbd3ccd432d85fd560253016329c692bbab3c3aba2e` |
| OCI revision | 完整目标SHA一致 |
| 业务版本API | `commit_id=479416c90` |
| Frontend | 保持1ba24ef5f，容器ID `7b624ec2ee904cb7a0ab86458954722bae831f20f1f3c9da54c7ca3f5e53c448` 未变化 |
| 数据库 | 3027，dirty=false，连接目标未改变 |
| 端口 | APP8080 healthy且HTTP200，Frontend80 HTTP200 |
| 其他服务 | PostgreSQL、Redis、DocReader及原停止Mock容器元数据、挂载与端口均未变化 |
| 回滚 | 未触发；旧镜像与APP-only回滚pin保留 |

维护中断窗口：北京时间10:49:53.829794—10:50:06.598700，约12.77秒；完成回归10:50:27，最终只读复核10:50:56。时间来自UTC收据转换，不作为SID请求时间。

使用候选现有镜像，没有拉取、构建或数据库迁移，没有切换旧库或改动原文件卷。本轮没有真实SID登录、员工绑定或企业Session创建，也没有访问生产服务器。

## 发布前核查、备份及Compose顺序

重新确认原035f35e1d、Frontend1ba、DB3027clean、候选镜像与完整OCI revision、原数据库及身份记录基线。首次只读脚本误将未设置的false默认值要求为字面false而停止；修正检查后通过，失败阶段未修改运行服务。

新备份目录：

`/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05/private/prompt-login-app-upgrade-backups/20261016T024953Z`

保存9份配置/Compose/pin来源、APP等容器元数据及挂载配置材料，目录0700、受保护文件0600。停止APP写入后制作 `current-db-3027.dump`，377,583字节，0600，pg_restore目录解析通过；这不是另一次完整恢复演练。原镜像、数据库及全部数据卷保留。

严格保留原4层Compose并最后追加候选：

1. `knowhub-sourceid-business-test-db1b2cd05/compose.yml`
2. `knowhub-sourceid-phaseb-preflight-431226c3b/candidate-image-pin.yml`
3. `knowhub-oa-ui-candidate-1ba24ef5f/frontend-ui-pin.yml`
4. `knowhub-allroles-035f35e1d/app-release-pin.yml`
5. `knowhub-prompt-login-479416c90/prompt-login-app-pin.yml`

以上均位于 `/home/aqyfzx/ai/`。项目 `knowhub-sourceid-business`，project-directory维持原目录。仅执行APP服务 `up -d --no-deps --no-build --pull never --force-recreate app`。检查实际镜像、OCI revision与业务版本，避免旧pin覆盖导致假升级。

候选pin中的强制选项从默认false的插值明确固定为false，并显式加入authorization_recheck_confirmed=false；未编辑基础Compose或受保护env原文件。其他来源文件字节保持一致，APP数据库目标、挂载及原账号权限保持一致。

## 运行门禁

| 配置 | 升级后实测 |
| --- | --- |
| SOURCEID_FORCE_REAUTH_PROMPT_LOGIN | false，新增功能未启用 |
| SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED | false，原未设置、默认false，现显式false |
| SOURCEID_PROTOCOL_ONLY | true |
| SOURCEID_CONTRACT_CONFIRMED | false |
| SOURCEID_EMAIL_BINDING_ENABLE | false |
| SOURCEID_EMAIL_CONTRACT_CONFIRMED | false |
| SOURCEID_AUTO_BINDING_ENABLE | false |
| SOURCEID_SESSION_MODE | NOT_SET，仍为默认external |
| SOURCEID_LOCAL_REFRESH_ENABLE | NOT_SET，保留原状态 |
| SOURCEID_LOGOUT_ENABLE | false |
| SOURCEID_BASIC_ENCODING | oauth_form |
| OIDC_AUTH_ENABLE / SOURCEID_DIAGNOSTICS_ENABLE | true / true，保留升级前现场状态 |
| OIDC_AUTH_PKCE_ENABLE / AUTO_MIGRATE | false / false |

不是将所有OIDC开关都关闭，而是按本次授权保留原现场并明确关闭新功能及企业业务门禁。数据库用户2、租户1、空间成员2，external_identities、绑定审核、企业Session和local Session均0；原用户权限选定字段一致。

## 部署后实际回归

使用既有合成账号，通过实际Frontend代理和新版APP业务接口断言，不使用真实员工凭据。

| 验收 | 结果及范围 |
| --- | --- |
| 本地邮箱登录 / auth/me | 200，登录success，随后退出令牌失效 |
| 系统版本 | 200，commit_id准确匹配479416c90 |
| 知识库 / agents / models基础API | 200 |
| 合成KB创建/读取 | 201/200，读取ID与创建ID一致；未登录读取401 |
| MCP基础管理API | 200 |
| MCP协议 | initialize通过；tools/list恰好3项；list_knowledge_bases、list_documents、wiki_search均返回有效结果、无工具错误 |
| 无效MCP令牌 | 401，安全拒绝 |
| 飞书个人配置及文档列表 | 200 |
| 飞书管理员配置 | 当前非系统管理员合成账号403，预期权限拒绝；没有修改真实飞书配置 |
| OIDC配置代理 | 200，原enabled=true、oauth_form及批准callback不变 |
| 无state callback | 302且invalid_state，不兑换Token、不发起SID授权 |
| Frontend兼容 | 旧Frontend1ba与新APP API正常，前端容器未变 |
| 合成资源清理 | KB及MCP删除200，活动KB和MCP endpoint回到0；保留正常软删除/审计语义，不清理历史数据 |
| 退出 | logout200，原本地Token随后auth/me401 |
| 日志脱敏 | 仅在远端进程内扫描本轮APP5166字节、Frontend3839字节日志，未发现敏感格式或合成测试凭据；未保存原始日志 |

日志扫描有本轮时段及条数边界，不宣称审计了所有历史日志。非SourceID Provider不受影响的结论依据同一479416c90已通过的CI/合成回归；本次没有切换真实Provider或进行其他Provider真实认证。完整RAG、模型调用及真实OA业务放行不属于本轮回归证据。

## APP-only回滚能力

旧APP镜像仍为：

`sha256:2d91336c3f5ba1ba330820801c7bf0573b1c9c45b9280994dd0862d016435286`

回滚pin：`/home/aqyfzx/ai/knowhub-prompt-login-479416c90/prompt-login-app-rollback-pin.yml`。

需要回滚时：关闭后续试验入口，保留故障安全证据；恢复备份中的原APP配置，保留Compose前4层，将第五层替换为rollback pin，仅重建APP。候选pin不同时传入，避免遗留新选项覆盖。复核035f35e1d、auth/me、KB/MCP和原门禁。数据库保持3027，不执行down、切库、数据卷删除或整体服务栈回滚，不恢复raw。

本轮没有触发回滚，也没有为了演练再次中断服务。回滚镜像及文件已核对存在；自动异常处理路径仅恢复APP，实际回滚未执行，不能报告为“实测回滚成功”。

## 下一阶段操作卡

已另备 [单次真实SID协议联调操作卡](SOURCEID_PROMPT_LOGIN_REAL_PROTOCOL_OPERATION_CARD.md)，**未执行**。

最小准备：另获一次真实事务授权；受控确认浏览器页面/API/callback全部到打包机并绕过代理误路由；仅临时开启强制认证选项，protocol_only及身份写入门禁继续保持；SID负责人核对已有SSO仍重新认证；一次新Code只兑换一次；继续签名、nonce及UserInfo校验；auth_time只保存存在性和固定分类。最终预期停在identity_contract_pending，不建立企业会话。

SID Code90秒、停用前Code仍可兑换的残余风险及D未知仍保留。10分钟会话未获批准；local_approved和自动绑定未启用。不得把本次健康回归当作真实SID200或强制认证效果证明。

## 机器证据

- [重新核查现场](evidence/prompt-login-app-deploy-precheck-20261016.json)
- [实际部署、备份与业务回归](evidence/prompt-login-app-deployment-20261016.json)
- [部署后只读复核](evidence/prompt-login-app-deployment-closeout-20261016.json)
- [目标版本四项CI](evidence/prompt-login-ci-20261016.json)

已完成本次授权范围，保持现状，等待真实协议联调的独立授权。
