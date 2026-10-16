# SID 服务端证据分析与下一笔自动监测方案

2026-10-16。状态：**READ_ONLY_AUDIT_COMPLETE / MONITOR_PLAN_READY / NO_NEW_LOGIN_STARTED**。

本轮只读访问172.30.232.42，未发起authorize、Token、UserInfo或真实登录请求，未重建容器、修改配置/事务或触碰生产。APP479416c90、Frontend1ba24ef5f、DB3027clean及全部企业门禁保持现状。

## 1. 已有证据必须分组

### A. 先前prompt=true窗口：不完整事务

北京时间11:41:26—11:43:35，用户观察已有SSO仍出现重新输入密码页面，但明确未完成认证。

- 归档授权Trace `03a6279c-d628-4c8b-9b67-c1d48a636981`，authorize PASS，305ms。
- Nginx在11:40—11:46扫描窗口记录1次 `/api/v1/auth/oidc/url` 200、2次配置API200，没有callback记录。
- 本次时间范围内授权事务1条，已被定向失效，Result/verifier已清除；消费标记来自本地收尾，**不能误认为callback已经消费或Token已兑换**。
- Callback、Token、ID Token、nonce、UserInfo及protocol_only终点均NOT_EXECUTED。无callback不等于Token失败，也不是OIDC代码故障证据。
- 原启用时APP容器已被恢复操作替换，其原始Docker日志不能从当前容器重新读取；已有白名单收据仍可用。不能把缺失阶段写成诊断过期导致的证据缺失，因为用户已明确尚未完成认证。

### B. 恢复prompt=false后的独立事务：已有协议通过证据

只读查看11:46之后的当前日志发现两笔回调，不由本轮分析程序发起。不能确认操作者，不能把它们当作A的续接，也不能证明SID在这两笔执行了prompt强制认证。

| 项目 | 独立事务1 | 独立事务2 |
| --- | --- | --- |
| 回调Trace | `6037dfd9-ddd0-4dac-a0cd-d499b4087e19` | `2b28fe39-642d-4783-a222-4058817d7475` |
| Token发送北京时间 | 11:49:53.835014676 | 11:51:45.359859861 |
| Token接口 | POST sid.ruijie.com.cn/oidc/accessToken | 同左 |
| Token HTTP | 200，HTTP诊断248ms | 200，HTTP诊断234ms |
| 整体Token阶段 | PASS，255ms | PASS，240ms |
| callback/state | PASS，1ms | PASS，2ms |
| ID Token | PASS，215ms | PASS，190ms |
| UserInfo | PASS，194ms | PASS，187ms |
| 实际写出次数 | 1，无观察到重试 | 1，无观察到重试 |
| 身份映射/回调结果 | rejected，无local_session成功阶段 | rejected，无local_session成功阶段 |

HTTP耗时与业务Token阶段耗时测量边界不同，不应写成矛盾。

请求脱敏结构均为oauth_form Basic、单Authorization、表单字段client_id/code/grant_type/redirect_uri，code单值非空、redirect_uri匹配、无client_secret/code_verifier正文，PKCE关闭，state PASS。网络TLS1.3/HTTP1.1，实际远端39.107.195.76:443；这不是请求来源NAT地址。

现行代码id_token PASS发生在严格签名、Issuer/Audience/时间/nonce检查之后，UserInfo PASS发生在sub一致性检查之后。因而这两笔已有相应协议通过证据，没有观察到signature错误。原protocol_only=true的分支在身份写入前返回“sourceid identity contract pending”，阶段日志分类仅为rejected；结合代码、当前门禁及企业记录0，符合门禁拒绝，不能将泛化rejected记录解释为绑定故障。没有此次浏览器最终提示原文，最终提示类别应注明代码推导。

采集器保存的最后一组字段schema显示：ID Token没有auth_time字段，UserInfo有number类型auth_time。**UserInfo auth_time不是签名ID Token认证时间证据，不能用于宣称强制认证新鲜度通过。**该schema导出器按阶段保留最后值，不能无证据扩展为两笔每一份Token都已逐一检查auth_time存在性。

### C. 现存数据及诊断可用性

