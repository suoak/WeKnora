# KnowHub真实OA业务验收READY检查报告

更新日期：2026-10-17（北京时间）。产品基线：**公司全员一次性开放，不按事业部、部门、组织、角色或人数限制登录。** 事业部范围确认及组织字段校验要求已取消，不再是阻塞或待开发项。结论：采集器 Mock 校准通过；真实已有账号与 JIT 验收均暂为 NO-GO。本次只更新材料与检查源码，未发起真实 SID 认证或修改环境；以下现场数值沿用上次只读收据，不声称本次重新核查。

## 1. 业务采集器是否Mock校准通过

**PASS：Windows、测试机 Linux 各25项；原监测器28项兼容回归通过。** 固定独立项目 `knowhub-oa-sso-pilot`、APP `f40b1602f69ce5d88e1e87dc3cefae36e9bde356`、DB3029 clean；实际镜像 `sha256:742c7ce56ef38ad32548900c8e7f8b264f9f6cd6138043674b91b27557443413`。

新增 [状态机](business-test/business_monitor.py)、[业务适配器](business-test/business_monitor_adapter.py)、[受控运行入口](business-test/business_monitor_pilot.py) 与 [测试](business-test/business_monitor_test.py)。只复用原白名单过滤器和事务结构，不调用旧 `protocol_monitor_runtime.py`。当前仅将候选脚本放入独立工具目录，未执行真实运行入口。

- 普通 SSO 必须 prompt=false；期望 identity_mapping、local_session、Portal、auth/me、权限读取及退出均有明确断言。既有账号核对原 user_id、用户管理标记、成员关系、组织关系、ACL；JIT 核对仅一个新用户/身份记录、tenant_id=0、无管理标记/成员/默认知识权限。
- 单笔事务、唯一 callback Trace、独占窗口及私有 Session 增量联合关联；这不是应用原生事务到 Session 的外键证明。出现第二笔 authorize、未知 callback、多个 Session 或无法归属时停止，不能猜测 PASS。
- 最长人工窗口8分钟且受本地事务到期限制；密码页面不是完成信号。必须有 Portal/API 观察结果和本人完成反馈。缺日志为 NOT_CAPTURED，未执行为 NOT_EXECUTED，失败不重试；Token200或页面跳转不能单独通过。
- 正常结束、异常和超时均先保存白名单证据，再恢复原 APP 配置，定向撤销可唯一归属的 Session/Access，并清理本笔未消费事务。无法归属时不批量撤销其他人的 Session，报告恢复失败并要求定向处理。SIGKILL/主机故障不属于已证明的自动恢复范围。
- 输出不包含员工原值、Token、Code、Cookie、完整URL或私有用户/Session句柄。内部随机句柄只在内存及0700目录/0600观察通道中用于等值核对；异常对象和原始日志不落盘。

本次校准范围是合成控制面、loopback Mock HTTP 观察通道及现场只读 SQL 结构核验；**尚未连接日常 Chrome 的真实观察器，不能视为真实浏览器业务校准完成**。缺少可信通道时不能 READY/PASS。证据：[现场与Linux校准](evidence/business-monitor-readiness-20261017.json)。

## 2. 真实Chrome路由是否准备完成

**方案与无凭据预检完成；日常有SSO的 Chrome：NOT_READY。** 临时空白浏览器已证明页面、静态资源、配置 API、空参数 callback 经本机18190隧道进入服务器loopback18090；只访问 SID 公共 Discovery，没有真实 authorize/Token/UserInfo。IPv4和IPv4映射IPv6直连18090被拒绝。证据：[路由](evidence/business-readiness-browser-route-20261017.json)、[入口限制](evidence/business-readiness-windows-ingress-20261017.json)。

日常 Chrome 当前 hosts 仍把 KnowHub 域名指向测试机地址，普通80端口会进入默认项目；空白浏览器结果不能证明日常 Chrome 的路径安全。本轮未关闭、重启或改动日常 Profile、Cookie、全局代理、hosts/DNS。

待单独确认的最小操作：保存浏览器工作，完整退出日常 Chrome → 建立18190到试点18090的受控 SSH 隧道 → 使用原 Profile 重新启动 Chrome，仅本进程使用无代理及 KnowHub 域名到本机18190的临时解析规则 → 接入受保护观察通道，自动核验上述四类请求及 callback 同源路由。SID 官方 HTTPS 不改道。SSO丢失、观察通道不可用或任一路由不确定即停止，不另行认证。恢复时关闭受控 Chrome/隧道，按原方式启动原 Profile，不保留临时参数。**本报告不授权执行这次浏览器生命周期操作。**

## 3. 历史账号归属数据是否就绪

**NOT_READY。** 独立干净副本有3个用户、5条空间成员关系；生产账号总数 UNKNOWN。本轮遵守“不操作生产”，未连接生产数据库，不能以副本3人代表生产范围。

数据负责人需统一提供完整历史账号范围及受保护批次：原 user_id、可信 RJGH/邮箱、历史归属依据与来源、有效状态、版本、verified_at、valid_until及变更规则。所有角色使用同一核对规则，不能用当前邮箱唯一或本次 OIDC 成功倒推出历史所有权。沿用已有批量工具默认 dry-run → 冲突汇总 → 独立批准 apply → 幂等重放；本轮未导入或 apply 真实批次。生产范围可由负责人提供只读汇总，或另行授权后开展只读核查。

