# 正式一次性全员开放 GO / NO-GO

产品基线：一次性向符合条件的全事业部员工开放；enterprise_all无固定人数上限、无线上逐人名单、所有角色同一认证/归属流程；已有账号保留user_id与权限，新员工经独立批准JIT成为无特权tenantless普通账号。

当前发布结论：**NO-GO**。独立环境技术通过不等于生产授权或真实账号验收通过。

| 项目 | 状态与放行要求 |
|---|---|
| RJGH/RJEMAIL | USER_CONFIRMED：稳定唯一不回收/可信企业邮箱，无需重新确认 |
| 源码/CI | PASS：f40b1602f四项CI成功；独立APP实际revision/镜像及003029资源核对通过，Frontend保留832b6652a |
| 数据完整性/回滚 | PASS：DB3029 clean，原数据/文件/ACL一致；09 APP在3029兼容并实际回滚恢复通过，见本轮最终验收报告 |
| 历史归属数据 | BLOCKED：数据负责人、可信历史邮箱归属、原user_id/RJGH映射、来源/版本/核验时间/有效截止；不以OIDC成功反向证明历史归属 |
| 批次接入 | 合成dry-run/apply/幂等已验证；真实批次未提供，真实dry-run待数据，apply另行授权 |
| 新员工开户资格 | BLOCKED：SID身份与事业部范围/允许企业域/开户资格、全量历史账号核验完整性、异常处理人，真实JIT政策引用。公司邮箱域不等于事业部范围；须确认SID应用准入是否已限定所需事业部，或由产品明确批准更广开户范围，不能自行把企业域匹配称为事业部资格校验 |
| 目录维护 | BLOCKED：全量及增量接口或受保护批次交付、身份变更/邮箱转让/停用/撤销事件、最大延迟、失败恢复和责任人 |
| 新鲜度 | BLOCKED：EnterpriseMaxAge与valid_until由真实同步周期决定；E=min(verified_at+MaxAge,valid_until)，同版本重放不刷新核验时间 |
| 会话审批 | BLOCKED：60m Access/60m Absolute/30m Idle/60m AdmissionMaxAge仅候选；无Refresh、定向撤销和本地退出，策略批准引用与责任人需正式确认 |
| 普通SSO风险 | BLOCKED：具名接受人；复用SID SSO不保证实时员工有效性，本地退出不等于SID统一登出；D未知非0，停用前Code90秒内仍可兑换 |
| 新账号权限 | 待产品确认：默认无空间/KB/管理权限；公共知识与默认空间如需开放必须另行明确正常授权策略，不能默认继承敏感资源 |
| 真实业务验收 | NOT_EXECUTED：分别批准已有账号关联和真正新员工开户；覆盖Portal/auth-me/原KB读取/越权拒绝/重复登录/退出/撤销 |
| 实际Chrome路由 | NOT_VERIFIED：无凭据专用浏览器隧道验证不等于日常SSO Chrome；页面/资源/API/callback必须同源进入18090独立项目 |
| 容量 | 独立10场景功能及合成到达率复测通过，生产容量仍待确认。300–500人USER_CONFIRMED；实际峰值、正式规格、SID限流/并发与验收门槛待确认；PG主池无显式MaxOpenConns，客户端WaitCount/WaitDuration为NOT_CAPTURED，须明确生产连接预算与观测 |
| 真实业务监测 | 待业务采集适配校准：旧protocol_only worker硬编码默认项目且会拒绝local_session，不得直接用于独立项目业务验收；复用现有白名单并在Mock校准后才可READY，不修改OIDC主流程 |
| 检索/RAG | 原文件与已有向量读取可验证；模型/AES恢复、外部Embedding/重排/BM25/完整RAG需批准配置及内容传输边界后验证 |
| 发布/恢复 | BLOCKED：具名发布、数据、网络、安全、SID、回滚负责人和维护窗口；生产端口/回调/备份/迁移/写入冻结与成套DB文件回退预演，生产操作另行授权 |

正式目标配置为enterprise_all、oidc_sso_approved、local_approved、prompt=false、Refresh=false。其余contract/email/auto/enterprise/JIT/SSO政策门禁仅在上述条件成立且真实审批引用就绪后按操作卡加载；不能全部设true或填写虚构政策号。

严重错绑、越权、重复用户或系统性登录故障：立即关闭OA自动绑定/JIT/业务准入、定向撤销企业Session和派生Access，保存审计后按APP-only固定镜像回滚；不降迁、不恢复raw、不删除新用户/审核记录掩盖问题。业务写入后如需恢复DB，必须使用同代文件并先处理新增写入差异。

达到上述条件后再单独申请一次真实已有账号和一次真实JIT业务验收；通过后另行申请正式一次性发布。本轮不自动进行任何真实认证。