北京时间11:54:54只读数据：事务总80，已消费78、未消费过期2、未消费有效0；用户2、成员2、企业绑定/审核/Session/local Session均0。全部事务计数包含其他历史事务，不能全部归因于本次强制窗口。

当前APP启动约698秒，未超过现有30分钟诊断窗口；扫描范围没有diagnostics_limit事件。**没有本次证据证明已因窗口过期而遗漏已执行阶段。**但程序诊断机制有每进程30分钟/10MiB预算，未来长时间运行可能停止输出；不能把“当前未过期”当作永久保证。收尾重建后也会丢失被替换容器的标准日志，因此必须先采集再恢复。

扫描仅在远端内存中处理原始日志：每容器最多3000行/2MiB，固定时间窗；Nginx只导出白名单路径、方法及状态，剥离查询参数，不输出IP身份、Cookie、Header或错误原文。APP使用现有诊断白名单过滤，不转存raw日志。

## 2. 下一笔由Codex控制的监测状态机

本节为可执行方案，**尚未实施新运行器，也未启动下一笔**。此前运行器需按本节做最小控制校准，不能原样使用900秒等待和模糊人工结束条件。下一次执行授权应包含此监测校准与合成控制检查，不改变APP479416c90认证实现、数据库或会话策略。

### 启动与绑定范围

1. 获得新的一笔真实认证授权；确认SID负责人可核对，用户使用已有SSO的同一Chrome，当前默认prompt=false、门禁及数据库基线正确。
2. Codex启动监测并记录服务器生成run_id、UTC/BJT、原APP镜像/容器、五层Compose、账户权限受控一致性基线及企业记录计数；不读取SID凭据、Cookie或员工字段。
3. 确认有效未消费事务0。通过临时第六层仅APP启用prompt=true，核对实际进程和新容器就绪；新容器给出新的诊断窗口，不能假定旧窗口仍有预算。监测及失败恢复先准备好，再通知用户点击。
4. 服务端观察首个新授权成功事件及新授权事务，将其作为本run唯一pending事务。内部关联可使用数据库主键在受控进程内识别，**不导出state哈希、nonce、verifier、Result原文或任何敏感指纹**；仅输出安全随机run_id及原有Trace。
5. 授权与callback现有Trace不是同一个ID，监测器不能伪称APP已有原生跨阶段Trace。只有本run唯一新增授权事务、其单次消费、唯一对应时间窗callback及无其他新授权时，才作限定范围关联；出现第二授权、外部流量或多个候选callback则标CORRELATION_AMBIGUOUS，停止不猜测。仅服务端监测无法阻止其他用户访问全局授权入口，执行前必须安排不重叠测试窗口，不能把“只测试一人”当作网络白名单。

### 等待与完成

采用本地事务10分钟内的**8分钟人工窗口候选**，从新授权事务服务端创建T0开始，保留2分钟缓冲。预计人工操作不能覆盖时，在创建事务前停止；不可延长已存在事务。SID Code自签发起90秒，签发时间不能用T0代替。

状态：READY → AUTHORIZE_OBSERVED → WAITING_EMPLOYEE_AUTHENTICATION → CALLBACK_OBSERVED → TOKEN → ID_TOKEN → USERINFO → PROTOCOL_ONLY_PENDING → RESTORE。

- “出现SID密码页”或“已跳转”只记录UI中间观察，**继续等待**，不撤销事务、不恢复配置。
- 用户只需完成一次认证并返回KnowHub，再反馈“认证完成”；不提供Network记录、完整URL、Token或截图。状态含糊/模板消息继续在原期限内等待并只确认是否完成，不当作结束。
- 后端收到新Code立即单次兑换，不能等待聊天回复才发Token；无重试、无参数切换、无Postman消费。
- 观察callback终点后先保存证据；正常收尾等待用户明确“认证完成”或达到原截止。SID侧确认可单独PENDING，不为等SID回复无限保持true。
- 真正路由/授权拒绝/Token/签名/nonce/UserInfo失败、明确用户取消或截止到达时允许立即安全停止。预期identity_contract_pending不是协议失败。
- 截止未完成记INCOMPLETE_TIMEOUT，提示不再提交旧页，不自动新authorize、不延长旧事务。

### 日志与自动采集

