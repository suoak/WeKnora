# 首笔原账号真实OA业务验收操作卡

> 2026-10-17本轮更新，以[原账号历史核验与接线结果](SOURCEID_FIRST_ACCOUNT_HISTORY_AND_READINESS.md)为最新执行依据。下文30项/观察通道待接通为上一轮记录：现为39项Windows/Linux校准PASS，当前网关无凭据401观察PASS并恢复原配置。原账号UID、邮箱、创建信息、管理员标记和成员关系与原库一致；32条历史审计仅计数，不能证明RJGH归属。仍缺独立单行证明、真实政策批准及有效负向夹具；未apply、未认证、门禁未变。Portal由本人确认，API由临时网关白名单观察，不再依赖浏览器IPC或Network截图。具体2h单次归属期限及60/60/30/60分钟均仅审批建议。

2026-10-17。仅172.30.232.42原测试地址；本轮未重建服务、未改Chrome、未apply归属、未执行真实SID认证。**本卡只准备一名已有账号，JIT第二轮；不要求先导入公司全量归属数据。** 普通账号和管理员使用同一规则。

## 五项就绪结论

| 用户问题 | 当前结论 |
|---|---|
| 业务采集器是否READY | **新目标Mock校准READY**：Windows/Linux各30项PASS，原28项兼容回归PASS。固定knowhub-company-test、APP f40、DB3029、80/8080；不接受旧独立试点的日志/Session。真实运行尚未启动；Portal/API受保护观察通道仍需启动前接通，缺失时拒绝READY/PASS，不能把Mock当真实闭环 |
| 被测已有账号归属是否READY | **NOT_READY**：受保护邮箱在新库唯一命中1个启用历史账号，系统管理员=true、全局跨租户标记=false，1条owner成员，所属空间中1个有效原KB；原user_id仅在受保护进程/文件内保留。不把邮箱命中解释为SID历史归属；独立核验的RJGH、证明引用及有效时间尚未提供 |
| 会话政策是否READY | **NOT_APPROVED**：60/60/30/60分钟仍候选，EnterpriseMaxAge及valid_until未批准，没有填入真实政策引用 |
| 还需用户确认什么 | 指定核验人并经受保护渠道交付单账号RJGH/历史证明/时间；确认下表会话与一次性归属期限、风险及真实批准依据；之后分别批准该行apply和一笔真实登录窗口。无需公司全量数据或JIT政策才能验证此已有账号 |
| 能否开始第一次真实OA登录 | **NO-GO**：上述归属/政策未齐。路由已准备，无18090/Chrome隧道阻塞；门禁继续关闭 |

证据：[新项目采集校准](evidence/company-first-monitor-calibration-20261017.json)、[单账号只读核对](evidence/company-first-account-readonly-20261017.json)。截至本轮新项目无有效未消费授权事务，外部身份、归属、JIT及企业Session均0。当前APP f40b1602f、Frontend832b6652a、DB3029 clean，原80/8080/空参数callback已重新核验；未借用旧项目数据推断成功。

## 单账号归属准备

已安全保存待测原user_id内部句柄，不在报告输出邮箱/工号。通过原数据/权限基线继续核验原管理员标记、tenant_id、owner成员及KB/ACL；不创建用户或空间、不重设密码、不提升权限。

数据负责人仅需准备**这一行**真实归属：正式issuer、RJGH、可信企业邮箱、已选原user_id、独立历史ownership_ref、权威source_ref、真实policy_ref、revision、active、实际verified_at、valid_until。不能从当次Token、邮箱唯一或拟执行登录反向生成证明，也不猜测RJGH。聊天中提供的邮箱只用于查找，不是这份证明。

受保护行放在测试机0700目录、0600文件，通过已有安全渠道录入；不放命令参数、Git、CI或普通日志。[限定单账号dry-run工具](business-test/company-first-ownership-dryrun.py)固定新PG/镜像、核对选定user_id，拒绝缺失/过期/测试引用；没有`--apply`路径，实际调用原`sourceid-ownership`默认dry-run，只输出1行汇总。无证据时默认NOT_EXECUTED。**本轮实际归属dry-run为NOT_EXECUTED（缺RJGH/独立证明），不是PASS；包装器安全分支已用合成输入校准。** 真实apply必须后续独立批准，工具不会自动执行。

## 明确审批项目（当前全部待正式确认）

