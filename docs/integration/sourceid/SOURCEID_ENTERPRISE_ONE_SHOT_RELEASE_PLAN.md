# KnowHub OA：正式一次性全员开放方案

状态：发布设计及独立环境准备，**未批准生产切换**。本文件以最新产品要求为准，替代线上分批/人数灰度推广建议。独立项目名称中的 pilot 只代表发布前测试环境，不代表正式发布先开放部分员工。

## 统一产品规则

- 正式一次性向所有符合公司企业身份条件的员工开放，不限制事业部、部门或组织，不设置线上试点名单或100/1000人数上限，不按角色分流（2026-10-17最终产品范围）。
- 已有账号：严格 SID OIDC → 可信历史归属 → issuer+RJGH 对原 user_id → 原角色、成员与 ACL；全部角色相同。
- 无历史账号的新员工：严格 OIDC、获批公司级SID企业账号类型与完整精确邮箱域策略通过，且不存在旧账号、重复邮箱、撤销或身份冲突，才建立无特权 tenantless 普通账号。零默认空间/知识库/管理权限，不自动创建 owner 空间；公共知识通过共享ACL单独授权。
- 任何旧账号历史归属未核实、邮箱回收、账号停用或身份冲突，统一拒绝错误关联/JIT；不阻塞其他正常员工，不逐人额外审批管理员。

## 正式切换前的统一门槛

1. 独立环境已完成APP f40b1602f、DB3029及保留832b6652a Frontend的技术验收；09 APP在3029兼容回滚已验证。独立技术PASS不代表真实认证、真实绑定或生产切换获批。
2. 权威数据负责人一次性核验所有历史账号，完成全量 dry-run、冲突归类、批准 apply、幂等重放及增量/停用恢复验证。没有真实接口契约，不编造 HR/SID 连接器。
3. SID应用实际允许的企业账号类型及公司开户资格、完整企业邮箱域、来源停用与异常处理经实际批准。默认无特权已明确，仍需真实验收；不存在历史账号不能仅凭匹配失败推断。不要求事业部范围确认，不新增SZDW/SZDWCODE等组织字段校验。现有JIT配置支持逗号分隔多域，无需重复开发。
4. 独立真实 SID 业务验收完成：原账号/管理员/多空间权限不变，新账号无特权开户、重复登录、Access-only、SwitchTenant、退出、到期、撤销及冲突拒绝。相关真实写入和登录必须逐项取得单独授权，本轮没有执行。
5. 60/60/30/60分钟仅候选，EnterpriseMaxAge及来源同步频率另行确认。普通 SSO 不宣称实时在职检查；prompt=false，不要求强制 auth_time，仍需正式接受已有 SID SSO 与停用延迟的残余风险。
6. 根据全公司预计人数、最忙一分钟首次登录/到期重登、新老用户比例及资源/SID容量重新确定生产负载与成功率/P95/P99门槛；输入尚待确认。原300–500人仅旧规划，4300成功/100预期拒绝历史结果保留，不重复同参数压测。不能用10000条目录导入或同一身份12路竞争代替公司级全链路容量。
7. 取得生产只读预检/发布授权后核验实际版本、schema、扩展、容量、回调、DNS/代理/TLS、网络、安全日志和同代备份。**本轮没有访问生产，生产这些项目尚未实测**；不能假定生产与独立DB3029一致。

## 正式配置（仅审批完成后加载）

目标为 enterprise_all、oidc_sso_approved、local_approved、prompt=false、Refresh=false。实际启用还需：

| 配置组 | 正式要求 |
|---|---|
| OIDC及协议 | 启用原获批 OIDC，oauth_form、原callback/PKCE策略；PROTOCOL_ONLY=false，CONTRACT_CONFIRMED=true；保持 state/nonce/RS256/Issuer/Audience/时间/UserInfo-sub 校验 |
| 旧账号关联 | EMAIL_BINDING_ENABLE、EMAIL_CONTRACT_CONFIRMED、AUTO_BINDING_ENABLE；真实 AUTO_BINDING_POLICY_REF，不按角色区分 |
| 归属 | ACCOUNT_SCOPE=enterprise_all、ENTERPRISE_POLICY_CONFIRMED、真实 ENTERPRISE_POLICY_REF 与获批 ENTERPRISE_MAX_AGE；不加载线上试点名单 |
| 会话 | SSO_POLICY_CONFIRMED、真实 LOCAL_POLICY_REF/ADMISSION_POLICY_REF；Access/Absolute/Idle/AdmissionMaxAge实际批准值；LOCAL_REFRESH_ENABLE=false |
| 新员工 | JIT_ENABLE、JIT_POLICY_CONFIRMED、真实 JIT_POLICY_REF、全部获批企业 JIT_EMAIL_DOMAINS；不是逐人名单 |
| 继续关闭 | FORCE_REAUTH_PROMPT_LOGIN、AUTHORIZATION_RECHECK_CONFIRMED、SID统一LOGOUT；不能伪造审批或授权复核能力 |

若任一必需政策、数据或期限缺失，则不放行。新旧账号同一 OA 按钮与协议链路，仅在内部区分历史账号绑定和新账号注册，角色不作为区别条件。

## 发布责任

发布负责人决定维护窗口与一次性切换；数据负责人签署历史归属及源更新契约；安全负责人确认普通SSO、JIT、期限、撤销及数据保护；SID负责人协调容量/协议及事件排查；运维负责人验证备份恢复、网络、监控和值班。负责人具体人选和真实批准引用待提供，不使用虚构编号。

## 停止和回退

任何确认的错绑、越权或敏感空间暴露立即停止全员 OA 新登录与JIT；系统性登录/数据库异常按获批监控阈值触发。先关闭门禁、保留证据，定向撤销受影响企业 Session/Token，再 APP/Frontend 回退固定已验收镜像；不自动回 raw、不降迁、不删账号或审计。

原数据与新增写入需要成套核对；只有明确冻结所有写入且确认无新增业务差异时，才按批准灾难恢复流程恢复数据库及同代文件。已有新业务写入不得用旧dump直接覆盖；隔离差异、审计和人工核准后修复。
