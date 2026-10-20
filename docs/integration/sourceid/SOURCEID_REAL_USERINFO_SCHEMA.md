# 实际 UserInfo 结构（脱敏报告）

状态：**NOT_TESTED**。没有取得任何真实UserInfo，本报告没有“实际字段”或“实际脱敏样例”。以下只是待填结构记录格式，不能用于配置生产claims。

| 字段记录 | 当前值 |
|---|---|
| 实际来源/端点/请求方式 | NOT_TESTED |
| HTTP结果/JSON结构是否合法 | NOT_TESTED |
| 验签ID Token有效/nonce匹配 | NOT_TESTED |
| ID Token sub与UserInfo sub一致 | NOT_TESTED |
| 根字段名/类型/是否存在 | 未采集 |
| 嵌套对象路径/字段类型/是否为空 | 未采集 |
| 自然人工号path、string与前导零、语义确认人 | BLOCKED_EXTERNAL |
| 自然人/岗位身份字段及多身份规则 | BLOCKED_EXTERNAL |
| 员工状态path/type/字典 | BLOCKED_EXTERNAL |
| SID会话path/type/与SessionIndex等价 | BLOCKED_EXTERNAL |

获批且身份校验通过后，仅由受控服务进程遍历JSON的字段名/类型/层级输出schema，默认不输出任何值；数组记录item类型，不逐项披露；只保留存在/缺失/空值、subject相等bool、员工/状态类型符合bool。对象key若本身包含员工个人值也须省略或规范成占位名称。未知字符串一律按敏感值处理，不输出hash作“脱敏工号”（低熵hash可枚举）。

不得记录ID/access/refresh token、secret、code、nonce/state值、工号、邮箱、证件号、TGT、完整sid/SessionIndex、原始Profile/HTTPbody。证据编号与请求ID可脱敏关联，不导出cookie/HAR。

飞书文档仅说明示例Profile含sub/id/auth_time/attributes，attributes示例含login/gid/zjhm等；这些**不是本次实际结构，更不是企业自然人工号证明**。即使真实返回同名字段，也须HR/SID确认语义。字段类型符合不证明可信自然人身份。

当前登录接口不提供完整schema诊断，未知审核映射会失败。需要SID官方脱敏结构或另行评审的验签后只读诊断才能补足；不靠关闭SOURCEID_ENABLE、nonce或签名校验取信息。
