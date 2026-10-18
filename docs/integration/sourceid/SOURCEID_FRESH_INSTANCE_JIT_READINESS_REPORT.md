# 全新实例首次真实 JIT 开户准备

日期：2026-10-18。结论：**隔离环境和 Mock 已准备完成；等待真实 JIT 政策、入口切换及单笔认证授权。**

本次验证范围是“同一 SID 身份在无历史账号的全新 KnowHub 实例首次开户”，
不是该员工在公司第一次入职，也不是全公司真实新员工准入已经验收。
不得删除原管理员绑定来制造新员工，不以隔离实例的新账号替换原账号。

## 1. 原环境保留与备份

- 172.30.232.42，`knowhub-company-test`，80/8080 入口未切换。
- APP `5f988be51779f1a22bd9d027b7503fe6a8749f62`，镜像
  `sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b`。
- Frontend `832b6652a`，镜像
  `sha256:ab7845c89fadeb17ad2b68bd924e892f948b893781a50ab79e48385c319ddbe1`。
- 原 DB3030 clean；3 用户、1 外部身份绑定、0 JIT、3 原文档、2717 有效分块及2717向量引用保留。
- 原用户、租户/成员、知识库/文档、绑定审核、Session和关联决策表与开始时的受保护基线一致。
  原容器 ID、镜像、挂载、端口及网关配置未变化；文件逐项比对备份清单一致。
- 未删除原账号、绑定、会话审计、知识库或文件，未写入原库。

受保护备份目录：
`/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018/private/original-backup`。
保存 DB3030 dump（21,721,873 字节）、文件 tar（2,570,240 字节）、完整 Compose 层顺序及副本、
运行配置/容器元数据、原网关配置、数据基线及文件清单。目录700、文件600，不进入Git/报告正文。
DB为pg_dump自身事务一致快照；文件备份前后双扫描及数据基线稳定，原应用没有停机。
这不是存储层跨库/文件原子快照；未来真实切换窗口仍应重新冻结写入并保存最新成套基线。

## 2. 独立空白实例

| 项目 | 实际结果 |
|---|---|
| 项目 | `knowhub-jit-fresh` |
| 配置目录 | `/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018` |
| 数据库 | 独立PG容器，`knowhub_jit_fresh`，独立随机数据库凭据 |
| 迁移 | 标准 migrate，从空库完整执行至3030，dirty=false；无 force、跳版本或旧库恢复 |
| 镜像 | 与原测试站点同一已验收APP/Frontend不可变镜像；未构建、未拉取 |
| Redis/文件 | 独立Redis及密码、`knowhub-jit-fresh_redis`、`knowhub-jit-fresh_files` |
| 数据卷 | 独立 `knowhub-jit-fresh_postgres`、文档临时卷；不挂载原文件卷 |
| JWT/AES | 独立随机值；受控进程确认不同于原站点，不输出明文或哈希 |
| 原身份状态 | users、external identities、绑定审核、ownership、JIT、身份block、Session、auth_tokens均0 |
| 默认权限 | tenant_members、知识库均0，没有初始化个人owner账号 |
| 门禁 | OIDC=false、protocol_only=true、contract=false、自动绑定=false、JIT=false、Refresh=false |
| 端口 | 所有新服务无主机发布端口，IPv6关闭，PG/Redis不对外暴露 |

空白用户/身份表足以证明任何员工邮箱及RJGH/sub记录均不存在，不需要读取用户真实邮箱或工号。
必要初始化由完整迁移和应用正常启动完成，没有通过插入管理员绕过空库状态。
新APP到独立Redis的AUTH/PING实测PASS；修正了新Compose的`$$`环境变量转义，未修改原Redis。
最终原80/8080 health均200，均返回`company-test-verified-email-db3030`，不是新JIT环境标记。

首次迁移启动命令未成功建立迁移表，工具检查后再次用标准迁移成功；未force或手写版本。
新Compose补齐显式HTTP健康检查后APP healthy。前述准备问题仅发生在新环境，不影响原站点。

## 3. 隔离 Mock 结果

另建 `jit_mock_new` / `jit_mock_existing` 合成数据库，真实待测 `knowhub_jit_fresh` 始终保持空白。
Mock APP、Redis、TLS SID及Frontend使用临时容器，结束后全部移除；合成数据未写回原库/待测库。
TLS使用显式信任的合成CA；没有关闭TLS验证。仅Mock容器允许127.0.0.1端点通过SSRF检查。
Mock政策引用均明确为合成测试用途，未加载到真实待测APP。

- 新用户：两次合成OIDC登录均通过；首次创建1用户、1绑定、1 JIT记录，第二次仍同user_id。
- 新用户tenant_id=0，系统管理员/跨租户标记false，无成员关系、无知识库；切换未获授权空间403。
- 已有用户：经正常注册API准备的合成tenantless账号，两次OA登录保持同user_id及标记，
  总用户/绑定各1、JIT记录0。此夹具是普通合成账号，不冒称重新实测原管理员登录。
- 两类auth/me成功、无Refresh字段/Cookie/数据库记录；本地退出后旧Access请求401。
- 结束后两类有效Session/Access均0；两次重复登录仅属Mock，不增加真实认证授权笔数。
- Frontend静态登录壳正常。**实际浏览器Portal渲染 NOT_EXECUTED**，留待获批真实人工验收。
- 原管理员及成功绑定在原库中保持不变，由只读前后基线证明；本轮未重新登录原管理员。

Mock夹具修正了空库public schema恢复冲突、Mock回环SSRF白名单及随机注册密码超过现有32字符
上限的问题，未修改业务代码或降低正式密码、OIDC/TLS要求。失败合成库仅在该新PG内定向重建。

## 4. SID连通性与路由准备

