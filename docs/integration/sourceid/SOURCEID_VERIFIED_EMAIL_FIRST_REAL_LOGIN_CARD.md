# 首次真实OA业务操作卡：已有账号，JIT关闭

> 最新现场：5f988be51/832b6652a/DB3030已在原80/8080部署验收PASS，新策略与真实OIDC门禁仍关闭。无需再次部署或迁移；下一步等待真实公司政策、会话期限、A范围及一笔真实登录窗口批准。见[最终升级验收报告](SOURCEID_VERIFIED_EMAIL_TEST_SITE_UPGRADE_ACCEPTANCE_REPORT.md)。下文“候选待部署”为历史准备语境，以此状态为准。

准备材料；当前不启动。仅原测试地址进入172.30.232.42 company-test，不操作生产。部署后的候选APP/DB3030需单独批准与验收通过，旧f40采集器不能直接拿来监测新候选。

已准备候选`5f988be51779f1a22bd9d027b7503fe6a8749f62`，镜像`sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b`；业务采集器Windows/Linux各43项Mock通过。候选目录下`monitor/business-test`仅保存校准源码，没有启动worker。当前运行站点仍是f40/DB3029；须先单独批准APP-only升级及3030迁移，再批准政策和一笔真实登录窗口。

## 不再要求的逐人材料

verified_email_policy不要求参与员工提供ownership JSON，不预先导入RJGH，不调用sourceid-ownership apply。期望原user_id及权限基线从既有私有账号句柄读取；实际RJGH、可信RJEMAIL及sub来自本笔完整验证的SID UserInfo。管理员与普通用户相同流程。

## 必须真实批准的事项

1. 公司可信SID邮箱自动关联政策/版本：包含所有角色、已知异常拦截、未记录邮箱历史转让风险接受、异常处理负责人及真实引用；不是Synthetic引用。
2. 普通SSO、本地Session及自动关联政策：Access/Absolute/Idle/Admission候选60/60/30/60分钟、无Refresh、本地退出/撤销、SID缓存SSO风险及负责人。此卡不给候选期限自动批准。
3. 一笔真实登录窗口，明确允许本笔产生该原账号的自动绑定审计与本地Session；不允许真实JIT，不隐含第二次认证。
4. A正向验收独立范围由测试负责人明确批准；B完整跨空间负向验收单列，未执行记NOT_EXECUTED。

## 窗口配置（批准后临时加载）

固定：enterprise_all、oidc_sso_approved、local_approved、verified_email_policy、prompt=false、oauth_form、Refresh=false、JIT=false、统一登出关闭、AUTO_MIGRATE=false。

按真实批准加载：OIDC=true、protocol_only=false、contract/email/email_contract/auto/SSO/local政策与VERIFIED_EMAIL_POLICY_CONFIRMED=true及其真实引用。verified模式不需要填写EnterpriseMaxAge、valid_until或伪造source_ref/ownership_ref；旧trusted_ownership配置保持备份，不篡改其含义。

## 服务端及本人操作

1. 复核APP完整revision/镜像、DB3030 clean、实际进程配置、原用户/管理员/owner/空间/KB基线、单一受控来源与无并行事务。无需更改Chrome或使用18090隧道。
2. 更新并Mock校准专属采集器到**实际新候选镜像/SHA/3030**，白名单接受新政策字段、移除该模式的ownership_batch_ref要求；A须有明确正向范围批准，不跳过旧采集器B门禁冒充完整PASS。校验policy decision和local-link marker、无ownership造假、同一新Session及原权限。不使用旧f40固定worker或旧项目Session/log判定本笔结果。
3. 启用临时网关白名单观察并无凭据验证，原配置可恢复、Docker日志不持久化。再加载批准门禁；检查最终进程值后，采集器READY才通知本人点击一次OA。
4. 本人在日常Chrome打开原测试地址，点击一次“使用OA登录”，完成SID正常SSO并等待返回KnowHub；SID页面不是结束。最多等待批准的8分钟及授权事务剩余期限，超时停止、不延长旧事务，不自动重试或兑换第二次Code。
5. 严格OIDC通过→原唯一邮箱账号/稳定身份→同一原user_id与权限→Portal→auth/me→读取原KB/文档→本地退出及Session/派生Access失效。实际知识检索如需外部模型，先确认模型及原文传输边界，未执行不称完整RAG通过。B未执行保持NOT_EXECUTED。
6. 本人只反馈认证完成、Portal及安全提示；无需Network截图、Token、Cookie、完整URL或RJGH。任一签名/nonce/身份/会话/关联异常即停，不切Basic、不放宽验证、不自动第二笔。
7. 先保存脱敏阶段与政策审计，再恢复原门禁及网关配置，定向撤销本次Session和派生Access。保留真实绑定/审核记录及原账号；不自动删除，不给用户提升权限。

当前仍未执行真实业务认证。公司正式一次性全员开放和后续真实JIT分别审批，不把一笔已有账号验收当全公司或JIT PASS。
