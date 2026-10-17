> 2026-10-18: Candidate collector supports explicitly approved A_positive scope; 43 Mock tests pass on Windows and Linux. Full scope remains the default, and B is NOT_EXECUTED for A_positive. No real window or scope approval has been loaded. See SOURCEID_VERIFIED_EMAIL_FIRST_REAL_LOGIN_CARD.md.

# 首笔原账号 OA 验收：最小确认单

> 后续产品要求已调整为[可信SID邮箱政策自动关联](SOURCEID_VERIFIED_EMAIL_AUTO_LINK_DESIGN.md)。本单逐行证明与apply要求仅适用于原trusted-ownership路径，不再作为公司员工逐人接入要求。新模式尚在设计与隔离验证，尚未修改运行配置；改用获批的公司级关联政策，不生成虚假source_ref/ownership_ref。

状态：待人工核验及政策确认。无真实归属 apply、无真实 SID 登录；JIT关闭。仅限打包测试机 `172.30.232.42` 的 `knowhub-company-test`，不是生产放行。

## 1. 一份单账号受保护核验记录

| 谁确认 | 最少确认内容 | 需要保留的依据 |
|---|---|---|
| SID身份负责人或获授权人员目录负责人 | 本人与稳定RJGH、经验证企业邮箱对应 | 权威资料引用、核验人、实际核验时间 |
| 原KnowHub数据负责人 | 已选原user_id属于同一人，历史邮箱归属无已知转让或冲突 | 历史管理依据引用、核验人、实际核验时间 |

同一人具备两方面授权时可以合并一份确认；所有角色均使用此规则，不另设管理员申请流程。

数据负责人通过现有受保护渠道查看测试机 `private/first-account-preparation/history-review.json`；该摘要仅证明原账号连续性，不是独立人员证明。现有私有句柄可填充原user_id和邮箱，无需用户在聊天重发。RJGH、证明原文和完整单行数据只通过受保护渠道交付，不进入命令参数、Git、CI或普通日志。

记录须有真实 `source_ref`、`ownership_ref`、核验人、带时区的实际核验时间V及有效截止；不能从拟执行登录、邮箱命中或用户口头确认反向生成证明。当前尚未取得这份独立核验结果，不填写核验时间。

## 2. 单次测试政策确认（全部待批准）

| 项目 | 建议值/确认内容 |
|---|---|
| Access / Absolute / Idle / AdmissionMaxAge | 60m / 60m / 30m / 60m |
| EnterpriseMaxAge | 2h，仅本次单账号窗口，不是公司目录同步SLA |
| verified_at / valid_until | 实际独立核验时间V / V+2h；须核验负责人接受该截止 |
| 时间预算 | 最迟V+45m启动；认证等待不超过8分钟及授权事务剩余期限；不足则停止、重新核验，不延长旧证明 |
| Refresh / 强制重新认证 / JIT | 全部关闭；普通SID SSO |
| 风险接受 | 普通SSO可能复用既有SID会话，不提供实时员工有效性复核；本地退出不等于SID统一退出；不把停用传播延迟写为0 |
| 责任及引用 | 真实政策依据、风险接受人、测试负责人、执行人、异常联系及定向撤销负责人 |

上述期限须结合实际V和批准时间填写最终截止，不以文件生成或导入时间替代V。不虚构审批编号；一份真实批准可以明确覆盖自动关联、本地会话、普通SSO和本次归属期限。

## 3. A与B分别验收，不混算PASS

**A：首次真实正向业务验收。** 单笔authorize及Code兑换；严格OIDC验证；原user_id、系统管理员及owner权限保持；Portal、auth/me、原KB/文档读取；本地退出与Session/派生Access撤销。此次不创建用户、不调整权限。原知识检索若依赖尚未批准恢复的外部模型，应单列未执行，不能凭文档读取宣称完整RAG通过。

**B：完整跨空间负向权限验收。** 另行准备真实存在、按该账号实际权限应拒绝的非敏感资源与授权基线，单独批准及执行。不得用随机ID、已删除KB或降低原管理员权限制造403；未执行就是NOT_EXECUTED。

现有采集器仍要求完整正负向证据。**本轮未修改该门禁。** 若测试负责人批准A独立运行，应明确批准“A不等待B，但仅报告A，B保持NOT_EXECUTED”，之后才作限定验收范围的最小适配及Mock验证，不以操作时跳过检查替代批准。

## 4. 按顺序通知下一步

1. 独立证明和真实政策齐备：组装一条受保护归属记录，执行既有默认dry-run；只通知脱敏冲突/变更计数，不自动apply。
2. dry-run无冲突：通知可以**单独批准该行真实apply**。apply不会预建OIDC绑定，首次登录仍走统一认证和自动关联。
3. A范围明确批准并完成必要校准、该行获批apply、政策与时间预算有效：通知可以**另行批准一笔真实SID业务登录**。不把apply授权当登录授权。
4. 窗口获批后：复核无并行事务和原权限基线；临时启用并校验已Mock通过的网关白名单通道，确认日志不持久化；READY后才通知本人在原测试地址点击一次OA。
5. 本人完成认证并返回KnowHub，按A读取原知识、退出；失败停止、不自动重试、不降低OIDC校验。先保存脱敏审计，再恢复网关及原业务门禁，定向撤销本次企业Session与派生Access；不删除原账号、真实绑定或审核记录。

当前可推进动作：由上述授权负责人交付单账号受保护核验记录，并确认本单政策及A独立验收范围。未齐备前不启动窗口，不要求全公司归属数据或第二轮JIT资料。
