# SID 负责人核对单：强制重新认证页面观察

日期：2026-10-16。仅供用户转交SID负责人；本轮未向外部人员发送消息。结论为 **AUTHORIZATION_UI_OBSERVED / FULL_PROTOCOL_INCOMPLETE**，不能判定OIDC代码故障。

## 可关联的脱敏事实

| 字段 | 已有证据 |
| --- | --- |
| 应用 | Client ID `knowhub` |
| 请求目标 | `https://sid.ruijie.com.cn/oidc/authorize` |
| 协议参数 | Authorization Code、scope=openid、PKCE关闭；服务端强制模式生成prompt=login |
| Callback | `http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback` |
| 来源业务环境 | 打包机172.30.232.42，KnowHub APP479416c90 |
| 强制配置就绪 | 北京时间2026-10-16 11:41:26.801231 / UTC03:41:26.801231 |
| 恢复false | 北京时间11:43:35.456201 / UTC03:43:35.456201 |
| 授权请求精确发起时间 | NOT_CAPTURED；请先按上述开启窗口定位，不将配置就绪时间冒充请求时间 |
| 本地授权Trace | `03a6279c-d628-4c8b-9b67-c1d48a636981`，authorize PASS，305ms |
| Trace边界 | KnowHub本地生成，未证明已传SID；不能保证SID可按该ID检索 |
| 持久化事务 | 本次1条事务包含服务端prompt=login元数据；不是浏览器最终查询参数的独立截图 |
| 用户观察 | 日常Chrome已有SID SSO；点击OA后仍跳转SID并要求输入账号密码 |
| 用户是否完成认证 | 否，最后停在SID认证页面，未提交完成 |
| Callback / Token | NOT_EXECUTED；没有Token HTTP200/401或ID Token验证结果 |
| SID执行确认 | PENDING，尚未收到服务端结论 |

浏览器路由由用户在当前Chrome确认：页面、静态资源、配置API及无参数callback均172.30.232.42:80。此IP为测试环境标识，**不等于SID观察到的公网出口NAT IP**，本次没有采集SID所见来源地址。

## 请SID负责人只核对以下事项

1. 上述窗口是否收到knowhub的authorize请求；服务端解析结果是否为唯一 `prompt=login`？如无日志或无法定位，请明确NOT_CAPTURED/UNKNOWN，不补造。
2. 是否忽略已有SSO并启动新的交互认证流程？能否将“显示认证界面”“员工提交认证”“认证成功”“员工准入判断完成”分别确认？
3. 用户确认未完成认证，服务端是否同样显示未完成、没有签发Code？若员工准入只在认证完成后执行，本次应记NOT_EXECUTED，不因出现密码页就记准入PASS。

只需返回阶段、结果、可公开关联标识和非秘密时间。不要提供原授权URL、Cookie、密码、Code、Token、Basic头、员工字段或原始日志。

本次没有Token请求，暂不要求负责人排查Token401、签名或UserInfo问题。签名auth_time是否返回留待下一笔获授权的完整认证，不能从本次UI观察推断。

## 本地收尾说明

在收到“操作已结束”的模板反馈后，运行器被停止，APP恢复prompt=false，并定向失效本次唯一未消费授权事务；无新增绑定/企业Session、无账号权限变化、DB3027clean。用户随后澄清尚未完成认证，因此完整协议标记INCOMPLETE。

此过程暴露人工协作结束语不够精确；不是OIDC代码故障证据。下一笔操作卡已明确：看到密码页只是中间状态，等待本人完成并返回KnowHub，或到达预定超时/明确取消/技术失败才收尾；不会继续使用本次失效事务。

引用：[本次验收报告](SOURCEID_PROMPT_LOGIN_REAL_PROTOCOL_ACCEPTANCE_REPORT.md)、[脱敏运行收据](evidence/prompt-login-real-once-result-20261016.json)、[恢复及事务收尾](evidence/prompt-login-real-final-closeout-20261016.json)。
