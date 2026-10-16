# SID prompt=login 单次真实协议验收记录

2026-10-16。结论：**INCOMPLETE_NO_CALLBACK_OBSERVED / RESTORE_PASS**。

本次服务端强制授权配置成功加载，并观察到一次授权入口成功；未观察到本次callback或Token兑换，不能将本次标记为真实协议验证通过。按用户“操作已结束”反馈停止，未自动重试。当前已恢复prompt=false。

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
| SID执行强制重新认证及准入 | UNKNOWN，等待实际SID确认 |

持久化元数据证明服务端生成链路采用强制模式，结合已验收代码可核查参数构造规则；它不替代浏览器最终参数或SID实际执行的证据。没有把prompt=true本身写成“SID强制认证验收成功”。

## 3. 实际协议阶段

| 阶段 | 本次结果 |
| --- | --- |
| Callback/state | NOT_OBSERVED，无callback_state或callback_result记录 |
| Token HTTP | NOT_OBSERVED，无请求/响应诊断；没有HTTP状态或耗时可报告 |
| ID Token RS256/JWKS/Issuer/Audience/时间声明 | NOT_EXECUTED/NOT_OBSERVED，无验证结果 |
| nonce | NOT_OBSERVED |
| UserInfo/sub | NOT_OBSERVED |
| 签名auth_time | NOT_OBSERVED，没有ID Token结构或新鲜度事件 |
| 偶发signature失败 | 本次无相应失败事件；由于未到验证阶段，不能据此宣称问题修复或本笔签名通过 |
| identity_contract_pending | NOT_OBSERVED；本次没有证据到达该阶段 |

安全日志过滤收据只含一条authorize阶段、HTTP事件0、声明schema0。数据库同时发现本次授权事务尚未消费，支持“未观察到有效callback消费”的结论。不能伪造Token200/401、验签PASS或新鲜度分类。

用户回报为“【填写实际提示】”“【是/否】”等占位模板，不属于实际页面或SID证据，已要求仅补充本次真实页面状态和负责人结论，不要求重新操作。无法判断用户停在认证页、发生浏览器异常、SID拒绝或回调未返回；原因保持UNKNOWN。

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

真实OA业务验收仍缺：完成强制协议真实闭环与SID执行证明、auth_time边界如实确认、D及停用前Code90秒残余风险接受、获批的local_approved期限/无Refresh政策、全角色绑定及流量隔离/账号归属审批。10分钟仍为候选，不在此报告批准。

## 6. 脱敏证据

- [执行前现场](evidence/prompt-login-real-execution-precheck-20261016.json)
- [授权入口及自动恢复收据](evidence/prompt-login-real-once-result-20261016.json)
- [最终恢复及定向事务处理](evidence/prompt-login-real-final-closeout-20261016.json)

未保存完整URL、Secret、Code、Token、Cookie、员工字段原值、凭据指纹或原始HTTP响应。报告结论仅使用允许的阶段、布尔值、计数及安全Trace。
