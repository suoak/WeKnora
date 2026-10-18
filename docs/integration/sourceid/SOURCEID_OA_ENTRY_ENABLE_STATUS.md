# OA入口开启记录

北京时间2026-10-18，用户明确要求开启OA入口。仅172.30.232.42、knowhub-company-test。

- 唯一运行配置变更：`OIDC_AUTH_ENABLE=false→true`。基础Compose→既有app-pin.json→受保护`approved-entry-only.json`，只重建APP一次，仍为5f988be51；Frontend832、DB3030及其他容器不变。
- 80/8080配置API均返回enabled=true；Windows工作站直连也确认入口开启。刷新原登录页可显示“使用OA登录”。未改变Chrome、hosts或全局网络。
- 其余运行环境逐键对照一致：protocol_only=true、contract/email/auto/JIT=false、Session external、Refresh=false、prompt=false、verified邮箱政策confirmed=false，link_mode仍trusted_ownership；无虚假政策引用。
- 未由代理发起SID认证或创建员工身份绑定。本次仅开启入口，**真实认证后仍应在identity_contract_pending停止，不能进入Portal**。
- 若目标是原账号业务验收，仍需公司可信邮箱关联政策（含未知历史邮箱转让风险）、会话期限和A范围及一笔关联/Session写入批准；已向用户明确区分协议验证与业务登录，未自行放开其余门禁。

受保护配置及原运行配置备份：`/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51/private/oa-entry-20261018T012025Z`。恢复入口关闭只需移除最后一层entry覆盖，保留基础Compose及候选pin，仅重建APP，不改镜像或数据库。

脱敏证据：`evidence/company-oa-entry-enable-result-20261018.json`。本记录是入口状态，不是完整真实OA业务验收PASS。
