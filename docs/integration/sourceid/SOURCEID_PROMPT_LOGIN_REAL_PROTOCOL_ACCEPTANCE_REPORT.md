# SID prompt=login 本轮真实协议验收：校准监测器

2026-10-16。本轮run_id：`b1c1a75a-a661-4f9d-b4e3-40947af37327`。

**INCOMPLETE / INCOMPLETE_TIMEOUT / RESTORE_PASS**。只观察到一笔authorize，未观察到callback，未发送本次Token请求；不能归因为Token、签名、nonce或UserInfo失败。没有自动第二次登录。

## 本轮执行

- 用户确认SID负责人在线、原Chrome有SSO、KnowHub路由指向172.30.232.42、连续8分钟独占窗口，并明确授权。Chrome路由/SSO为用户确认，未读取Cookie验证。
- 14:36:19北京时间只读预检通过：APP479416c90、Frontend1ba24ef5f、DB3027 clean，活动未消费事务0，企业门禁关闭。
- 使用审核提交88c884a42的四个监测模块及两个匹配过滤文件；Git对象一致后从该提交提供源文件，没有使用历史worker。
- 启动包装器起初因私有父目录不存在而停止，未启动监测或改APP；建立0700目录后只启动一个有效监测进程。一个要求prompt=false的辅助只读快照在临时true期间按断言拒绝执行，不是凭据错误；主监测器继续运行。没有由工具发起authorize/Token。
- READY时实测进程prompt=true及原门禁。状态文件中的READY是初始快照，最终状态以final-result及恢复快照为准。
- 唯一事务创建：**14:38:43.942523北京时间 / 06:38:43.942523 UTC**。本地原到期14:48:43.942569；人工等待截止14:46:43.942523，没有延长。
- authorize Trace：`f15819c8-5eaf-4f45-87d6-9af7ae49d3ab`，PASS，254ms。唯一新事务包含服务端prompt=login元数据；不导出事务键或完整URL。
- 用户最初回复“认证完成”，随后确认仍在SID页面，并说明“没有报错，登录之后会自动跳转到sid页面”。operator_complete=true只证明收到反馈，不证明完成协议。运行器没有提前恢复，而是等待原截止。
- 截至14:47:19安全路径计数：OA入口200为1，callback/result为0；APP阶段也只有authorize。无第二笔关联冲突。

## 阶段结果

| 阶段 | 本轮结果 |
| --- | --- |
| 服务端强制模式 | PASS：进程配置和事务元数据已观察；不等于SID实际执行确认 |
| authorize | PASS，254ms |
| SID页面 | USER_OBSERVED：提交后仍在SID，无错误 |
| 忽略SSO、重新认证及员工准入 | PENDING_SID_CONFIRMATION；在线不等于已反馈执行结果 |
| callback/state | NOT_EXECUTED_AT_TEST_INSTANCE；没有相应请求或事件 |
| Token | NOT_EXECUTED；无HTTP状态、耗时或发送结构；不是HTTP401或200 |
| ID Token/RS256/JWKS/Issuer/Audience/时间/nonce | NOT_EXECUTED |
| UserInfo/sub | NOT_EXECUTED |
| 签名auth_time | NOT_CAPTURED：未到ID Token阶段，未用UserInfo替代 |
| identity_contract_pending | NOT_EXECUTED：未到业务门禁分支 |
| 企业身份/Session | 无新增，门禁保持关闭 |
| 历史signature问题 | 未到验签阶段，既不证明修复，也不是新失败样本 |

没有callback不能证明SID内部未签发Code，也不能确定SID内部跳转、浏览器路由或其他根因。没有因用户反馈或页面显示而判定完整PASS。

## 超时与安全恢复

480秒到期，监测器记录INCOMPLETE_TIMEOUT，先保存pre-restore-evidence，再原五层Compose仅恢复APP，核对后保存final-result。仅定向失效本次明确归属的未消费事务，无批量清理、不消费Code。