- 从新APP启动/本run时间范围采集，1秒轮询或受控流式读取；单次扫描3000行/2MiB、总持久化不超过256事件/1MiB。代码现有窗口30分钟/10MiB保持不变，不因诊断放宽协议。
- 每阶段输出PASS/FAIL/NOT_EXECUTED/NOT_CAPTURED、耗时及固定错误类别，附run_id和安全Trace；无Token/员工原值、原Header Map或原始HTTP dump。
- request/send/response按同一callback Trace关联；输出Token路径、Basic策略、字段名称、单次写出、HTTP状态/类型/长度及允许关联信息。请求前“nonce未验证”不得误判；最终nonce由ID Token安全阶段确认。
- 新鲜度输出签名auth_time的存在性及固定分类；不接受UserInfo同名字段冒充；若程序不开启强制或无新鲜度事件，记NOT_CAPTURED，不能伪造。
- Nginx作为独立路径/状态证据，只处理固定路径，查询部分在内存中丢弃，按限定时间窗聚合；不要求用户F12。缺少请求关联头时标UNKNOWN。
- 若诊断预算耗尽、日志超过上限、源容器消失或采集断开，记录OBSERVABILITY_INCOMPLETE；不能把未知阶段记失败或成功，也不能为补证自动第二次认证。

### 自动恢复与校验

先保存安全收据，再去掉第六层引用，以原五层仅重建APP，实测prompt=false及原门禁、镜像、Frontend/依赖容器、DB3027与账户权限基线。无新增企业绑定/Session；本run唯一未消费事务只按明确归属定向失效或等待到期，不消费Code清理。明确取消/失败/超时才失效，不因密码页出现触发。

不关闭用户日常Chrome，不读Cookie；只提示关闭已结束的认证标签页。若本run未启动专用SSH/浏览器，则不假称关闭了这些进程。恢复异常仅按已批准APP-only回滚，不能整体回滚、降库或恢复raw。

## 3. 最少人工操作

用户：确认同一已有SSO的Chrome及测试人员可连续参与 → 等Codex通知READY → 点击一次“使用OA登录” → 本人完成SID认证并等待返回KnowHub → 回复“认证完成”或安全错误提示。不提供Network、Cookie、URL、Code或Token，不刷新重试。

Codex：负责预检、临时开关、服务端事务和阶段采集、超时与错误定位、恢复及报告。

SID负责人：依据Codex提供的本runUTC/BJT窗口、Client ID=knowhub、prompt请求预期、允许的服务端关联信息，核对是否忽略原SSO、实际启动/完成认证及执行员工准入。KnowHub本地Trace不保证传给SID；没有关联字段时按时间窗定位，不能编造共有ID。

服务端证据能证明KnowHub处理链路，不能单独证明浏览器已有SSO或SID确实让员工交互认证。由用户确认已有SSO和本人完成认证、SID确认内部执行即可，不需手动Network排查。若请求实际未进入测试机，监测只能判未观察到授权并停止，**不能在不控制客户端路由的情况下保证任何Chrome永远不会访问生产**；保留既有正确路由，不未经批准改系统网络。

## 4. 当前不执行及后续门禁

本轮仅整理只读证据和操作方案；没有修改运行器代码并将其部署为新监测服务，没有启用prompt或重新激活旧事务。下一笔需要单独授权，并先完成最小运行器校准/合成控制验收；否则不能宣称已具备新版自动结束规则。

协议测试始终protocol_only=true、contract/email/auto-binding/recheck=false、Session默认external。完整协议通过不等于OA业务放行；local_approved政策、无Refresh与期限、D/90秒Code残余风险接受、账号归属与全角色绑定审批仍独立处理。

## 5. 机器证据

- [原窗口、事务及现场审计](evidence/prompt-login-server-evidence-audit-20261016.json)
- [恢复后APP/Nginx阶段审计](evidence/prompt-login-current-log-audit-20261016.json)
- [恢复后两笔独立脱敏HTTP诊断](evidence/prompt-login-postrestore-diagnostics-20261016.json)
- [人工协作操作卡](SOURCEID_PROMPT_LOGIN_REAL_PROTOCOL_OPERATION_CARD.md)
- [上次SID负责人核对单](SOURCEID_PROMPT_LOGIN_SID_REAUTH_HANDOFF.md)
