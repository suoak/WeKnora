# verified_email_policy 实现与验证

功能基线f40b1602f；候选源码 `5f988be51779f1a22bd9d027b7503fe6a8749f62`。本轮只开发、隔离验证和候选准备；没有替换80/8080、执行测试站点迁移、启用门禁或真实SID认证。最终测试/CI和镜像收据附在本报告末尾。

## 已实现

| 范围 | 代码及实际行为 |
|---|---|
| 配置 | `internal/config/sourceid_enterprise.go`、`config.go`：ACCOUNT_LINK_MODE默认trusted_ownership；verified_email_policy需enterprise_all、明确CONFIRMED与非空真实POLICY_REF；保留SSO/local/email/auto等批准，不接受未知模式 |
| 服务链 | `service/sourceid.go`、`sourceid_verified_email.go`：只在严格OIDC及普通SSO准入通过后调用新事务入口；无prompt/auth_time强制要求；旧模式保持原入口 |
| 首次账号关联 | `repository/sourceid_verified_email.go`：规范化邮箱、包含软删除的唯一历史匹配、active及已知冲突检查；全部角色走同一方法，不更改用户/权限 |
| 原子事务 | 绑定review、enrollment、external identity、政策决策、local Session及模式标记一起提交；失败全部回滚，不留下pending绑定或JIT用户 |
| 并发 | PostgreSQL统一users短表锁→review→session顺序，与JIT及directory ownership同步协调；现有审计/唯一约束继续生效。密码注册/邮箱写入受数据库表锁约束，不只靠新流程互相合作 |
| 持久化政策 | 3030仅新增link_decisions、local_link_policies、link_blocks及邮箱规范化查询索引；不修改用户/角色/ACL列，不填造ownership行或证明时间 |
| 撤销和Access | `sourceid_local_session.go`：持久化模式、邮箱政策版本、自动绑定版本与当前配置一致；复查approved绑定、账号、负向记录及原会话边界。版本/模式不符时拒绝并定向撤销，不因恢复旧配置复活已失效Access |
| 已知风险 | 拦截既有归属disabled/过期/冲突、review撤销或人工hold、不同RJGH/sub/user、已知邮箱转让的负向记录；RecordSourceIDLinkBlock为内部受控运维方法，无新增公开API，记录原因/负责人/引用并撤销相关Session及派生Access |
| JIT | 复用tenantless注册及原JIT审计。无任何历史账号/身份/邮箱痕迹才可在独立JIT批准和精确公司域规则下创建无特权用户；失败不能JIT兜底；不生成旧账号ownership证明 |
| Access-only | 不返回/持久化Refresh；SwitchTenant使用同一有效Session及原成员权限，不延长绝对期限；本地退出、绑定撤销、停用、过期继续拒绝 |

新政策决策固定类别为 `first_email_match` / `existing_binding` / `jit`；自动审核actor明确为 `company-verified-email-policy`。真实政策引用存审计；这不是人工独立历史归属证明。对已存在的可信长期绑定优先核验issuer+RJGH+sub，邮箱变化不会迁移到另一user_id。

## 默认和审批

新增配置在 `.env.example` 中仅作为注释：

```dotenv
SOURCEID_ACCOUNT_LINK_MODE=trusted_ownership
SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED=false
SOURCEID_VERIFIED_EMAIL_POLICY_REF=
```

新模式不要求逐人ownership JSON、EnterpriseMaxAge或valid_until，不伪装核验时间。trusted_ownership保持原归属新鲜度和审核规则。任何人不能通过浏览器参数声明政策批准；配置由服务器受保护运行环境控制，非空引用是配置校验，不是程序验证外部签字真实性，真实审批须运维核对。

政策须覆盖公司全体适用账号及所有管理角色、可信SID邮箱来源、邮箱回收/分配规则、未记录历史转让风险、异常处置、会话期限及责任人。已知异常仍拒绝。未记录邮箱重分配无法由当前SID邮箱唯一匹配识别，不能宣称技术上已消除；不要求每位员工逐人审批。

60/60/30/60分钟仍为候选，不是本轮批准值。普通SID SSO不是实时员工状态复核；停用风险不能仅声称最多60分钟，其边界还取决于已缓存SID SSO会话、传播D及停用前Code90秒有效期。本地退出不宣称统一退出SID。

## 验证范围及限制

