# KnowHub OA 登录模块最终验收

日期：2026-10-18。**核心登录功能可在已批准测试范围交付；正式全公司一次性发布仍需完成上线门禁。**

APP：`5f988be51779f1a22bd9d027b7503fe6a8749f62`；Frontend：`832b6652a`；DB3030 clean。
本次真实JIT仅使用同一SID身份在空白新实例开户，不冒称不同真实新入职员工的全面准入验收。

## 验收结论

| 维度 | 状态 | 实际证据及范围 |
|---|---|---|
| 已有账号真实OA登录及自动关联 | **PASS** | 前次用户确认正常进入原账号；服务端绑定及Session指向原user_id，无重复开户 |
| 原管理员、owner及知识库权限 | **PASS** | 真实页面/文档使用确认；原权限和绑定保留，3文档、2717有效分块及向量引用、文件清单保持 |
| 新实例真实JIT自动开户 | **PASS** | knowhub-jit-fresh中1用户、1绑定、1JIT记录，1笔授权且已消费；新user_id不同于原管理员；用户确认正常进入 |
| 新账号默认零权限 | **PASS** | tenant_id=0，system_admin/cross_tenant均false，成员数0、知识库数0；原管理员权限未复制 |
| Access-only、退出与撤销 | **PASS** | 真实网关auth/me200、logout200；新实例退出并定向撤销后有效Session=0、Access=0，Refresh=0；用户/绑定/JIT审计保留 |
| Session到期策略 | **PASS（隔离确定性）** | 合成JWT Access过期及Idle/Absolute/Admission分别到期，auth/me401、SwitchTenant401；页面刷新/Refresh均不能恢复；合法切换不延长绝对截止 |
| 自然等待到期 | **NOT_EXECUTED** | 没有等待真实60分钟或改变服务器时间，不与上述模拟结果混算 |
| 越权拒绝 | **PASS（隔离API）** | 两个真实存在的合成空间/KB；授权owner读取200，viewer本空间200，另一KB403、切入另一空间403；确认无成员/共享，未修改原管理员 |
| Trace与审计 | **PARTIAL** | 真实网关callback302、result200、auth/me200、logout200，JIT/绑定/Session及政策审计齐备；OIDC阶段及Token HTTP Trace未捕获，不能逐阶段补写PASS |
| 网关恢复及原业务数据完整性 | **PASS，附审计差异说明** | 两端口恢复原标记、原APP配置/DB/Redis/文件卷未变，新网络已断开；新实例门禁关闭；原用户3/绑定1/JIT0，原文档和向量完整 |
| 不同真实员工、完整公司账号类型及邮箱域 | **NOT_EXECUTED** | 本次仅一个人的同SID身份跨实例；ruijie.com.cn是本次获批单域，不等于全部公司域验收 |

新实例真实run_id：`46654ce8-26c9-4d9f-bf0a-a6e3229ff117`。
北京时间12:11:22 READY，12:13:01进入自动恢复，12:13:16完成恢复。
批准依据：[本次用户明确授权](SOURCEID_FRESH_JIT_REAL_TEST_AUTHORIZATION_20261018.md)。
执行人为Codex受控进程，通过aqyfzx仅操作172.30.232.42；不替其他负责人编造批准。

## Trace缺口与最小修正

已确定本次实例继承`LOG_LEVEL=error`，而SourceID阶段/HTTP诊断通过logger.Warn输出；
诊断开关true但输出仍被过滤。该预检遗漏导致真实阶段及Token状态/耗时为NOT_CAPTURED，
不能将Portal成功或数据库写入反填成Token200/签名/nonce各阶段的独立实测记录。

未来窗口模板已显式设置`LOG_LEVEL=warn`，并保留进程最终配置校验；不修改当前已恢复服务。
在同一APP镜像的隔离Mock中，已采集7笔合成授权各8个完整成功阶段，证实warn级别修正可恢复
阶段事件。HTTP诊断过滤后的汇总在该Mock中仍为0，HTTP传输摘要完整关联尚未验收，继续PARTIAL。
未为补Trace发起另一笔真实认证，不修改OIDC校验或重写认证主流程。

## 原库审计差异：完整披露，不清理记录

恢复程序在12:13:16完成时，原受保护数据基线一致。后续只读复核发现：
12:13:21.567（北京时间）原账号新增一条原实例Session，已revoked、无有效Access；
原外部身份的last_login_at/updated_at随之改变。没有新增/删除原用户或绑定，原JIT仍0，
原角色、成员、知识库及文件不变。

这是恢复后观察到的独立原实例登录审计，不能归因成新JIT用户写回，也不推测操作者或具体入口。
运行器没有调用真实authorize/Token/UserInfo；本次新实例始终只有1笔授权。
因此不宣称“事后所有审计字节完全未变”，不删除该记录以制造基线一致。
最终两个实例有效企业Session/Access均0；新JIT用户、绑定和审计均保留。

## 隔离补测及既有证据复用

新增隔离补测使用独立、无外网的临时PG/Redis/APP/Mock SID/Frontend，恢复的是认证前已保存的
空白合成模板，不复制本次真实用户。全部临时容器已移除，实际两个站点未受补测影响。

- Access过期使用合成JWT时间声明；Idle/Absolute/Admission使用合成数据库时间记录，不改真实时钟。
- 到期前合法SwitchTenant保留Session ID及绝对截止；到期后所有访问/切换/Refresh拒绝。
- 邮箱政策标记失效后Access401，恢复旧标记后仍401，未复活已撤销会话。
- 对照资源有实际DB记录及授权owner的200读取，拒绝结果为403，不是随机ID或404。
- 准备夹具时修正了沿用旧schema的已删除rerank_model_id字段；未修改产品schema或放宽业务断言。

重复登录、停用、撤销、邮箱重复/软删除/转让冲突、历史冲突不得JIT兜底、全角色及并发保护，
复用未变化5f988be51的实现报告、PostgreSQL和四项CI证据及前序Mock，不机械重跑。
不将此前合成全部角色测试称为所有角色均已由不同真实员工登录。
本轮控制/恢复/采集器本地回归63项PASS；本轮没有新的APP构建、迁移或CI通过声明。

## 正式一次性全公司上线仍需完成

1. 正式公司级可信邮箱关联/JIT风险接受、负责人、完整企业账号类型及精确邮箱域集合；
   测试用户批准不能冒充生产公司政策。无需逐员工ownership JSON，无部门或管理员额外入口。
2. 正式Session期限、普通SSO停用风险及撤销响应责任；已知邮箱转让/身份冲突、停用和异常数据
   更新处理机制。若启用目录分支，再批准真实同步周期、EnterpriseMaxAge/valid_until，不伪造数据源。
3. 阶段/Token HTTP监控与告警的完整性校准；保持本次真实Trace缺口记录，不追认完整协议Trace PASS。
4. 公司预计规模/集中首次登录峰值、生产资源和PG池指标、SID并发/限流及P95/P99标准。
   原300/500人Mock通过不是全公司容量认证，不重复同参数压测。
5. 生产网络/TLS/callback、真实数据与文件成套备份、增量迁移、集中监控、回滚责任和发布操作卡，
   获得正式发布的单独授权。生产10.51.134.114本轮未操作。

正式策略仍是一次性向符合条件的全公司员工开放，不设置线上人数白名单或按角色灰度。

安全收据：服务器`real-window/evidence.json`、`recovery.json`、`acceptance-safe.json`、
`final-integrity-safe.json`及`private/security-closeout/result.json`；本机同名验收收据。
仅脱敏计数、固定类别和配置元数据进入报告，真实身份值、凭据、Code、Token、Cookie均未导出。