- final-result：restore=PASS；transaction_cleanup=EXACT_UNUSED_TRANSACTION_INVALIDATED；HTTP事件0；监测进程已结束。
- 14:47:19独立核验：prompt=false，protocol_only=true；contract/email/auto-binding/recheck/logout=false；Session默认external；oauth_form不变；OIDC及诊断保持现场true；AUTO_MIGRATE=false。
- APP原镜像及479416c90不变，恢复后容器`7e2138ebd613725c2156baff2e509c94ef0212c46fb9fed68e9d596720d6b40e`，healthy；Frontend原容器及1ba24ef5f不变。
- DB3027 clean；用户2、身份绑定0、本地企业Session0、活动未消费事务0。适配器在内存比对用户/成员权限及相关表计数不变；配套容器和挂载不变。frontend/app/配置代理均HTTP200。
- 未切库、迁移、启用local_approved或业务绑定，未访问生产。未创建/关闭日常Chrome、读取Cookie、修改hosts/全局代理或新建隧道。
- 当前SID测试页面对应事务已失效，请关闭，不再提交、延长或重复登录。

## SID负责人最小核对单

Client ID=knowhub，scope=openid，服务端prompt=login，原审批callback不变；oauth_form/client_secret_basic、无PKCE未调整。

请核对**2026-10-16 14:38:43.942523至14:46:43.942523北京时间**（UTC06:38:43.942523–06:46:43.942523）：

1. 是否收到本笔prompt=login，忽略原SSO并执行新认证/员工准入？仅提供事件类别及时间。
2. 用户提交认证后为何仍回到SID页面，是否发生内部重定向或会话异常？
3. 是否签发Code并发送回调跳转？仅提供布尔、时间及回调目的路径是否正确，不提供Code或完整Location。
4. 若确实发出回调，再通过独立批准定位为何测试机未接收；本轮不追加认证或修改参数。

state创建时间不是SID Code签发时间，90秒Code兑换规则本轮未验证。下一步范围为SID认证至callback到达之间；不能把本轮当Token401或认定OIDC代码故障。

## 本轮证据

- [最终脱敏结果](evidence/prompt-login-real-calibrated-final-20261016.json)
- [执行前现场](evidence/protocol-monitor-authorized-preflight-20261016.json)
- [恢复后独立核验](evidence/protocol-monitor-real-restored-20261016.json)
- [前端路径计数](evidence/protocol-monitor-live-paths-20261016.json)
- [授权事务时间](evidence/protocol-monitor-live-window-20261016.json)

本轮停止，等待SID反馈及后续独立授权，不执行第二笔或企业业务放行。

---

# 历史记录：此前11:41单次真实协议验收（不与本轮关联）

2026-10-16。结论：**INCOMPLETE / CALLBACK_AND_TOKEN_NOT_EXECUTED / RESTORE_PASS**。

本次服务端强制授权配置成功加载，并观察到一次授权入口成功；未观察到本次callback或Token兑换，不能将本次标记为真实协议验证通过。按用户“操作已结束”反馈停止，未自动重试。当前已恢复prompt=false。

用户随后补充实际观察：点击OA后Chrome已跳转SID，已有SSO的上下文仍被要求输入账号密码。此项记为 **USER_OBSERVED_REAUTHENTICATION_UI**，符合prompt=login预期，但用户当时尚未完成交互认证，不能代表SID已完成认证/员工准入，亦不能代表Token或后续校验通过。

特别说明：在补充观察到达前，运行器已按此前“操作已结束”的反馈恢复false，并定向失效该笔未消费事务。**当前旧SID页面不应继续提交**；即使SID页面仍可提交或后来签发Code，KnowHub本地事务已无效，不能把它当成仍可继续的本笔验收。不重新激活已消费/失效事务，不恢复旧state，不借此自动发起第二笔登录。

## 1. 执行前事实与路由

- 用户确认SID负责人在线，日常Chrome已有有效SID SSO；程序未读取Cookie验证。
- 用户确认同一Chrome的登录页、静态JS、配置API及无参数callback的Remote Address全部为172.30.232.42:80。
- APP479416c90、Frontend1ba24ef5f、DB3027clean及原门禁核对通过。原有效未消费事务0，企业绑定/审核/Session/local Session均0。
- 继续使用原日常Chrome；没有新建空SSO Profile，没有读取/复制/导出Cookie，没有改全局hosts/代理/DNS，没有访问生产KnowHub。