| 项目 | 候选/需填写事项 |
|---|---|
| Access | `SOURCEID_LOCAL_ACCESS_TTL=60m` |
| 本地绝对期限 | `SOURCEID_LOCAL_ABSOLUTE_TTL=60m`；SwitchTenant/刷新页面不延长 |
| 空闲期限 | `SOURCEID_LOCAL_IDLE_TTL=30m` |
| 准入证明期限 | `SOURCEID_ADMISSION_MAX_AGE=60m`，普通SSO不等于实时员工状态检查 |
| 归属新鲜度 | `SOURCEID_ENTERPRISE_MAX_AGE`需明确数值；这一行verified_at必须是实际独立核验时间 |
| 来源截止 | 提供带时区的真实valid_until；`E=min(verified_at+EnterpriseMaxAge, valid_until)`，到E拒绝，不自动改写证明时间。若要完整60分钟会话，需确认E覆盖该会话；不把会话候选自动当作归属期限 |
| 一次性证据维护 | 首笔可用单账号受控证明，不要求先建公司全量同步；须确定本笔证明更新/到期/撤销负责人、恢复预算，到期不能静默续用 |
| 普通SSO残余风险 | 接受复用已有SID SSO不保证停用实时复核，本地退出不等于SID统一退出；无Refresh |
| 批准依据 | 自动关联、SSO/Admission、Local Session、Enterprise Ownership的真实引用与责任人，可由同一实际批准记录覆盖多项；不生成Synthetic编号 |

## 获得单独授权后的唯一执行顺序

1. 再核对f40/832/DB3029、新项目无并行认证；新入口仅获准工作站可达。记录本账号私有权限基线和当前所有门禁，不修改原数据库或独立试点。
2. 对真实单行执行dry-run，冲突0后**另获apply批准**再导入；导入只准备归属，不预建OIDC身份绑定。不因该账号为管理员增加不同流程。
3. 只在批准窗口加载：enterprise_all、oidc_sso_approved、local_approved、OIDC=true、protocol_only=false、contract/email/email_contract/auto/SSO/enterprise批准开关及真实引用；诊断临时开启。保留oauth_form、prompt=false、Refresh=false、JIT=false、authorization_recheck=false、统一Logout=false、AUTO_MIGRATE=false。未批期限或引用缺失即停止。

   AUTO_BINDING仍是全局策略，不是单用户白名单。首笔范围由获准工作站入口、独占时间窗口、仅一行已核验归属及JIT=false共同约束；并行认证或其他新Session出现即停止，不以“只测试一个人”代替技术边界。`scope_evidence_ref`只引用公司身份/受控测试窗口依据，不要求部门证明。
4. 以新采集器启动且观察通道就绪后才通知点击；采集器只认新项目唯一新增事务、新callback Trace和新Session。用户日常Chrome打开**原测试地址**，点击一次OA，完成认证，返回Portal。看到SID页面不是结束；人工认证等待≤8分钟且不超过本地事务期限，超时不延长/重试。
5. 严格OIDC各阶段PASS，核对原user_id、所有原管理标记、tenant/成员及ACL不变；Portal、auth/me200、原KB及文档读取、无Refresh、退出旧Access失效逐项取证。签名/nonce/归属失败立即停止，不切Basic、不放宽校验，不自动发起第二笔。普通SSO不要求prompt/auth_time强制新鲜度证明。
6. 当前原库只有1个有效KB，位于该账号已有成员空间；**尚无可直接用于该账号负向验收的空间外有效KB**。不能用随机ID/已删除KB代替。完整越权用例需另行批准准备一项真实存在、未共享的合成对照KB；若系统管理员按既有授权本就可读该资源，按实际权限断言，不能为追求403修改原权限。原Mock跨空间拒绝仍PASS，但不冒充此账号真实负向实测。
7. 用户反馈“认证完成”及最终页面；Portal/UI观察与服务端API证据分开标记，缺观察为NOT_CAPTURED，不能据Token200推定登录成功。无需提供Network截图、Cookie、Token或完整URL。采集器不读取浏览器Cookie；当前受保护观察IPC未接通时不会冒称真实READY。
8. 先保存脱敏证据，再去掉一次性覆盖，仅重建新APP恢复原关闭门禁；定向撤销本次可唯一归属的企业Session及派生Access，核对权限和用户数量不变。保留本次真实绑定/审计，不自动删除用户/归属。关联歧义或恢复失败报告FAIL并定向处理，不批量撤销其他人的Session。

预期：此次不新增用户，绑定1个原账号；原账号和权限完整，Access-only登录后可用原KB。再次登录、退出后重登和JIT各是**额外真实事务**，均需明确批准，不隐含在首次一笔中。现阶段不要求全公司导入、公司JIT域政策或容量复测才能准备这一笔；正式全公司发布仍按原GO/NO-GO另行审批。
