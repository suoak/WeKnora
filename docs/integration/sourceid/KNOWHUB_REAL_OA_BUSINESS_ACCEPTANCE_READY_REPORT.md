# KnowHub真实OA业务验收READY检查报告

核查日期：2026-10-17（北京时间）。结论：**采集器 Mock 校准通过；真实已有账号与 JIT 验收均暂为 NO-GO。** 本轮未发起真实 SID 认证，未改变运行门禁、APP、数据库或默认项目，未访问生产。

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

## 4. 事业部JIT准入依据是否确认

**UNKNOWN，JIT不可放行。** RJGH稳定唯一不回收、RJEMAIL可信均已确认，不重复列为阻塞。待 SID 确认的是应用是否仅允许目标事业部员工；若不是，须提供可信范围/组织属性的准确路径、语义、允许值、时效与变更规则。

历史结构曾观察到 SZDW/SZDWCODE 等字符串，但没有可信事业部字典，不能据字段名推断。f40候选的域名限制不构成事业部资格证明；若依赖组织声明，需另行实现并测试最小资格校验门禁后才可开启 JIT，不能只打开配置。新用户不得继承历史账号或敏感空间权限。

## 5. 会话和目录同步政策是否确认

**尚未批准。** 普通 SSO、Access-only、prompt=false方向已确定；Access/Absolute/Idle/AdmissionMaxAge 的60/60/30/60分钟仍为候选，政策引用、残余风险接受人和发布/撤销责任人待确认。普通 SSO 可能复用既有 SID 会话，不承诺实时员工状态复核或统一登出。

目录需确定全量/增量来源、周期P、最大交付延迟L、恢复预算R及余量；EnterpriseMaxAge不能凭空填写。代码实际要求同时满足 `now < verified_at + EnterpriseMaxAge` 和 `now < valid_until`，且 active/政策/归属一致。配置应覆盖批准的P+L+R及余量，并受业务可接受失效窗口约束；同步重放不能伪造新的核验时间。过期或来源不可用时不得无限沿用旧证明，恢复必须重取权威数据，停用/撤销按已有机制执行。

容量已验收的4300成功/100预期拒绝保留，本轮未重复压力测试。生产 APP 副本、CPU/内存、PG连接预算、SID Token/UserInfo并发/限流和成功率/P95/P99阈值仍 UNKNOWN。现场测试PG为100连接、保留3，不能套作生产参数。PG代码只设 idle=10、连接寿命10分钟，未显式限制 MaxOpen（默认0，无上限），缺 DBStats 等待观测。建议单独提交连接池配置及受控聚合指标变更，观察 InUse/Open/WaitCount/WaitDuration，以“副本数×连接上限+任务+运维余量≤PG预算”验收；本轮未修改或部署该变更。

## 6. 真实SID业务验收还需要我完成什么操作

1. 单独确认上述日常 Chrome 关闭/重启方案；之后由执行方接入并校准真实观察通道，用户不需提供 Network 截图、Cookie或Token。
2. 指定数据负责人，通过受保护渠道提交历史归属批次及事业部资格依据；先批准 dry-run，真实 apply 另行授权。SID补充范围契约与接口限流；运维补充生产资源及容量阈值。
3. 与安全/业务负责人确认会话期限、普通SSO风险、目录新鲜度/恢复预算、政策引用和撤销责任人；不使用虚构审批编号。
4. 分别批准 A既有账号、B确无历史账号的新员工JIT的测试身份、数据写入及认证笔数。首次、再次登录各消耗新事务，均须显式授权；失败不自动重试。

A验收：原 user_id/全部角色与空间权限不变，Portal、auth/me、原知识库实际读取、禁止资源拒绝、退出/撤销通过；管理员与普通用户完全同流程。B验收：仅一个无特权新账号，无默认空间/知识权限，Portal和退出/撤销通过。每笔均由采集器先保存证据、恢复门禁并定向失效会话；不自动删除新增账号或绑定，数据清理另行批准。

## 7. 可否开始真实已有账号/JIT登录测试

**A：NO-GO；B：NO-GO。** A缺真实Chrome观察链路、可信历史归属批次/批准apply、会话政策与单笔业务授权；B另缺事业部开户资格契约及必要的资格门禁适配。不能以已通过 Mock 或 Token200替代这些条件。

当前独立试点 APP f40b1602f、Frontend832b6652a、DB3029不变；OIDC关闭、protocol_only=true、contract/email/auto_binding关闭、Session仍external、prompt=false、无Refresh、oauth_form不变；Session/JIT记录均为0，无有效未消费事务。默认项目、原库与生产未操作。最终仍按审批后一次性 enterprise_all 开放，不设线上人数白名单，不按角色区分接入。