新增配置、仓储及真实HTTP测试覆盖默认拒绝、原模式、全部角色、首次/重复、邮箱变化、RJGH/sub冲突、软删除及重复邮箱、停用、已知转让、无JIT兜底、事务故障、12路并发、JIT竞争、非合作邮箱写入、目录停用/绑定撤销竞争、政策切换、失效、无Refresh及SwitchTenant。

浏览器测试复用真实Vue与真实后端auth handler/service/repository、隔离Mock SID。普通账号、owner、系统管理员、跨空间及多空间、JIT均有明确业务断言。Portal其他API为限定夹具；不是实际原知识库完整RAG或真实SID认证。

隔离DB迁移从当前公司测试DB3029的一致性dump恢复到无网络的独立临时PG，只对副本标准migrate up 1到3030。对照原UID/管理标记/成员/KB/文档/ACL与向量引用元数据，不输出员工字段或原文；源库始终3029 clean。该副本没有启动SID或创建真实身份绑定。源文件卷未挂载、未修改，部署时仍须重新创建同代DB/文件备份。

旧APP回滚保留3030新增审计表，禁止执行down；现有3029业务表无破坏性修改。新模式Session必须先撤销，旧f40不应被用来继续放行新策略；保留旧镜像、真实配置和备份是升级批准前的必要条件。

## 修正记录

隔离验证发现并修正了重复登录时GORM携带冲突插入UUID导致审计行查找失败的问题；并发写入夹具改为调用真实tenantless用户仓储，避免测试自身写tenant_id=0触发外键错误。副本恢复就绪检查改为等待最终PG TCP服务，避免临时initdb服务假就绪。均未触碰运行测试站点。

部署及真实窗口分别见[升级计划](SOURCEID_VERIFIED_EMAIL_TEST_SITE_UPGRADE_PLAN.md)和[首次真实操作卡](SOURCEID_VERIFIED_EMAIL_FIRST_REAL_LOGIN_CARD.md)。生产操作、测试站点替换、真实身份写入与SID登录均未获本轮授权。

## 最终验证与候选收据（2026-10-18归档）

- APP源码：`5f988be51779f1a22bd9d027b7503fe6a8749f62`，已推送开发分支 `feature/knowhub-sourceid-enterprise-jit`。
- [GitHub CI 39045640602](https://github.com/suoak/WeKnora/actions/runs/39045640602)：Linux、PostgreSQL、Frontend、Browser全部成功。包含race、真实PG事务、迁移治理、认证/MCP/飞书回归、类型检查、前端构建及真实Vue + Mock SID业务浏览器测试。浏览器覆盖viewer、contributor、owner、tenant_admin、org_admin、system_admin、cross_space_admin、multi_space和JIT。
- 新模式覆盖12路首次绑定/JIT并发、事务失败回滚、撤销竞争、策略变化后Access失效、SwitchTenant与无Refresh；不是新增真实SID或生产容量测试。
- 隔离离线Go测试收据对应`728406a6d`（随后只修正浏览器夹具）；最终完整源码验证以以上CI为准，不能将中间提交测试冒充最终提交收据。
- DB3029一致性副本标准迁移至3030 clean，保留型down/up通过；3用户、2个KB记录（含历史）、3文档、2717非空分块及2717 embedding一致，原用户/管理员/成员/ACL和向量引用元数据一致。源DB保持3029 clean，原文件卷未挂载、未写入；没有新增原文读取或完整RAG实测。
- 候选业务采集器固定新APP镜像/3030及company-test，Windows/Linux各43项Mock通过；另28项原协议控制兼容测试在Windows通过。A_positive须明确批准，B未执行不会记PASS。未启动真实worker。

候选镜像：`sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b`，OCI revision为上述完整源码SHA，离线Go1.26.7及已验证本地工具链构建，没有拉取或重建运行站点。

远端候选目录：`/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51`，已生成`app-pin.json`与`app-rollback-pin.json`。回滚APP固定原f40镜像；新增策略仍默认trusted_ownership/confirmed=false。APP镜像与Frontend832的revision不同，接口兼容由CI验证，不宣称同一版本。

收据见`evidence/verified-email-*-20261018.json`。当前站点只读核查仍为f40/832/DB3029，80/8080及无参数callback正常、身份写入门禁关闭、无真实绑定或企业Session。**候选尚未部署；现场APP-only恢复及上线后健康验收须在另行批准的部署窗口执行，不能把副本迁移测试称为已完成现场回滚。**
