# verified_email_policy 测试站点实际部署记录

北京时间2026-10-18；仅172.30.232.42、knowhub-company-test。结果：**迁移成功，APP升级验收中止并回滚；未完成目标版本发布。**

| 核查项目 | 最终实际结果 |
|---|---|
| 80/8080是否运行目标5f988be51 | **否**。候选曾启动healthy、镜像校验通过，但入口验收误判后按方案恢复f40 |
| 当前APP | f40b1602f69ce5d88e1e87dc3cefae36e9bde356；`sha256:742c7ce56ef38ad32548900c8e7f8b264f9f6cd6138043674b91b27557443413` |
| Frontend | 832b6652adf541892545a7eac8b68733f846ab95，容器和镜像未变 |
| DB | **3030，dirty=false**；标准migrate执行up 1，不执行down或force |
| 原数据 | 3用户、5成员关系、1有效KB、3文档、2717非空有效分块及2717 embedding；原UID、管理员/owner、租户成员、ACL、文档和向量引用元数据与备份前一致 |
| 原文件 | 只读成套备份；逐文件内容及长度对照一致，未改文件卷 |
| verified_email_policy | 数据库增量已安装；**新APP实现目前未运行**。当前trusted_ownership、confirmed=false、policy_ref为空 |
| APP-only回滚 | **本次现场实际触发且验证通过**，f40可在DB3030启动并提供基础API；未降迁数据库 |
| 其他服务及网络 | Frontend、PG、Redis、DocReader及其他原运行容器的ID/镜像/卷/端口未变；防火墙规则对照一致；不增加外部数据库/缓存端口 |
| 真实SID/身份写入 | 0次真实SID请求；无新增绑定、ownership、JIT或企业Session |

## 执行与停止原因

执行前镜像、OCI完整revision、原f40/832/3029 clean及Compose顺序均匹配。未消费有效授权事务0、其他活跃数据库查询0、80/8080已建立连接0；未发现当时占用。但这不证明不存在未登记的离线调用方。

停止APP写入后备份数据库及文件。首次备份流程的防火墙快照读取失败，自动恢复旧APP；修正只读工具调用后重新成套备份成功，随后才执行迁移。第一次失败没有执行迁移。

标准迁移3029→3030成功；基础Compose→已准备app-pin.json，仅重建APP，目标镜像为`sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b`，OCI revision为`5f988be51779f1a22bd9d027b7503fe6a8749f62`。未重新构建或拉取镜像。

候选APP健康及镜像检查通过后，部署脚本错误地要求8080的`/login`返回前端页面200。实际既有设计为80→Frontend、8080→APP；后端`/login`未认证返回401，不能套用前端页面验收条件。这个验收脚本错误触发了自动回滚，**没有证据据此判定候选OIDC代码故障**。

按批准的失败停止规则恢复f40 APP及受保护配置，保留DB3030。已修正后续脚本：登录页面及静态资源仅检查80；80/8080均检查health、配置API和无参数callback。**修正后未再次部署**，不能将它当作新版本现场PASS。

## 备份与回滚证据

受保护备份目录：`/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51/private/deployment-20261017T171954Z`（目录名UTC）。数据库dump 21,706,942字节，配套文件tar 2,570,240字节。包含基础/有效Compose及顺序、APP运行配置、容器元数据、原镜像信息、APP候选/回滚pin、网关原配置和私有数据/文件/网络基线；目录0700、文件0600。原凭据不导出到Git、报告或对话。

实际回滚使用基础Compose→app-rollback-pin.json，仅恢复APP；Frontend、DB3030和全部卷保留，Basic仍oauth_form。网关上游未改变，环境标记已如实更新为`company-test-f40-db3030`，避免旧标记误称DB3029。

## 回滚后基础验收

- 80登录页和静态资源200；80/8080 health及OIDC配置200，无参数callback302。
- auth/me、知识库、飞书管理员/个人及MCP探针未携带凭据，均401，未发生匿名放行。
- OIDC=false、protocol_only=true、contract/email/auto/JIT=false、Session external、Refresh=false、prompt=false、AUTO_MIGRATE=false、诊断false。无虚假政策引用。
- 原用户权限、ACL、文件及向量引用逐项对照通过；新增政策决策/会话元数据为空。
- **未执行**真实密码登录、有效MCP Key业务、飞书授权/同步、真实原文检索或完整RAG，不将401接口保护检查称为完整业务回归。飞书/模型等外部业务配置未在本轮恢复或重新授权。
- 候选的隔离认证、Session、JIT与浏览器Mock回归沿用已通过的四项CI和43项采集器校准；本轮现场在验收中止后未继续运行新业务Mock，不伪称现场新APP完整验收通过。

脱敏收据：`evidence/verified-email-deployment-*-20261018.json`及`verified-email-rollback-label-20261018.json`。日志不含Token、Code、Cookie、员工身份或完整认证URL。

## 下一步

需要确认一次**修正验收脚本后的APP-only重试**：当前已是DB3030，不再执行迁移、不更换候选镜像、不替换Frontend。重新核查现场与最新备份，使用同一基础Compose→app-pin.json；真实门禁继续关闭。此前“从DB3029迁移”的预检不能直接重跑。

候选实际部署并验收成功后，首次真实OA登录仍分别需要：公司级可信邮箱自动关联政策及真实引用（覆盖全部角色和未知历史邮箱转让残余风险）；普通SSO/本地Access-only会话期限（60/60/30/60分钟仍为候选）及风险责任人；A正向验收范围和一笔真实SID登录/关联/Session写入窗口授权。JIT保持关闭，B跨空间负向未执行不得标PASS。新模式不要求逐人ownership JSON，不填EnterpriseMaxAge或伪造valid_until。

本轮已停止，不自动重试、不开启真实认证或业务门禁，不操作生产。
