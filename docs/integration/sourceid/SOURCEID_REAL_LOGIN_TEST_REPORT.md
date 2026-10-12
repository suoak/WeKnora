# 真实 OIDC 登录测试报告

日期：2026-10-12。结果：**NOT_TESTED / BLOCKED_EXTERNAL**。

没有启动SID授权流程、使用员工凭据、交换真实code、取得真实token或创建真实绑定。用户持有Client Secret不构成本轮已测试配置正确的证据。

| 项目 | 结果 | 待取得证据 |
|---|---|---|
| 正式issuer/授权/Token/Profile/JWKS配置 | BLOCKED_EXTERNAL | SID签署的非秘密完整地址及元数据；基地址sid.ruijie.com.cn不能替代issuer |
| Client ID/Secret有效性 | NOT_TESTED | 一次获批授权码交换成功；报告不保留secret |
| 浏览器授权码流程/code短TTL | NOT_TESTED | 回调和兑换的脱敏时间线/请求ID |
| state一次消费/nonce/PKCE真实兼容 | NOT_TESTED | SID回传及验证bool，不保存state/nonce/verifier原值 |
| ID Token alg/JWKS/RSA签名 | NOT_TESTED | 实际alg、kid是否匹配与签名bool；示例RS256不算实际alg |
| iss/aud/azp/exp/iat/nbf/sub | NOT_TESTED | 每项验证结果，时钟偏差说明；不保存原JWT |
| UserInfo请求/subject一致 | NOT_TESTED | 请求方式及HTTP结果、subject_equal=true；不保存token/个人值 |
| 自然人工号/状态/岗位身份 | BLOCKED_EXTERNAL | HR/SID签署字段含义和状态字典，不能仅以返回类型推定语义 |
| 原KnowHub账号复用/权限保持 | NOT_TESTED | 已审批映射、原ID等同bool、权限diff |
| 无敏感日志/无URL本地JWT | NOT_TESTED（真实流） | 全链路日志验收及浏览器路径观察，不导出HAR |

自动化证据来自[已验证CI](https://github.com/suoak/WeKnora/actions/runs/38242327216)，仅说明本地支持范围，不能替换以上真实结果。

下次执行记录表（仅填脱敏结果）：环境/commit；用户批准时间/范围；执行时间；请求关联ID；正式端点（无敏感参数）；算法和校验bool；错误安全类别；原账号与权限比较；证据受限链接；PASS/FAIL/NOT_TESTED；清理/撤销结果。流程与通过标准见 [A–I用例A/B](SOURCEID_P6_E2E.md)。

停止条件：环境不隔离、SID URL/证书不可信、签名/nonce/sub失败、日志泄露、身份冲突或缺会话来源。停止后不关闭校验获取员工信息，不把失败Token decode成已可信证据。
