# KnowHub真实OA业务验收READY检查报告

更新日期：2026-10-17（北京时间）。产品基线：**公司全员一次性开放，不按事业部、部门、组织、角色或人数限制登录。** 事业部范围确认及组织字段校验要求已取消。最新获批部署已将原测试80/8080切到`knowhub-company-test`（f40/832/DB3029干净副本），见[入口替换报告](KNOWHUB_COMPANY_TEST_ENTRY_DEPLOYMENT_REPORT.md)。原独立试点保留，不再要求用户走18090/Chrome隧道；未发起真实SID认证，业务门禁仍关闭。真实已有账号与JIT均为NO-GO。

## 1. 业务采集器是否Mock校准通过

**新入口Mock校准PASS：Windows、测试机 Linux 各30项；原监测器28项兼容回归通过。** 现固定 `knowhub-company-test`、APP `f40b1602f69ce5d88e1e87dc3cefae36e9bde356`、DB3029 clean、原80/8080；镜像 `sha256:742c7ce56ef38ad32548900c8e7f8b264f9f6cd6138043674b91b27557443413`。旧独立试点的日志/Session不用于新环境判定，单Compose基线不再包含旧image pin。

新增 [状态机](business-test/business_monitor.py)、[业务适配器](business-test/business_monitor_adapter.py)、[受控运行入口](business-test/business_monitor_pilot.py) 与 [测试](business-test/business_monitor_test.py)。只复用原白名单过滤器和事务结构，不调用旧 `protocol_monitor_runtime.py`。当前仅将候选脚本放入独立工具目录，未执行真实运行入口。

- 普通 SSO 必须 prompt=false；期望 identity_mapping、local_session、Portal、auth/me、权限读取及退出均有明确断言。既有账号核对原 user_id、用户管理标记、成员关系、组织关系、ACL；JIT 核对仅一个新用户/身份记录、tenant_id=0、无管理标记/成员/默认知识权限。
- 单笔事务、唯一 callback Trace、独占窗口及私有 Session 增量联合关联；这不是应用原生事务到 Session 的外键证明。出现第二笔 authorize、未知 callback、多个 Session 或无法归属时停止，不能猜测 PASS。
- 最长人工窗口8分钟且受本地事务到期限制；密码页面不是完成信号。必须有 Portal/API 观察结果和本人完成反馈。缺日志为 NOT_CAPTURED，未执行为 NOT_EXECUTED，失败不重试；Token200或页面跳转不能单独通过。
- 正常结束、异常和超时均先保存白名单证据，再恢复原 APP 配置，定向撤销可唯一归属的 Session/Access，并清理本笔未消费事务。无法归属时不批量撤销其他人的 Session，报告恢复失败并要求定向处理。SIGKILL/主机故障不属于已证明的自动恢复范围。
- 输出不包含员工原值、Token、Code、Cookie、完整URL或私有用户/Session句柄。内部随机句柄只在内存及0700目录/0600观察通道中用于等值核对；异常对象和原始日志不落盘。

本次校准范围是合成控制面、loopback Mock HTTP 观察通道及新项目现场只读SQL/入口核验；**真实Portal/API观察IPC仍需启动前接通，不将Mock当真实业务闭环**。缺少可信通道时不能真实READY/PASS。[最新校准](evidence/company-first-monitor-calibration-20261017.json)。本轮未重建APP或改变门禁。

## 2. 真实Chrome路由是否准备完成

**原地址路由准备PASS，不再需要隧道或日常Chrome重启。** 用户原hosts指向172.30.232.42保持；80/8080现统一代理到新环境。Windows临时无凭据Chrome实测页面、静态资源、配置API和空callback均经172.30.232.42:80进入新环境，8080健康同一环境标记；callback安全invalid_state。日常SSO Cookie未读取、复制或修改，尚未执行真实授权。[新入口浏览器证据](evidence/company-entry-browser-20261017.json)。

原18090隧道方案已取消，不作为用户操作或阻塞。新业务采集器固定目标已改为`knowhub-company-test`并通过30项Mock校准，Portal/API真实观察需在获批窗口前接通；没有真实日常Chrome认证证据时不声称业务PASS。当前工作站允许、未批准Docker桥接来源拒绝，真实窗口前还要确认没有代理绕路或并行认证。

## 3. 历史账号归属数据是否就绪

**NOT_READY。** 独立干净副本有3个用户、5条空间成员关系；生产账号总数 UNKNOWN。本轮遵守“不操作生产”，未连接生产数据库，不能以副本3人代表生产范围。

**首次已有账号验收只需一行，不必等待公司全量批次。** 本轮受保护邮箱唯一命中1个active历史账号，system_admin=true、cross_tenant=false、1个owner成员、所属空间1个有效KB；既有归属/身份均0，未改原user_id或权限。仍缺独立核实的RJGH、历史证明、来源/政策引用、版本和verified_at/valid_until。单账号dry-run工具已准备，但真实dry-run尚未执行（缺证据），真实apply另行授权。邮箱唯一或本次OIDC成功不等于历史所有权。公司全量批次仅仍为后续正式发布准备，不阻塞首笔这一个账号。

## 4. 公司级JIT身份与邮箱域政策是否确认

