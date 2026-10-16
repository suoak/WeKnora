# 单次真实 SID 强制认证协议联调操作卡

状态：**PREPARED / NOT_AUTHORIZED_TO_EXECUTE**。本次APP代码部署授权不包含此操作卡的真实登录。

目标仅为协议验证，不验证企业账号业务放行。目标服务器仅172.30.232.42，真实KnowHub Handler；不使用Protocol Probe，不访问生产10.51.134.114。

## 执行前门禁

1. 用户另行授权一次员工事务；SID负责人在线，准备对照服务端认证及Token日志。
2. 只读核对APP479416c90、Frontend1ba24ef5f、DB3027clean、oauth_form、PKCE=false、scope=openid、原批准callback和正式Metadata/JWKS TLS。
3. 保持protocol_only=true、contract_confirmed=false、email_binding=false、auto_binding=false、authorization_recheck_confirmed=false、Session默认external；不创建企业绑定或Session。
4. 在独立批准范围内临时将强制认证选项设为true，运行时安全加载当前有效SID凭据；不读取/导出Secret或Header。不改变其他认证配置。先备份APP配置并准备关闭强制选项的回退方式。
5. 确认浏览器页面、静态资源、API及callback均进入172.30.232.42：受控浏览器的本地解析/SSH路由必须覆盖整个正式域名，禁用会将域名转发至生产的代理路径；不修改生产DNS/网关。只凭本地hosts配置存在不能判定路由已验证。
6. 通过无秘密探测确认远端测试入口、前端资源与API版本；域名错误或代理不明确时停止。没有真实授权前，不以点击OA入口作探测。

## 一笔事务

1. 记录UTC及北京时间、本地诊断Trace；从KnowHub OA入口获取服务端生成授权URL，仅在受控进程中检查scheme/host/path、scope、redirect_uri、prompt=login单值、state/nonce存在性，无max_age/PKCE参数。不得保存完整URL或state/nonce原值。
2. 用户在已有SID SSO Cookie的浏览器上下文中执行一次OA登录。SID负责人确认prompt=login忽略已有SSO并重新执行认证与员工准入；不索取浏览器Cookie或员工密码。
3. 新code自动进入KnowHub真实callback，state及持久化事务单次消费，立即兑换一次；不刷新、重复点击或由Postman消费同一code。
4. 保存既有严格脱敏Token HTTP诊断，确认真实Token请求次数=1、oauth_form、状态和耗时。401或安全校验失败立即停止，不切换参数。
5. Token200继续RS256/JWKS、Issuer/Audience/时间声明/nonce、UserInfo sub验证。
6. 对auth_time只记录是否存在及固定证据分类。缺失明确记NOT_PROVIDED_CONTRACT_REQUIRED；存在但旧/未来/类型错误应拒绝，不能以UserInfo同名字段补充。不得保存原值或原ID Token。
7. 协议全部通过后，预期身份契约门禁返回identity_contract_pending；核对users/membership/身份绑定/企业Session没有新增。该提示是本轮预期安全终点，不要求进入Portal。

## 通过及停止条件

通过：实际授权prompt=login已验证；SID服务端确认已有SSO仍重新认证；一笔Token200且ID Token/nonce/UserInfo通过；auth_time证据边界如实记录；protocol_only阻止身份写入。

仅看到登录页、Token200或本地prompt配置true，不足以证明SID执行了强制认证。无法确认时记UNKNOWN，保持业务门禁；任何失败不自动进行第二次真实事务。

## 收尾

恢复部署后强制认证false及原诊断状态，仅按授权范围重建APP；关闭本轮专用浏览器/SSH隧道，核对端口释放、80/8080正常及其他容器未变化。保存脱敏报告及SID侧执行结论，不保存授权URL、code、Token、Cookie、员工信息或原响应。

真实业务一键登录另需本地会话、全角色绑定及流量隔离批准。Code90秒且停用前Code仍可用；D没有量化SLA，10分钟本地期限仍为审批候选，不在本卡中启用local_approved。
