# SID 单笔协议监测器校准与合成验收

2026-10-16。结论：**SYNTHETIC_CONTROL_PASS / READY_CONDITIONAL_ON_EXCLUSIVE_WINDOW / NO_REAL_TEST_STARTED**。

代码提交：`88c884a4200ef6c4849eb7500c9ec1c819b08a26`。本轮只改Python监测控制与测试，没有改OIDC认证代码、运行APP/Frontend、数据库或安全配置，也没有启动真实监测适配器。APP仍479416c90、Frontend1ba24ef5f、DB3027，prompt=false，企业放行门禁保持原状。

## 1. 实现清单

| 文件 | 职责 |
| --- | --- |
| `business-test/protocol_monitor.py` | 无I/O的状态机、run_id、唯一事务和Trace关联、等待/超时、安全结果分类及收尾顺序 |
| `business-test/protocol_monitor_runtime.py` | 受控打包机适配器：白名单日志、只读事务观察、APP-only临时true与恢复false、账户/会话基线核对、定向清理 |
| `business-test/protocol_monitor_signal.py` | Codex将用户明确进度转成固定枚举；不接收原URL、Token或自由文本 |
| `business-test/protocol_monitor_test.py` | 28项纯合成/Mock适配器控制测试 |
| `.github/workflows/sourceid-monitor.yml` | 独立Python3.8/3.12 CI，不构建或部署KnowHub |

原 `prompt-login-real-once-worker.py` / `...stop-remote.py` 只保留为历史证据，不再作为下一次运行器。没有回滚或替换已验收APP，不重复OIDC核心开发。

## 2. 单笔关联与安全停止

- 服务器随机生成run_id。开始前保存原事务集合于私有内存，要求没有有效未消费事务；观察唯一新增事务并绑定。事务主键是内部定位句柄，不进入日志、报告或持久化收据，不导出state哈希、nonce、verifier或Result原文。
- 当前SourceID授权事务的服务器生成元数据只取prompt布尔、创建时间和到期时间；不将创建时间当SID Code签发时间。
- authorize及callback的APP原生Trace不相同。监测器明确记录两个Trace，基于唯一新事务、其消费、唯一callback和受控窗口关联，不伪造共享APP Trace。
- 出现第二授权、第二新事务、不同callback Trace或不能归属的回调，终止并报CORRELATION_AMBIGUOUS。没有捕获到未消费状态、事务先于轮询被消费，也安全拒绝关联，不猜测。
- 首笔callback_state与Token/ID Token/UserInfo阶段按顺序断言。Token或签名失败停止，不由监测器发新请求；local_session出现或预期门禁异常视为安全失败。
- id_token PASS仅依据部署479416c90中在签名、Issuer/Audience/时间/nonce之后发出的安全事件；不读取真实Token。nonce结果标明来自此严格流水线，不宣称新增独立nonce事件。
- 签名auth_time仅接收现有 `sourceid_reauthentication` 固定枚举事件，UserInfo字段不参与。缺失事件记NOT_CAPTURED，不能宣称完整观测；无字段枚举NOT_PROVIDED_CONTRACT_REQUIRED与校验PASS分开保留。

## 3. 等待与结束规则已校准

就绪后先最多等待120秒出现首笔授权；绑定后从服务端事务创建T0起最多480秒，即8分钟，且不得超过事务到期。当前本地事务10分钟，创建后轮询时剩余期限不足480秒则停止，不延长事务。

| 输入或状态 | 行为 |
| --- | --- |
| 已出现SID密码页面 | 继续等待，不结束、不恢复、不失效事务 |
| 占位模板或模糊消息 | 不当作完成；Codex只询问实际进度，原截止不变 |
| 用户明确认证完成、尚无callback | 继续在原截止内等callback，不新发authorize |
| callback完成、用户尚未确认 | 立即保存证据，仍等待完成反馈或原截止 |
| callback符合严格协议及预期门禁，用户确认完成 | 正常收尾；SID端执行确认可另记PENDING |
| 明确取消、超时或技术/安全失败 | 安全停止，保存证据并恢复，不重试 |

SID Code仍自签发起90秒、单次兑换；本地人工等待不是Code期限。callback收到后由现有APP立即兑换，监测器不等待聊天反馈才兑换、不触碰请求内容、超时、Basic或重试策略。

## 4. 白名单、体积与恢复

适配器复用既有 `export_token_diagnostics.py` 和 `probe_report_filter.py`，不重复实现HTTP敏感数据过滤。阶段事件固定字段/枚举，陌生字段不落盘。只保存安全run_id、Trace、阶段/耗时、HTTP诊断结构和布尔/计数；不存Secret、Code、Token、Cookie、密码、完整URL、员工字段原值、原日志或敏感指纹。

每轮最多3000行、2MiB，超上限或诊断预算耗尽即OBSERVABILITY_INCOMPLETE。最多256条HTTP事件/768KiB，加上状态机收据总量不超过1MiB，文件0600、目录0700。消费状态和账户权限一致性摘要只在受控内存比对，不导出身份指纹。协议测试结束后由负责人批准受保护收据保留期，普通Docker日志不作为永久诊断包。

