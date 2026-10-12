# 待 SID 负责人确认的最终问题

请逐项由负责人填写：正式值/契约版本、适用环境、证据位置、确认人/日期、KnowHub复核人。不发送 Client Secret、原始JWT、SessionIndex或真实员工资料到 Git/公开工单；通过企业批准的秘密通道和受限测试证据库交付。关联 [完整契约清单](SOURCEID_CONTRACT_CHECKLIST.md)。

| 编号 | 必须回答的问题 | 可接受的交付证据/门禁 |
|---|---|---|
| Q01 | 正式 issuer 精确值是什么？是否提供 Discovery？授权/Token/Profile/JWKS 完整 URL 各是什么？ | 正式元数据或签署端点表；示例 /cas 路径不算证据 |
| Q02 | ID Token 的签名算法、kid、JWKS 无凭据获取/轮换/CA方案？aud/azp是否为 knowhub？ | 正式签名配置及脱敏结构；当前仅RSA RS256/384/512 |
| Q03 | state原样回传与ID Token nonce匹配是否支持？PKCE S256是否支持/强制？时钟偏差与code TTL？ | 正式字段契约；不能因示例nonce为空取消校验 |
| Q04 | Token支持urlencoded POST吗？client_secret_post或basic？Profile是哪种body/请求头编码？是否有额外网关认证？ | 精确Content-Type/方法/参数位置说明；仅multipart Token需代码适配 |
| Q05 | 工号在何可信来源/字段？其自然人唯一性、前导零、岗位多身份/sub变化、离职再入职如何处理？ | 身份负责人和HR审核；不能把学工号示例或输入login作为企业工号 |
| Q06 | 启用/冻结/离职字段与允许值字典？三个业务字段能否同在ID Token或subject一致的Profile中？ | 真实字段树与状态字典；全选一来源，否则需必要适配 |
| Q07 | **OIDC每次会话对应通知SessionIndex的哪个字段？在ID Token、Profile还是Token Response？** 是否字符串原样相等、稳定且不复用？ | 两个独立SID会话的脱敏相关性证明；不能拿工号/sub/jti/auth_time代替；Token Response尚未支持 |
| Q08 | 独立设备是否独立TGT/index？主动退出影响一个设备还是所有设备？会话超时会通知此OIDC应用吗？版本支持条件？ | 应用配置、主动退出/超时的事件范围说明与通知集合；允许记录预期多个index但不能误判隔离 |
| Q09 | 注册的 callback 与 backchannel URI 是否准确匹配审批？HTTP是否获安全验收？SID节点是否能访问80端口？ | 注册/审批凭据、节点网络证明；若仅HTTPS需重新审批并适配固定callback |
| Q10 | POST logoutRequest实际在body还是query？Content-Type、XML namespace/version、多index规则、最大长度和压缩/编码？ | 脱敏HTTP形状与无真实会话的供应商fixture；不能从URL例子认定body已确认 |
| Q11 | **通知如何证明来源？** 原生签名/mTLS/专网保证、信任证书/密钥轮换、SID egress/NAT CIDR、代理拓扑分别是什么？ | 安全负责人确认的信任边界；内部网关secret须在真实来源验证后注入且不可后端绕过 |
| Q12 | ACK成功body/status、错误处理、timeout/retry/backoff、延迟/重复窗口、网络失败/数据库503的再次送达保证？ | 可执行事件送达契约/SLA与受控测试标准 |
| Q13 | SessionIndex/请求ID生命周期、重放规则、tombstone保留期、SID最大会话时间/续期策略是什么？ | 生命周期不复用证明与可验收保留策略；本地会话目前最长7天 |
| Q14 | 谁提供测试环境、审核测试员工、两独立设备、只读权限基线？谁批准首次真实登录、主动退出、超时和故障演练？ | 测试账号审核单、环境隔离/备份证明、执行人/时间窗/动作范围；有密钥不等于授权 |

KnowHub/平台团队另需收口：敏感路由日志排除（网关、后端、APM）；只读部署/网络检查；现有管理员应急可用；secret安全注入；准确 peer 白名单与后端隔离；HTTP风险验收、备份恢复/回退演练。以上未完成不能开启真实测试，更不能发布。