新APP使用独立网络命名空间及复制的最小SID出站守卫，未改变原守卫或全局防火墙。
DNS通过，官方TLS证书/主机名验证通过（TLS1.3），Discovery200、JWKS200且有RSA密钥。
没有调用官方authorize、Token、UserInfo。无关公网及172.30.232.42的80/8080出站探测被拒绝。

新实例内部实际验证：/login200、静态asset200、OIDC配置200且enabled=false、
无参数callback302/invalid_state。原已批准callback保持不变。

原网关是HTTP代理，80使用`set $upstream frontend:80`，8080使用`app:8080`。
已生成但未加载：
`private/route/candidate.conf`、`private/route/original.conf`。
候选仅把两个upstream改成`jit-fresh-frontend:80`、`jit-fresh-app:8080`，
并将环境标记改成`jit-fresh-isolated-db3030`；原来源限制及其他HTTP行为保留。
新Frontend/网络命名空间已有对应别名。候选nginx -t PASS。
**原网关尚未连接新网络，80/8080、现有Chrome和生产DNS均未改动。**
目前不把候选语法检查和内部探测冒称原地址已指向新实例。

## 5. 后续须批准的单笔真实窗口

### 批准项

1. 本人同SID身份在新实例首次JIT；允许新库写入1个无特权用户、绑定、JIT审计及有期限Session。
2. JIT可信公司SID企业身份政策及真实批准引用；本次精确邮箱域建议`ruijie.com.cn`，需明确确认。
   单域测试不代表公司全部邮箱域或所有SID账号类型已经确认。
3. 60m Access / 60m Absolute / 30m Idle / 60m Admission、无Refresh及普通SSO残余风险接受，
   记录本次真实批准，不复用Synthetic引用，不将原账号关联批准默认为JIT开户批准。
4. 测试机原80/8080短时独占维护窗口、临时代理切换、失败/超时恢复及一笔SID事务。

### 执行顺序（本轮未执行）

- 重新确认原站点零有效授权事务、无其他认证测试；保存最新原配置及DB/文件成套备份。
- 核对新库仍空白、无Mock连接，锁定项目/镜像/DB名及独立密钥；没有原库连接或文件挂载。
- 受控进程仅向新环境加载现有有效SID配置/凭据及真实获批JIT/Session政策。
  开启新环境企业业务路径；保持prompt=false、Access-only、Refresh=false，不改callback。
  暴露入口前关闭新实例公开密码注册，防止无关注册创建owner；不得将该设置写回原站点。
- 开启只监测该新实例的白名单采集，区分授权与结果交接；唯一关联不明则NOT_CAPTURED，
  不冒称PASS；协议安全失败停止，不自动第二次登录。
- 临时维护阻止其他测试流量并清理/排空旧HTTP连接，避免旧worker/keepalive跨越切换。
  将**网关**临时接入`knowhub-jit-fresh_isolated`，核实唯一别名解析到新APP/Frontend，
  不将原APP或原PG/Redis接入新网络。
- 对原挂载nginx.conf按原inode受控写入候选，nginx -t后reload；配置失败立即恢复原文件。
  在无凭据阶段核对80页面/静态资源/API、8080/API、无参数callback、环境标记及新DB。
  不能用仅浏览器hosts或仅镜像标签当作路由证据。SID继续访问官方HTTPS。
- 排空后才READY，用户在原地址点一次OA登录，完成认证并返回KnowHub。
  看到SID页面不算结束；授权等待窗口上限8分钟且不超过本地事务TTL，Code90秒且只兑换一次。
- 预期新user_id与原实例不同，tenantless，无管理员/跨租户/空间/知识库权限；auth/me返回新账号，
  Portal正常呈现无授权状态；1条绑定、1条JIT审计，无Refresh。不能以空资源404证明跨空间拒绝。
- 用户本地退出，确认该新Session和派生Access撤销；再次真实登录需另行授权。

### 强制恢复及核验

- 先保存脱敏证据，再关闭新实例真实业务开关并定向撤销本次新Session；不删用户/绑定/JIT审计。
- 恢复网关原文件，nginx -t/reload并排空连接；确认80和8080同时恢复
  `company-test-verified-email-db3030`、原APP/Frontend及原DB。
- 网关断开仅本次新增的新网络连接，保持原网络/来源规则/端口/容器和旧配置层不变。
- 对比原user_id、管理员/owner/成员/ACL基线、原绑定和审计、知识库3文档及2717分块/向量、文件。
  核实原APP仍连接原PG/Redis，原JWT/AES没有变化；新库写入数和来源审计单独留存。
- 从不将新账号、Redis、Session、绑定或审计回灌原库，不降迁、不清空原数据，不操作生产。
- 硬超时、安全失败或取消均停止，不重试、不延长旧Code/事务。看见密码页面不是结束信号。
  网关恢复失败必须明确告警和人工恢复，不能只凭脚本退出声称恢复成功。

## 6. 本轮实际结论

准备 **READY，真实测试 NOT_AUTHORIZED / NOT_EXECUTED**。
正式站点及原数据保持原状；JIT待测环境空白且业务门禁关闭；Mock PASS；
路由候选PREPARED_NOT_SWITCHED。新环境真实Portal、实际JIT开户、真实退出、实际切换/恢复
尚未执行。公司级真实新员工准入整体及全公司发布条件仍未因此通过。

收据位于服务器该目录的`prepare-result.json`、`mock-result.json`、`final-audit.json`，
以及本机`jit-fresh-complete.json`、`jit-fresh-mock-status.json`、
`jit-fresh-network-route-prepare.json`、`jit-fresh-egress-readonly.json`、`jit-fresh-final-audit.json`。
只有安全计数/分类/配置元数据进入普通报告，身份材料及密钥不进入Git、CI或对话。