每次轮询都原子写入脱敏收据。**恢复前必须先写证据**；正常结束及异常都经过统一finalize，再使用原五层Compose仅重建APP，实测prompt=false、原镜像/DB3027、原门禁、其他容器/挂载及账户/企业Session计数不变。磁盘写入失败时仍恢复false，但结果记证据/恢复待复核，不宣称PASS。

仅明确绑定的未消费事务可定向失效，按唯一主键及服务端prompt元数据限制，不按时间窗批量清理、不影响其他用户、不消费Code。SIGINT/SIGTERM通过finally恢复；SIGKILL、宿主机宕机或Docker不可用无法由同一进程保证恢复，须人工核对并使用APP-only恢复方案，不能声称覆盖所有不可恢复故障。

## 5. 合成测试与CI结果

本地Python3.9.5：28项通过、失败0、错误0。现有脱敏导出器测试再次通过。

[独立CI](https://github.com/suoak/WeKnora/actions/runs/38858387367)：Python3.8、3.12两项全部成功，目标提交88c884a42。测试不访问SID、Docker或真实数据库；适配器外部调用均Mock。适配器默认运行输出NOT_EXECUTED，不与服务器交互。

覆盖：密码页继续等待、callback后完整阶段采集、提前/延后完成反馈、从事务T0超时、无授权就绪超时、明确取消、第二authorize/事务/callback拒绝、过快消费无法关联拒绝、期限不足拒绝、Token/签名失败不重试、UserInfo auth_time不能替代签名证据、缺失事件NOT_CAPTURED、陌生敏感字段不落盘、重复轮询去重、证据先于恢复、磁盘失败仍恢复、APP-only不传true层、恢复后prompt及计数/其他容器一致性。

适配器两项测试初次遗漏Mock部署收据文件而失败，补齐合成文件读取Mock后28项全通过；没有通过连接真实服务器弥补测试夹具，没有执行真实配置切换。本轮没有宣称已做新运行器真实端到端部署演练。

## 6. 认证流量隔离只读调查

北京时间13:47:06现场：恢复后日志共2次授权入口200、2次callback302；观测到1个地址来源，归类private_network，不导出地址、不推断用户身份。日志未提供可用浏览器类别。最近5分钟没有OA相关请求，有效未消费事务0；没有发现已知Probe/单笔运行器/联合调试进程。

北京时间13:57:19计划任务范围：当前aqyfzx无crontab；可读系统及用户timer列表没有OIDC/SID标记。未读取其他用户或特权cron、客户端浏览器扩展/自动化配置。没有标记不代表绝无外部调用，不能据此猜测两笔事务操作者。

结论：**目前没有观察到正在并行的认证测试，但没有证明未来测试窗口独占。**新运行器的单进程锁只能防本工具并发，不能阻断其他浏览器访问全局OA入口。

下一次启动仍需用户/SID负责人确认不重叠窗口；监测器强制 `--exclusive-window-confirmed` 才能开始，但该声明不是技术网络隔离。本次不改防火墙、Nginx或全局路由。如实际出现第二流量即停止关联并恢复，不报告PASS；无法安排受控窗口就不启动真实认证，不能自行关闭他人项目来隔离。

## 7. 下一次最小执行方案

**目前为有条件就绪，不是授权执行。**下一次用户单独授权后：

1. Codex核对APP479416c90、前端1ba、DB3027及门禁、无活动事务、无其他并行测试，并约定连续8分钟人工窗口。
2. 受控SSH将本次提交的四个监测模块及匹配过滤器按原目录结构提供给一次性运行目录；不使用历史旧worker。未获授权前不上传启动或启用true。
3. 只有同时显式提供 `--execute-authorized --exclusive-window-confirmed` 才启用监测适配器。先启动监测及临时配置，确认READY，再通知用户点击；不由工具请求授权URL。
4. 用户在已有SSO及已验证路由的原Chrome中点击一次OA，完成本人SID认证、等待回KnowHub，回复“认证完成”和安全提示。无需Network截图、URL或Token。
5. Codex将用户反馈写成匹配run_id的固定action：password_page_observed、authentication_complete或cancel。密码页不触发收尾。
6. 后端立即单次兑换、监测器自动采集/分类；终点为identity_contract_pending，不要求Portal或企业Session。
7. 自动恢复false、核对现场、处理仅本笔未消费事务并生成报告。SID负责人只按窗口核对prompt接收、忽略已有SSO、新认证及员工准入实际执行，服务端尚未答复单独PENDING，不补造结果。

本轮无新SID请求、真实身份写入、运行APP/数据库/网络规则变化，未访问生产服务器。完成后停止等待授权。

## 8. 证据

- [28项合成测试收据](evidence/protocol-monitor-synthetic-tests-20261016.json)
- [Python双版本CI](evidence/protocol-monitor-ci-20261016.json)
- [流量/已知进程只读核查](evidence/prompt-login-traffic-isolation-audit-20261016.json)
- [计划任务只读核查](evidence/protocol-monitor-scheduler-audit-20261016.json)