路由与既有SSO是用户明确确认的观察，不能写成程序独立验证了日常Chrome的Cookie。没有为建立SSO自行增加真实认证。

## 2. 临时启用及授权证据

| 项目 | 结果 |
| --- | --- |
| 开始运行器 | 北京时间11:41:05.263374 |
| APP就绪 | 11:41:26.801231，进程prompt=true |
| 配置方式 | 原五层Compose后追加本次private第六层，仅强制参数false→true |
| 其他门禁 | protocol_only=true，contract/email/auto-binding/recheck=false；Session默认external、Refresh保持原NOT_SET |
| 授权阶段 | 一条authorize PASS，305ms |
| 授权Trace | `03a6279c-d628-4c8b-9b67-c1d48a636981`，服务器自有诊断标识 |
| 持久化强制请求事实 | 本次时间范围内1条未消费事务包含服务端版本1、prompt=login元数据 |
| 浏览器实际URL唯一prompt | NOT_CONFIRMED：用户没有提供实际检查结果；不保存完整授权URL |
| SID登录页跳转 | OBSERVED，用户已确认 |
| 已有SSO仍显示重新输入密码界面 | OBSERVED，用户已确认；尚未提交认证 |
| SID服务端重新认证及准入确认 | PENDING，负责人反馈尚未取得，具体执行状态UNKNOWN |

持久化元数据证明服务端生成链路采用强制模式，结合已验收代码可核查参数构造规则；它不替代浏览器最终参数或SID实际执行的证据。没有把prompt=true本身写成“SID强制认证验收成功”。

## 3. 实际协议阶段

| 阶段 | 本次结果 |
| --- | --- |
| Callback/state | NOT_EXECUTED，用户确认未完成SID认证，无callback_state或callback_result记录 |
| Token HTTP | NOT_EXECUTED，无请求/响应诊断；没有HTTP状态或耗时可报告 |
| ID Token RS256/JWKS/Issuer/Audience/时间声明 | NOT_EXECUTED/NOT_OBSERVED，无验证结果 |
| nonce | NOT_OBSERVED |
| UserInfo/sub | NOT_OBSERVED |
| 签名auth_time | NOT_OBSERVED，没有ID Token结构或新鲜度事件 |
| 偶发signature失败 | 本次无相应失败事件；由于未到验证阶段，不能据此宣称问题修复或本笔签名通过 |
| identity_contract_pending | NOT_OBSERVED；本次没有证据到达该阶段 |

安全日志过滤收据只含一条authorize阶段、HTTP事件0、声明schema0。数据库同时发现本次授权事务尚未消费，支持“未观察到有效callback消费”的结论。不能伪造Token200/401、验签PASS或新鲜度分类。

最初的用户回报为“【填写实际提示】”“【是/否】”等占位模板，不能据此认定已完成认证。用户随后确认停在SID重新输入密码的页面，尚未确认完成认证；这解释了当前没有callback及Token结果的观察边界。没有证据证明SID拒绝、签名错误或浏览器异常，不推断这些根因。

运行器原始结果字段FAIL源于其初始未完成状态及操作结束后的受控中断，**不是Token失败或ID Token验签失败**。本报告将业务结论准确分类为INCOMPLETE_NO_CALLBACK_OBSERVED，原始安全收据保留。

## 4. 停止与恢复

收到“本次操作已结束”后，先在受控进程内截取严格白名单诊断，不保存原始日志，再校验确切worker PID和脚本路径，定向中断运行器，执行其finally恢复。未使用宽泛kill/pkill，没有再发authorize或Token请求。

北京时间11:43:35.456201恢复完成；11:44:38.904839只读复核及定向事务收尾通过。

