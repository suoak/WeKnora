# 正式一次性全员开放 GO / NO-GO

> **2026-10-18 最新结论优先：** [首次真实 OA 验收收口](SOURCEID_REAL_OA_ACCEPTANCE_CLOSEOUT.md)。
> 原账号 OA 登录、原权限和文档访问已完成人工正向 PASS；退出后企业有效 Session/Access 均为 0，
> 无 Refresh。自动 Trace 为 PARTIAL/NOT_CAPTURED，跨空间负向、真实到期与真实 JIT 尚未执行。
> 当前测试 APP 为 5f988be51、DB3030 clean，业务配置按获批测试政策保持生效。
> 已采用 verified_email_policy，逐人 ownership JSON/独立 RJGH 历史证明不再是该模式的前置条件；
> 下文旧的“真实业务 NOT_EXECUTED”、旧版本、关闭配置及逐人归属阻塞说明仅作历史记录，不作为当前门禁。
> 正式一次性公司全员发布仍 NO-GO，具体剩余项目以上述收口报告为准，不增加事业部或管理员单独流程。

> 首笔账号准备最新状态见[历史核验与观察接线](SOURCEID_FIRST_ACCOUNT_HISTORY_AND_READINESS.md)：39项校准及无凭据网关联通PASS，不是新的真实OIDC/业务PASS；原账号连续性已核实，仍需独立RJGH归属及真实政策批准。公司正式发布条件与首笔单账号验收分开，不要求先完成全公司导入或JIT审批才能准备该笔已有账号验证。

产品基线（2026-10-17更新）：一次性向符合公司企业身份条件的全公司员工开放；不限制事业部、部门、组织或角色，enterprise_all无固定人数上限、无线上逐人名单。已有账号保留user_id与全部权限，新员工经获批公司级SID企业身份及精确邮箱域策略JIT成为无特权tenantless普通账号。事业部范围确认与组织字段校验开发已取消，不是上线阻塞。

当前发布结论：**NO-GO**。独立环境技术通过不等于生产授权或真实账号验收通过。

| 项目 | 状态与放行要求 |
|---|---|
| RJGH/RJEMAIL | USER_CONFIRMED：稳定唯一不回收/可信企业邮箱，无需重新确认 |
| 源码/CI | PASS：f40b1602f四项CI成功；独立APP实际revision/镜像及003029资源核对通过，Frontend保留832b6652a |
| 数据完整性/回滚 | PASS：DB3029 clean，原数据/文件/ACL一致；09 APP在3029兼容并实际回滚恢复通过，见本轮最终验收报告 |
| 历史归属数据 | BLOCKED：数据负责人、可信历史邮箱归属、原user_id/RJGH映射、来源/版本/核验时间/有效截止；不以OIDC成功反向证明历史归属 |
| 批次接入 | 合成dry-run/apply/幂等已验证；真实批次未提供，真实dry-run待数据，apply另行授权 |
| 新员工开户资格 | BLOCKED：SID应用实际允许的企业账号类型、公司正式批准的完整精确邮箱域集合/开户资格、全量历史账号核验完整性、异常处理人及真实JIT政策引用。现有配置支持逗号分隔多域，不新增组织字段校验，不要求事业部限制 |
| 目录维护 | BLOCKED：全量及增量接口或受保护批次交付、身份变更/邮箱转让/停用/撤销事件、最大延迟、失败恢复和责任人 |
| 新鲜度 | BLOCKED：EnterpriseMaxAge与valid_until由真实同步周期决定；E=min(verified_at+MaxAge,valid_until)，同版本重放不刷新核验时间 |
| 会话审批 | BLOCKED：60m Access/60m Absolute/30m Idle/60m AdmissionMaxAge仅候选；无Refresh、定向撤销和本地退出，策略批准引用与责任人需正式确认 |
| 普通SSO风险 | BLOCKED：具名接受人；复用SID SSO不保证实时员工有效性，本地退出不等于SID统一登出；D未知非0，停用前Code90秒内仍可兑换 |
| 新账号权限 | USER_CONFIRMED：无默认空间/敏感KB/管理权限；公共知识仅通过KnowHub共享ACL单独授予，允许登录不是全知识访问授权。实际无特权及共享边界仍需真实JIT验收 |
| 真实业务验收 | NOT_EXECUTED：分别批准已有账号关联和真正新员工开户；覆盖Portal/auth-me/原KB读取/越权拒绝/重复登录/退出/撤销 |
| 实际Chrome路由 | 原测试80/8080获批替换后无凭据PASS：页面/资源/API/callback均进入新f40/832/DB3029环境，Windows直接原域名实测，无18090隧道要求；未执行日常SSO真实授权，真实窗口仍需路由明确 |
| 容量 | 独立10场景4300成功/100预期拒绝保留。原300–500人是旧规划，不代表公司总人数；全公司预计用户数、最忙一分钟登录人数、新旧比例、正式规格、SID限流/并发及成功率/P95/P99阈值UNKNOWN，须据此重定生产容量目标。PG主池无显式MaxOpenConns，WaitCount/WaitDuration为NOT_CAPTURED，须明确连接预算与观测 |
| 真实业务监测 | 新knowhub-company-test目标Mock PASS：Windows/Linux各30项、原28项兼容回归通过；原80/8080/callback只读复核PASS，不使用旧项目日志/Session。真实窗口前仍需观察通道及审批齐备；原测试有效SID配置已受控加载但OIDC关闭，不是新的认证PASS |
| 检索/RAG | 原文件与已有向量读取可验证；模型/AES恢复、外部Embedding/重排/BM25/完整RAG需批准配置及内容传输边界后验证 |
| 发布/恢复 | BLOCKED：具名发布、数据、网络、安全、SID、回滚负责人和维护窗口；生产端口/回调/备份/迁移/写入冻结与成套DB文件回退预演，生产操作另行授权 |

正式目标配置为enterprise_all、oidc_sso_approved、local_approved、prompt=false、Refresh=false。其余contract/email/auto/enterprise/JIT/SSO政策门禁仅在上述条件成立且真实审批引用就绪后按操作卡加载；不能全部设true或填写虚构政策号。

公司级策略不是生产发布、真实归属apply或真实认证授权。已另获批准将打包机原80/8080指向新的f40/832/DB3029副本，全部真实身份业务门禁仍关闭，原独立试点保留；见[部署报告](KNOWHUB_COMPANY_TEST_ENTRY_DEPLOYMENT_REPORT.md)。剩余阻塞以[最新READY报告](KNOWHUB_REAL_OA_BUSINESS_ACCEPTANCE_READY_REPORT.md)为准。

严重错绑、越权、重复用户或系统性登录故障：立即关闭OA自动绑定/JIT/业务准入、定向撤销企业Session和派生Access，保存审计后按APP-only固定镜像回滚；不降迁、不恢复raw、不删除新用户/审核记录掩盖问题。业务写入后如需恢复DB，必须使用同代文件并先处理新增写入差异。

达到上述条件后再单独申请一次真实已有账号和一次真实JIT业务验收；通过后另行申请正式一次性发布。本轮不自动进行任何真实认证。

首笔验收范围更正：以上全量数据/JIT/公司容量要求属于正式公司发布，不要求在一次已有账号验证前全部完成。首次只核验并dry-run一条可信历史归属，独立批准apply及会话/有效期后再批一笔真实事务，JIT关闭；见[单账号操作卡](SOURCEID_FIRST_EXISTING_ACCOUNT_OPERATION_CARD.md)。不因测试账号是管理员另设申请或登录逻辑。