## 4. 公司级JIT身份与邮箱域政策是否确认

**公司级产品范围已确认；实际SID账号类型及完整获批邮箱域集合 UNKNOWN。** RJGH稳定唯一不回收、RJEMAIL可信均已确认，不重复列为阻塞。只需核实 SID 应用实际允许的企业账号类型与公司批准的开户资格（包括如何处理非员工/服务等账号），及所有正式企业邮箱域。Discovery不能证明应用级账号类型政策；当前资料未给出正式完整清单，不能把已见邮箱域当成唯一或已获批的全量域。

现有 f40 支持 `SOURCEID_JIT_EMAIL_DOMAINS` 逗号分隔的多个精确企业域，规范化大小写并逐域匹配；不使用通配符或自动包含子域，无需重写JIT。不新增 SZDW/SZDWCODE 或任何事业部组织属性校验。严格SID企业身份和正式批准的完整邮箱域政策共同控制开户；新账号无管理标记、无默认空间/敏感知识权限。公共知识访问通过原有共享ACL单独授予，允许登录不等于允许读取所有知识。

## 5. 会话和目录同步政策是否确认

**尚未批准。** 普通 SSO、Access-only、prompt=false方向已确定；Access/Absolute/Idle/AdmissionMaxAge 的60/60/30/60分钟仍为候选，政策引用、残余风险接受人和发布/撤销责任人待确认。普通 SSO 可能复用既有 SID 会话，不承诺实时员工状态复核或统一登出。

目录需确定全量/增量来源、周期P、最大交付延迟L、恢复预算R及余量；EnterpriseMaxAge不能凭空填写。代码实际要求同时满足 `now < verified_at + EnterpriseMaxAge` 和 `now < valid_until`，且 active/政策/归属一致。配置应覆盖批准的P+L+R及余量，并受业务可接受失效窗口约束；同步重放不能伪造新的核验时间。过期或来源不可用时不得无限沿用旧证明，恢复必须重取权威数据，停用/撤销按已有机制执行。

容量已验收的4300成功/100预期拒绝保留，不重复同参数压力测试。原300–500人仅代表旧规划，不是全公司总规模或公司级容量目标。全公司预计用户数、最忙一分钟登录人数、新旧账号比例、生产 APP 副本/CPU/内存、PG连接预算、SID Token/UserInfo并发/限流和成功率/P95/P99阈值仍 UNKNOWN；取得这些输入后重新确定合成负载及验收目标。现场测试PG为100连接、保留3，不能套作生产参数。PG代码只设 idle=10、连接寿命10分钟，未显式限制 MaxOpen（默认0，无上限），缺 DBStats 等待观测。建议单独提交连接池配置及受控聚合指标变更，观察 InUse/Open/WaitCount/WaitDuration，以“副本数×连接上限+任务+运维余量≤PG预算”验收；本轮未修改或部署该变更。

## 6. 真实SID业务验收还需要我完成什么操作

1. 单独确认上述日常 Chrome 关闭/重启方案；之后由执行方接入并校准真实观察通道，用户不需提供 Network 截图、Cookie或Token。
2. 指定数据负责人，通过受保护渠道提交公司全部历史账号归属批次；先批准 dry-run，真实 apply 另行授权。SID确认应用实际允许的企业账号类型和接口限流，公司负责人批准完整企业邮箱域集合；运维补充公司用户规模、峰值、生产资源及容量阈值。无需事业部范围或组织字段资料。
3. 与安全/业务负责人确认会话期限、普通SSO风险、目录新鲜度/恢复预算、政策引用和撤销责任人；不使用虚构审批编号。
4. 分别批准 A既有账号、B确无历史账号的新员工JIT的测试身份、数据写入及认证笔数。首次、再次登录各消耗新事务，均须显式授权；失败不自动重试。

A验收：原 user_id/全部角色与空间权限不变，Portal、auth/me、原知识库实际读取、禁止资源拒绝、退出/撤销通过；管理员与普通用户完全同流程。B验收：仅一个无特权新账号，无默认空间/知识权限，Portal和退出/撤销通过。每笔均由采集器先保存证据、恢复门禁并定向失效会话；不自动删除新增账号或绑定，数据清理另行批准。

## 7. 可否开始真实已有账号/JIT登录测试

**A：NO-GO；B：NO-GO。** A缺真实Chrome观察链路、可信历史归属批次/批准apply、会话及同步政策与单笔业务授权；B另缺公司级SID账号类型/开户资格与完整邮箱域政策批准、历史账号范围完整性核验及独立JIT写入授权。事业部范围、组织字段和拟新增资格适配已移除；不需要因此开发新的部门门禁。不能以已通过 Mock 或 Token200替代剩余条件。

上次只读核验：独立试点 APP f40b1602f、Frontend832b6652a、DB3029；OIDC关闭、protocol_only=true、contract/email/auto_binding关闭、Session仍external、prompt=false、无Refresh、oauth_form，Session/JIT记录0，无有效未消费事务。本次保持不变，默认项目、原库与生产未操作。正式目标为 enterprise_all / oidc_sso_approved / local_approved / prompt=false / Refresh=false，其他门禁审批后按依赖加载。公司全员一次性开放，无线上人数白名单或部门/角色分流；已有账号继承原权限，新账号无特权，知识访问继续遵循ACL。
