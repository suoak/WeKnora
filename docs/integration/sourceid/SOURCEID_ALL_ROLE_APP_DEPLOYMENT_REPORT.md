# 全角色 OA 关联 APP-only 打包机升级报告

日期：2026-10-16。目标：`172.30.232.42`，项目 `knowhub-sourceid-business`。

**实际完成 APP-only 升级及回归，结果 PASS；未回滚、未迁移、未切库、未开启企业身份放行。** 没有访问生产服务器，没有发起真实SID登录或写入真实身份绑定。

## 1. 实际运行版本

| 服务 | 升级前 | 升级后 |
| --- | --- | --- |
| APP | 431226c3b | **035f35e1d** |
| Frontend | 1ba24ef5f | **1ba24ef5f，容器未变化** |
| 数据库 | 3027 / dirty=false | **3027 / dirty=false，同一数据库** |
| APP端口 | 8080 | 8080，healthy |
| Frontend端口 | 80 | 80，HTTP200 |

APP目标完整OCI revision：`035f35e1d388a2f02615d35cf826bbdcc22e56b0`。实际 `/api/v1/system/info` 返回 `commit_id=035f35e1d`，不是仅依据Compose标签判断。

新APP image：`sha256:2d91336c3f5ba1ba330820801c7bf0573b1c9c45b9280994dd0862d016435286`。

新APP container：`842f61a9825fe013dfbd87285cfc6846c65bd086131bf26dbdcbd192c13c4e78`。

Frontend image：`sha256:63146a134fc3be75b663a1ac6ddba205be2b0a3c881891a3ba29a28d6a04f48a`。

Frontend container：`7b624ec2ee904cb7a0ab86458954722bae831f20f1f3c9da54c7ca3f5e53c448`，保持不变。APP/Frontend revision不同，是经本轮API回归确认的兼容组合。

