# 单次真实 SID 强制认证协议联调操作卡

状态：**UPDATED_PLAN_ONLY / NEXT_TRANSACTION_NOT_AUTHORIZED**。本卡按上次停在SID认证页的经验更新，下一笔完整认证需另行授权。本轮只归档和更新操作卡，不改运行配置或程序。

目标仅为协议验证，不验证企业账号业务放行。目标服务器仅172.30.232.42，真实KnowHub Handler；不使用Protocol Probe，不访问生产10.51.134.114。

## 执行前门禁

1. 用户另行授权一次员工事务；SID负责人在线，准备对照服务端认证及Token日志。
2. 只读核对APP479416c90、Frontend1ba24ef5f、DB3027clean、oauth_form、PKCE=false、scope=openid、原批准callback和正式Metadata/JWKS TLS。
3. 保持protocol_only=true、contract_confirmed=false、email_binding=false、auto_binding=false、authorization_recheck_confirmed=false、Session默认external；不创建企业绑定或Session。
4. 在独立批准范围内临时将强制认证选项设为true，运行时安全加载当前有效SID凭据；不读取/导出Secret或Header。不改变其他认证配置。先备份APP配置并准备关闭强制选项的回退方式。
5. 确认浏览器页面、静态资源、API及callback均进入172.30.232.42：受控浏览器的本地解析/SSH路由必须覆盖整个正式域名，禁用会将域名转发至生产的代理路径；不修改生产DNS/网关。只凭本地hosts配置存在不能判定路由已验证。
6. 通过无秘密探测确认远端测试入口、前端资源与API版本；域名错误或代理不明确时停止。没有真实授权前，不以点击OA入口作探测。
7. 开始前用户和SID负责人均可连续参与，确认预计人工认证时间不超过8分钟；如需更久，先不创建授权事务，不在等待中延长旧事务。当前本地事务自授权创建起10分钟，预定8分钟认证窗口为下一笔操作方案，需随下一次执行授权确认。
8. 核对并校准运行器结束条件后才可开始。现有归档运行器有900秒总等待及callback_result后自动收尾逻辑，**不能原样宣称已经满足本卡新规则**。下一次获批执行前，受控运行器需按下述人工状态和8分钟截止要求完成最小控制校准/合成检查；不改APP认证、期限或数据库。若未校准则停止准备，不能靠操作者临时猜测来执行。

## 一笔事务

1. 记录UTC及北京时间、本地诊断Trace；从KnowHub OA入口获取服务端生成授权URL，仅在受控进程中检查scheme/host/path、scope、redirect_uri、prompt=login单值、state/nonce存在性，无max_age/PKCE参数。不得保存完整URL或state/nonce原值。
2. 用户在已有SID SSO Cookie的浏览器上下文中只点击一次OA登录。**看到SID重新输入账号密码页面只是中间观察，不代表认证完成或测试结束。**用户可反馈“已出现重新认证页面，尚未完成”，运行器继续等待，不恢复配置、不失效事务。SID负责人分别核对prompt接收、忽略原SSO、交互认证开始；不索取Cookie或密码。
3. 用户本人继续完成这一次认证，等待浏览器自动返回KnowHub，再明确反馈“认证完成，已返回KnowHub”，并只说明安全页面提示。不能用“已出现密码页面”“已跳转”或未填模板代替完成确认。若表示“认证完成”但尚未返回，运行器在原截止内继续等待callback，不自行再发授权请求。
4. 新code自动进入KnowHub真实callback，state及持久化事务单次消费，立即兑换一次；不等聊天“完成”反馈才兑换，不刷新、重复点击或由Postman消费同一code。SID Code从签发时起90秒，不从本地事务创建或出现密码页起算；实际签发时刻未知时不能补造。
5. 保存既有严格脱敏Token HTTP诊断，确认真实Token请求次数=1、oauth_form、状态和耗时。401或安全校验失败立即停止，不切换参数。
6. Token200继续RS256/JWKS、Issuer/Audience/时间声明/nonce、UserInfo sub验证。
7. 对auth_time只记录是否存在及固定证据分类。缺失明确记NOT_PROVIDED_CONTRACT_REQUIRED；存在但旧/未来/类型错误应拒绝，不能以UserInfo同名字段补充。不得保存原值或原ID Token。
8. 协议全部通过后，预期身份契约门禁返回identity_contract_pending；核对users/membership/身份绑定/企业Session没有新增。该提示是本轮预期安全终点，不要求进入Portal。SID负责人再确认认证及准入是否实际完成；未有SID确认时单独记PENDING。

