# 单笔真实SID协议验收：最终执行前检查

2026-10-16，北京时间14:28。结论：**SERVER_READONLY_PASS / EXECUTION_PENDING**。

本轮没有启动运行适配器、开启prompt、调用authorize/Token或修改运行APP、数据库、网络和企业门禁。尚未形成新的真实协议验收结果。

## 当前现场

| 项目 | 实测结果 |
| --- | --- |
| 目标 | 172.30.232.42，仅打包测试机 |
| APP revision | 479416c9086d91de4484dbbd2cc5f912cdcdb697 |
| APP镜像 | sha256:85f9d6c052298e7f52e7d7c4533e131ce19326d461521378e90c6df3b01fa49c |
| Frontend revision | 1ba24ef5f9a730bb98d9ade6a5a5d141ea49292b |
| DB | 3027，dirty=false |
| frontend/app/proxy config | HTTP200；APP healthy |
| prompt / protocol_only | false / true |
| contract/email_binding/auto_binding/recheck/logout | false |
| Session模式 | NOT_SET，默认external，未启用local_approved |
| Basic | oauth_form |
| OIDC / 专项诊断 | true / true，保持现场原值 |
| AUTO_MIGRATE | false |
| 有效未消费事务 | 0 |
| 用户/身份绑定/本地企业Session数量 | 2 / 0 / 0 |

APP及Frontend容器ID与校准时记录一致。五层Compose顺序与原部署收据一致，原恢复文件、数据库备份及APP-only回滚pin存在；本轮未执行恢复或重新备份。

14:28:43只读流量快照：最近5分钟无OA相关请求，无已知测试运行器。恢复后的历史仍为两次授权入口200及两次callback302，没有新增观察。仅能证明本次观察范围安静，不能证明未来独占、识别操作者或排除其他客户端。

## 监测器及恢复核对

四个protocol_monitor模块、现有export_token_diagnostics及probe_report_filter的Git blob均与审核提交88c884a4200ef6c4849eb7500c9ec1c819b08a26一致。没有使用历史旧worker，尚未上传或启动本次运行器。

复核已通过的28项合成控制测试及Python3.8/3.12 CI，不重复认证开发。校准覆盖：run_id、唯一新增事务、分离的authorize/callback Trace、第二笔流量冲突停止、固定枚举白名单、体积上限、密码页面继续等待、事务T0起480秒等待且不超过原期限、callback证据先保存后恢复、APP-only恢复false及门禁/计数核对。就绪等待首笔授权另限120秒。

上述恢复能力是Mock验证及代码核查，不是本轮真实配置切换证据。SIGKILL、主机宕机或Docker不可用仍须人工APP-only恢复，不承诺进程能覆盖所有故障。

## 尚待执行现场确认

1. SID负责人当前在线，可按本次脱敏时间窗口核对prompt接收、忽略原SSO、重新认证及员工准入。
2. 用户现有Chrome仍有有效SID SSO，且此前已验证的KnowHub页面、静态资源、API、callback测试机路由未发生变化。服务器健康检查不能证明日常Chrome路由；不索取Cookie、Token、完整URL或Network截图。如路由存在疑问，停止执行。
3. 明确安排连续8分钟、无其他OA认证测试窗口；单进程锁不能替代流量隔离。任何第二授权或无法归属的callback都按CORRELATION_AMBIGUOUS停止，不能报告PASS。
4. 用户明确发出“开始本次真实测试”。确认现场条件不等于该执行授权。

上述确认已向用户询问，尚未收到本轮答复。等待期间保持prompt=false，不提前创建授权事务；执行前再次核对活动事务及版本，快照不能作为永久有效状态。

## 授权后的最小操作顺序

Codex先提供审核模块及匹配过滤器，携带执行授权和独占窗口两个显式门禁启动新run_id；适配器临时第六层仅启用prompt=true，APP-only重建并核验后进入READY。用户才点击一次OA，完成本人认证、等待返回KnowHub，反馈“认证完成”及安全提示。密码页面只是中间进度，不触发恢复。

既有APP收到callback后立即一次性兑换Code；监测器不发送authorize/Token、不重试、不等待聊天才兑换。SID Code90秒与本地480秒人工认证等待分开，不以state创建时间推断Code签发时间。

采集state、Token、严格ID Token/nonce、UserInfo、签名auth_time固定分类及protocol_only。页面跳转或Token200均不能单独判定PASS；缺失阶段按NOT_EXECUTED/NOT_CAPTURED区分，SID端尚未确认单独PENDING。

完成、取消、超时或失败时先落盘脱敏证据，再恢复原五层APP及prompt=false，核对原镜像、DB、门禁、其他容器及账户/企业Session；仅定向处理本次明确归属的未消费事务。不得自动第二次登录，不开启业务门禁。

## 本轮收据

- [现场版本、门禁、DB及恢复文件](evidence/protocol-monitor-final-preflight-20261016.json)
- [当前流量及已知进程](evidence/protocol-monitor-final-traffic-20261016.json)
- [审核源码一致性](evidence/protocol-monitor-final-source-check-20261016.json)
- [校准与合成验收报告](SOURCEID_PROTOCOL_MONITOR_CALIBRATION_REPORT.md)

最终状态：只读预检完成；本次真实协议事务**NOT_STARTED**。等待现场条件及明确执行确认，不能归档为真实协议PASS。