**公司级产品范围已确认；实际SID账号类型及完整获批邮箱域集合 UNKNOWN。** RJGH稳定唯一不回收、RJEMAIL可信均已确认，不重复列为阻塞。只需核实 SID 应用实际允许的企业账号类型与公司批准的开户资格（包括如何处理非员工/服务等账号），及所有正式企业邮箱域。Discovery不能证明应用级账号类型政策；当前资料未给出正式完整清单，不能把已见邮箱域当成唯一或已获批的全量域。

现有 f40 支持 `SOURCEID_JIT_EMAIL_DOMAINS` 逗号分隔的多个精确企业域，规范化大小写并逐域匹配；不使用通配符或自动包含子域，无需重写JIT。不新增 SZDW/SZDWCODE 或任何事业部组织属性校验。严格SID企业身份和正式批准的完整邮箱域政策共同控制开户；新账号无管理标记、无默认空间/敏感知识权限。公共知识访问通过原有共享ACL单独授予，允许登录不等于允许读取所有知识。

## 5. 会话和目录同步政策是否确认

**尚未批准。** 普通 SSO、Access-only、prompt=false方向已确定；Access/Absolute/Idle/AdmissionMaxAge 的60/60/30/60分钟仍为候选，政策引用、残余风险接受人和发布/撤销责任人待确认。普通 SSO 可能复用既有 SID 会话，不承诺实时员工状态复核或统一登出。

目录需确定全量/增量来源、周期P、最大交付延迟L、恢复预算R及余量；EnterpriseMaxAge不能凭空填写。代码实际要求同时满足 `now < verified_at + EnterpriseMaxAge` 和 `now < valid_until`，且 active/政策/归属一致。配置应覆盖批准的P+L+R及余量，并受业务可接受失效窗口约束；同步重放不能伪造新的核验时间。过期或来源不可用时不得无限沿用旧证明，恢复必须重取权威数据，停用/撤销按已有机制执行。

容量已验收的4300成功/100预期拒绝保留，不重复同参数压力测试。原300–500人仅代表旧规划，不是全公司总规模或公司级容量目标。全公司预计用户数、最忙一分钟登录人数、新旧账号比例、生产 APP 副本/CPU/内存、PG连接预算、SID Token/UserInfo并发/限流和成功率/P95/P99阈值仍 UNKNOWN；取得这些输入后重新确定合成负载及验收目标。现场测试PG为100连接、保留3，不能套作生产参数。PG代码只设 idle=10、连接寿命10分钟，未显式限制 MaxOpen（默认0，无上限），缺 DBStats 等待观测。建议单独提交连接池配置及受控聚合指标变更，观察 InUse/Open/WaitCount/WaitDuration，以“副本数×连接上限+任务+运维余量≤PG预算”验收；本轮未修改或部署该变更。

## 6. 真实SID业务验收还需要我完成什么操作

1. 无需批准Chrome关闭/重启或隧道。新项目采集目标Mock已校准；执行方启动前接通观察通道。用户继续使用原测试地址，真实窗口获批后只需点击一次OA并完成认证，不需提供Network截图、Cookie或Token。
2. 首笔先由数据负责人提供这一个已选原账号的RJGH与独立历史归属证明及有效时间，dry-run后另批apply；不等待全公司批量导入。公司完整域集合/JIT政策及公司容量留后续，不作为首笔已有账号额外条件。无需事业部范围或组织字段资料。
3. 与安全/业务负责人确认会话期限、普通SSO风险、目录新鲜度/恢复预算、政策引用和撤销责任人；不使用虚构审批编号。
4. 分别批准 A既有账号、B确无历史账号的新员工JIT的测试身份、数据写入及认证笔数。首次、再次登录各消耗新事务，均须显式授权；失败不自动重试。

A验收：原 user_id/全部角色与空间权限不变，Portal、auth/me、原知识库实际读取、禁止资源拒绝、退出/撤销通过；管理员与普通用户完全同流程。B验收：仅一个无特权新账号，无默认空间/知识权限，Portal和退出/撤销通过。每笔均由采集器先保存证据、恢复门禁并定向失效会话；不自动删除新增账号或绑定，数据清理另行批准。

## 7. 可否开始真实已有账号/JIT登录测试

**A：NO-GO；B第二轮，不在本轮启用。** A只缺单账号可信归属/dry-run与批准apply、会话和该行归属新鲜度政策、启动前观察通道及一笔业务授权，不需要公司全量数据或JIT批准。采集器新目标30项Mock已通过。首笔账号没有空间外有效KB，真实负向用例需另批合成对照资源并遵守该管理员原有权限，不能用不存在ID冒充403。B后续仍需公司级JIT政策及独立授权。[本笔操作卡与五项结论](SOURCEID_FIRST_EXISTING_ACCOUNT_OPERATION_CARD.md)。其余OIDC严格校验及门禁不变。

新原址环境f40/832/DB3029：OIDC关闭、protocol_only=true、contract/email/auto/JIT批准开关关闭、Session仍external、prompt=false、无Refresh、oauth_form，Session/JIT/归属记录0，无有效未消费事务。旧默认APP/Frontend停止保留，旧DB3027及其他组件保留；原独立试点、原业务库及生产未改。正式目标为 enterprise_all / oidc_sso_approved / local_approved / prompt=false / Refresh=false，其他门禁审批后按依赖加载。公司全员一次性开放，无线上人数白名单或部门/角色分流；已有账号继承原权限，新账号无特权，知识访问继续遵循ACL。
