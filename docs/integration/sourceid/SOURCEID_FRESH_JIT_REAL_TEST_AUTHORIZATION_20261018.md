# 本次真实 JIT 测试授权依据

授权来源：用户本次明确消息“以下事项全部允许”及其列明的七项权限。
不是独立安全负责人签字、正式生产批准或公司全员发布授权。

- 测试批准及风险接受来源：当前发起测试的用户本人；不得补造其他负责人的身份或签字。
- 实际执行：Codex受控进程，通过aqyfzx账号仅操作172.30.232.42。
- 实际执行时间、run_id及维护起止以服务器受保护`real-window/approval.json`、
  `status.json`、`recovery.json`的UTC记录为准，可换算北京时间；不预填完成时间。
- 真实引用：`user-conversation:2026-10-18:fresh-jit-single-business-window-v1`。

本次批准enterprise_all、verified_email_policy和JIT，精确域ruijie.com.cn，普通SSO，
prompt=false，Access/Absolute/Idle/Admission=60/60/30/60分钟，无Refresh。
用户已接受复用SID SSO不等同实时停用复核、本地退出不等同SID统一退出的测试风险。
SID callback保持原合法地址，严格state/nonce/签名/issuer/audience/时间/UserInfo主体校验不变。

允许原80/8080入口短时维护、冻结原测试APP写入以制作最新DB/文件备份、临时切换至
knowhub-jit-fresh；原管理员账号、绑定、权限和知识库不改。仅在新实例创建1个普通tenantless
用户、身份绑定、JIT审计及有期限Session，管理员/跨租户标记false，无默认成员和知识库权限。
不限制部门，不为管理员设置不同认证流程。

一次真实认证必须由用户本人点击OA并完成；运行器不发起真实authorize，不自动重试或
重复兑换。失败/取消/超时、完成退出后均自动保存脱敏证据、关闭新门禁、定向撤销本次Session/
Access、保留新用户和审计，恢复原网关与网络并核对原数据。恢复失败必须明确告警。

用户同时批准剩余合成权限/可控时间测试及复用既有安全证据，未授权第二笔真实认证。
同一SID身份在空白实例开户不代表不同真实新员工、公司全部账号类型或全部邮箱域已验收。
不修改生产10.51.134.114，不实施全公司发布，不将新实例数据写回原DB3030。