| 检查 | 最终结果 |
| --- | --- |
| prompt | false，原五层重建APP生效，true第六层不再参与运行 |
| APP镜像/revision | 原目标479416c90不变，`sha256:85f9d6c052298e7f52e7d7c4533e131ce19326d461521378e90c6df3b01fa49c` |
| APP新容器ID | `20d5404be8c690a9358e1bee3c0365e53c85fada7c8c157e475dc505f83accf9`，healthy |
| Frontend / APP / 配置API | 80/8080/配置均200 |
| 数据库 | 3027，dirty=false，未迁移或切库 |
| 用户及成员 | 2 / 2，原账号/权限受控比对一致 |
| 企业绑定/审核/Session/local Session | 均0，未新增 |
| 其他容器 | Frontend、PostgreSQL、Redis、DocReader及挂载均未变化 |
| Basic与业务门禁 | oauth_form，protocol_only=true，contract/email/auto-binding/recheck=false，Session/Refresh原NOT_SET |
| 遗留授权事务 | 仅本次有明确时间范围及服务端prompt元数据的1条未消费事务定向失效，清除Result/verifier，不输出原值；最终有效未消费0 |

定向事务清理只修改本笔认证事务，不修改账户、权限、企业身份或历史其他事务。旧Code没有被兑换用于清理。由于不再接受本笔事务，请用户不要继续提交仍停留的SID页面，也不要点击第二次登录。

本轮未启动专用Chrome或SSH隧道，使用用户已确认直连测试机的日常Chrome，因此没有对应专用进程需要关闭。**未强行关闭用户日常Chrome**。没有回滚APP代码，没有启用local_approved或自动绑定。

## 5. 后续边界

本次授权事务已经结束，不因结果不完整自动补做。先补齐此次实际页面和SID侧记录，定位为何未出现callback；任何后续真实事务等待独立授权。

可交给SID负责人的最小核对信息：2026-10-16北京时间11:41:26—11:43:35强制配置开启窗口；Client ID为 `knowhub`；本地授权Trace `03a6279c-d628-4c8b-9b67-c1d48a636981`；用户观察已有SSO仍显示密码认证页，明确尚未提交认证；客户端没有callback/Token记录。请核对该窗口的prompt请求是否忽略原SSO、重新认证流程是否已启动，以及是否执行员工准入；不得把认证界面出现等同于准入已完成。该Trace是KnowHub本地标识，未必传到SID，不能声称SID可直接按此检索。不得索取原始Code、Token、Cookie、员工数据或完整请求日志。本程序没有向外部联系人发送消息；由用户将脱敏核对项交负责人，服务端结果收到后再归档。

最终用户确认：不会继续提交旧认证页面、刷新或重复登录；尚未独立核对浏览器授权请求中的唯一prompt参数。当前仅依据服务端受控配置及持久化事务确认强制模式，完整协议验收保持INCOMPLETE。没有新增SID负责人确认结论，不补造PENDING事项的结果。

真实OA业务验收仍缺：完成强制协议真实闭环与SID执行证明、auth_time边界如实确认、D及停用前Code90秒残余风险接受、获批的local_approved期限/无Refresh政策、全角色绑定及流量隔离/账号归属审批。10分钟仍为候选，不在此报告批准。

## 6. 脱敏证据

- [执行前现场](evidence/prompt-login-real-execution-precheck-20261016.json)
- [授权入口及自动恢复收据](evidence/prompt-login-real-once-result-20261016.json)
- [最终恢复及定向事务处理](evidence/prompt-login-real-final-closeout-20261016.json)

未保存完整URL、Secret、Code、Token、Cookie、员工字段原值、凭据指纹或原始HTTP响应。报告结论仅使用允许的阶段、布尔值、计数及安全Trace。

## 7. 后续只读审计补充——不得合并为本笔强制认证结果

2026-10-16北京时间11:54—11:56服务端审计发现，恢复prompt=false之后出现两笔独立事务：11:49:53、11:51:45发送Token，分别HTTP200/248ms、200/234ms，callback/state、ID Token及UserInfo阶段PASS，之后身份映射rejected。企业绑定及Session仍0，符合protocol_only门禁；操作者及是否强制重新认证没有证据，本轮程序未发起这些请求。

这两笔不能补成上次11:41—11:43强制窗口的闭环，也不能修改本报告主结论INCOMPLETE。最后保存的字段schema中ID Token未见auth_time，UserInfo有number类型auth_time；后者不能冒充签名新鲜度证据。详细独立Trace、证据边界及下一笔采集方案见 [服务端监测方案](SOURCEID_SERVER_PROTOCOL_MONITORING_PLAN.md)。