## 人工协作状态与截止条件

| 状态/反馈 | 运行器动作 |
| --- | --- |
| 已出现SID密码页、尚未认证 | 仅记录UI OBSERVED，继续等待，不撤销事务 |
| 用户消息仍有占位模板或含义不明 | 询问实际进度，在原截止内继续等待；不能自动解释为结束 |
| 已完成认证并返回KnowHub | 核对callback及后端阶段结果，允许进入收尾 |
| 后端已出现成功callback终点但用户未明确完成 | 先保存安全证据，等待用户确认或原截止；不单因UI消息提前撤销 |
| 用户明确取消/停止本笔 | 停止并收尾，记CANCELLED/INCOMPLETE，不重试 |
| 真实技术失败（路由/授权拒绝/Token/签名/nonce/UserInfo等） | 立即停止并收尾，记录具体阶段，不等待第二笔 |
| 到达预定截止、没有完整结果或人工确认 | 停止并记INCOMPLETE_TIMEOUT，提示不再提交旧页面，不延长旧事务、不自动重试 |

截止从**本笔服务端授权事务创建时间T0**起算，预计人工认证最多8分钟，保留相对本地10分钟期限的2分钟缓冲；不能从APP重建开始任意900秒代替。创建时间只从服务端安全元数据取非身份时间，不读取state/nonce/Code，不将它解释为SID Code签发时间。

开始前必须验证服务端剩余事务期限覆盖预定窗口和缓冲。若期限配置与现有10分钟不一致、创建时间无法安全识别，或预计人工操作超过预算，应在发起授权前停止。不修改APP期限、不续期旧事务。收到callback后立即兑换，不进行人为排队；人工进度消息只控制收尾，不延迟90秒Code兑换。

截止是测试协作窗口，不能宣称为SID传播SLA或准入有效期。已经在途的Token请求按既有超时完成/安全失败，不追加请求；不得因恢复APP造成已产生结果的证据未保存。运行器退出前保存白名单收据；整体到达截止但未完成时不能记协议PASS。

## 通过及停止条件

通过：实际授权prompt=login已验证；SID服务端确认已有SSO仍重新认证；一笔Token200且ID Token/nonce/UserInfo通过；auth_time证据边界如实记录；protocol_only阻止身份写入。

仅看到登录页、Token200或本地prompt配置true，不足以证明SID完成了强制认证和准入。界面出现记OBSERVED，SID确认为PENDING/CONFIRMED，OIDC各阶段分别判定。没有callback时记NOT_EXECUTED/INCOMPLETE，不能仅据此判定OIDC代码故障；任何失败不自动进行第二次真实事务。

## 收尾

仅在上述明确完成、预定超时、明确取消或技术失败条件满足后收尾：先保存本笔脱敏证据，去掉临时第六层，只用原五层APP-only重建恢复prompt=false及原诊断状态；实测进程false、原revision、80/8080、DB3027及门禁不变。只对明确归属本笔的未消费事务定向失效或等待过期，不能在“出现密码页”阶段执行该步骤。

关闭本轮专用浏览器/SSH隧道并核对端口释放；若复用用户日常Chrome，不强行关闭整个浏览器，提示关闭已结束的认证标签页，不读取Cookie。保存脱敏报告及SID侧执行结论，不保存授权URL、code、Token、Cookie、员工信息或原响应。

真实业务一键登录另需本地会话、全角色绑定及流量隔离批准。Code90秒且停用前Code仍可用；D没有量化SLA，10分钟本地期限仍为审批候选，不在本卡中启用local_approved。

给SID负责人的上次核对资料：[脱敏核对单](SOURCEID_PROMPT_LOGIN_SID_REAUTH_HANDOFF.md)。本轮未触发新的授权、应用重建、事务失效或运行配置变化。

更新：下一笔优先采用 [服务端自动监测方案](SOURCEID_SERVER_PROTOCOL_MONITORING_PLAN.md)，用户只点击一次、本人完成认证并反馈完成，不再要求提供Network截图、完整请求URL或Token。浏览器既有SSO及路由沿用已确认上下文；仅服务端监测不能凭空证明客户端路由，发生未观察到授权时停止，不要求用户通过重复登录补证。