复用既有已通过CI和隔离验收的不可变镜像，没有重新构建或拉取其他版本。[功能CI](https://github.com/suoak/WeKnora/actions/runs/38828530863)仍对应目标代码提交。

## 2. 发布时间与最新备份

北京时间：

- 08:49:10：现场版本、门禁、Compose解析及候选镜像核查PASS。
- 08:51:50.589527：仅停止APP，保持Frontend及配套服务运行，创建最新DB快照。
- 08:52:04.101784：新APP健康检查通过，维护过程约13.51秒；不是浏览器精确测得的全部请求中断时长。
- 08:52:24：升级与业务回归完成。
- 08:53:44：最终只读核查PASS。

受保护备份目录：

`/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05/private/allroles-app-upgrade-backups/20261016T005150Z`

备份当前三层Compose、APP/Frontend引用的env文件及必要挂载配置。6份Compose/env配置源有内容一致性比较，另保存挂载CA文件和安全容器元数据；没有修改其原文件。目录0700、文件0600，不下载或输出配置中的凭据。

最新 `current-db-3027.dump` 为376,672字节，APP停止写入后制作的 `pg_dump -Fc --no-owner --no-acl`。已用pg_restore解析目录验证；本轮没有另建恢复库重做恢复演练，不能将目录解析写成实际恢复测试。既有原APP镜像仍可用，未prune或删除卷。

## 3. Compose覆盖与实际操作

按顺序解析四个文件：

1. `/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05/compose.yml`
2. `/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b/candidate-image-pin.yml`
3. `/home/aqyfzx/ai/knowhub-oa-ui-candidate-1ba24ef5f/frontend-ui-pin.yml`
4. `/home/aqyfzx/ai/knowhub-allroles-035f35e1d/app-release-pin.yml`

第四层只声明APP的image与pull_policy=never；没有覆盖其他服务或认证环境。实际执行仅 `up -d --no-deps --no-build --pull never --force-recreate app`。

原APP容器被替换。Frontend、PostgreSQL、Redis、DocReader及停止状态的原Mock容器，其ID、镜像、运行状态、端口和挂载均前后相同。APP文件卷、DocReader只读卷及原挂载也相同；没有创建替代数据库卷，没有修改默认DB目标。

其他Compose源及env文件在部署前后逐字一致。私有进程比较APP实际DB_HOST/PORT/NAME/USER/DRIVER，结果一致，未导出其值。

没有执行migrate、数据库版本force、down -v、全局prune、Frontend重启或原库复用。

## 4. 部署后实际回归

使用已有合成本地owner，不读取或使用员工SID凭据；以下不是一次真实OIDC登录：

| 验收项 | 实际结果 |
| --- | --- |
| APP8080 /health，Frontend80 | 200 |
| 本地邮箱密码登录及auth/me | 200，原账号 |
| system/info实际二进制commit | 035f35e1d |
| 知识库、agents、models基础列表 | 200 |
| 合成KB创建/读取 | 201 / 200 |
| 未登录读取该真实存在的合成KB | 401 |
| MCP基础列表/工具目录 | 200 |
| 临时MCP endpoint initialize、tools/list | PASS，3个配置工具 |
| MCP list_knowledge_bases、list_documents、wiki_search | PASS |
| MCP错误token | 401 |
| 飞书个人状态、合成KB个人文档列表 | 200 |
| 飞书系统管理员配置 | 普通owner请求403，符合权限预期；未使用系统管理员写配置 |
| OIDC config经原Frontend代理 | 200，oauth_form及已审批callback不变 |
| 无state的callback | 302 + invalid_state，无Token兑换 |
| 本地退出，随后旧token访问auth/me | 200退出，旧token401 |

合成MCP endpoint和KB均通过业务接口删除，临时本地登录令牌退出后失效。仅留下正常审计/软删除历史，没有新增员工账号、企业绑定或企业Session。

最终当前库：users2、tenants1、members2；external_identities/reviews/SourceID Session/local Session均0。有效KB和有效MCP endpoint均0，测试夹具没有保留为有效业务资源。原用户身份/租户/管理标记及成员关系私有比较不变。

回归只覆盖基础API及无模型合成检索，没有宣称当前空验收库具备原WeKnora知识库、已完成真实OA或完整RAG。当前APP仍连接原验收库，旧79数据未接入。

## 5. 最终安全配置

| 配置 | 实际状态 |
| --- | --- |
| SOURCEID_BASIC_ENCODING | oauth_form |
| SOURCEID_PROTOCOL_ONLY | true |
| SOURCEID_CONTRACT_CONFIRMED | false |
| SOURCEID_EMAIL_BINDING_ENABLE | false |
| SOURCEID_EMAIL_CONTRACT_CONFIRMED | false |
| SOURCEID_AUTO_BINDING_ENABLE | false |
| SOURCEID_SESSION_MODE | 未设置，默认external |
| SOURCEID_LOGOUT_ENABLE | false |
| AUTO_MIGRATE | false |
| OIDC_AUTH_ENABLE | true，与升级前一致 |
| SOURCEID_DIAGNOSTICS_ENABLE | true，与升级前一致 |
| OIDC_AUTH_PKCE_ENABLE | false，与升级前一致 |

其他配置源、callback、凭据及协议参数未修改；没有为了验证功能关闭PROTOCOL_ONLY或启用local_approved。OA入口可见不代表真实账号自动关联已经放行。

只在远端内存扫描本轮时间窗内有界APP/Frontend日志，未发现本次合成密码/token或完整Authorization/JWT/原始SID code/access token模式；只导出扫描结论和字节数。该检查不证明全部历史日志永无敏感信息，也不输出原日志。[完整脱敏升级收据](evidence/allroles-app-deployment-20261016.json)。

## 6. 异常与回滚准备

首个前置检查工具错误假设组织成员表存在单列id，查询失败发生在停止服务之前；工具改为按实际列读取，重新核查后继续。没有现场版本/门禁不符，也未强制忽略这些门禁。[早期工具收据](evidence/allroles-app-deployment-precheck-attempt1-20261016.json)。

实际升级和回归全部通过，**没有触发回滚**。

APP-only回滚pin：`/home/aqyfzx/ai/knowhub-allroles-035f35e1d/app-rollback-pin.yml`，仅固定旧APP image `sha256:b2751ba9c8797b0c2440b9435ad92ba9ab2c30d4a95b0c28a2b1b34e4a4c3af1`。回滚时保留前三层Compose，将最后一层替换成此rollback pin，仍只recreate APP：

```sh
docker compose --project-name knowhub-sourceid-business \
  --project-directory /home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05 \
  -f /home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05/compose.yml \
  -f /home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b/candidate-image-pin.yml \
  -f /home/aqyfzx/ai/knowhub-oa-ui-candidate-1ba24ef5f/frontend-ui-pin.yml \
  -f /home/aqyfzx/ai/knowhub-allroles-035f35e1d/app-rollback-pin.yml \
  up -d --no-deps --no-build --pull never --force-recreate app
```

此命令不改环境，不恢复raw，不切数据库，不替换Frontend，不降迁3027。本轮未为证明回滚而无故再切换APP。

## 7. 旧库与后续授权

已另行形成[干净原库复用待审批方案](SOURCEID_LEGACY_DATABASE_CLEAN_REUSE_PLAN.md)，覆盖干净快照、资源句柄与文件卷、权限一致性、旧凭据/会话失效、获批模型完整RAG、保留当前3027库及维护窗口/写冻结/回退。

本轮没有执行该方案，也没有使用带Mock身份的旧隔离副本作为默认业务库。真实OA自动关联及local_approved放行仍需独立批准。本次工作在APP-only升级和报告交付后停止。

证据：[发布前](evidence/allroles-app-deploy-precheck-20261016.json)、[升级与业务回归](evidence/allroles-app-deployment-20261016.json)、[最终只读核验](evidence/allroles-app-deployment-closeout-20261016.json)。
