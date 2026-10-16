# 下一笔真实 OA 业务验收操作卡（待独立授权）

仅独立knowhub-oa-sso-pilot；APP f40b1602f，Frontend832b6652a，DB3029。本轮未执行本卡。正式产品仍一次性全员开放；独立验收来源限制不是线上按角色灰度。

## 前置核验

1. 业务/数据负责人经受保护渠道交付真实历史归属批次：issuer、稳定RJGH、可信企业邮箱、原user_id或可唯一解析的历史账号、独立历史归属证明、来源/版本、active/disabled、真实verified_at/valid_until及真实政策引用。报告只保留数量与冲突分类。
2. 先dry-run，无数据不制造样本充当真实证明；冲突进入异常处理。真实apply必须另行批准。干净独立库与原管理员账号不可合并默认库viewer。
3. 批准普通SSO、自动绑定、历史归属、JIT资格/企业域与60/60/30/60分钟候选会话期限，以及EnterpriseMaxAge。禁止填写synthetic引用或编造SLA。
4. 将真实用例分开授权：已有账号关联写入；真正新员工JIT创建新用户。重复登录、退出后重登和到期重登各自消耗新授权事务，必须事先明确总笔数，不能在“一笔测试”中自动追加。
5. 独立入口仅服务器127.0.0.1:18090；APP/PG/Redis无公开端口，来源限制和SID必要出站须仍有效。保留受控时间窗口与拒绝来源实测证据。

## 现有Chrome SSO与路由

已验证的无凭据链路：本机127.0.0.1:18190通过SSH到服务器127.0.0.1:18090；专用Chrome仅将KnowHub域映射到本机18190，SID继续官方HTTPS。页面/资源/API/空callback全部通过，隧道结束关闭。

**日常有SSO的Chrome尚未完成同样证明。** hosts指向172.30.232.42:80会进入默认服务，不得开始真实授权。不能给已运行Chrome追加启动参数假装生效，不能复制/导出/读取Cookie。

可选最小方案：经用户另行同意，在关闭所有日常Chrome进程后，以原Profile重新启动受控Chrome，仅为KnowHub域添加本机隧道路由及绕过代理参数，不创建新的无SSOProfile、不读Cookie、不改全局DNS/代理。用户需先保存工作；退出后移除临时参数。此方案涉及日常浏览器生命周期，目前尚未执行或视为已批准。若用户不批准，则选择经批准的同源专用浏览器路由方案，并另行安排建立SSO的事务授权。

由自动化仅采集安全path/status/remote-address，核对页面、一个静态资源、配置API及空callback均进入隧道，并关联独立服务器端版本。真实SID服务只访问官方HTTPS。实际上下文路由任何不明即停止，不要求用户提供HAR/Token/完整URL。

## 目标配置（仅审批后加载）

```
SOURCEID_FORCE_REAUTH_PROMPT_LOGIN=false
SOURCEID_BASIC_ENCODING=oauth_form
SOURCEID_ACCOUNT_SCOPE=enterprise_all
SOURCEID_ADMISSION_MODE=oidc_sso_approved
SOURCEID_SESSION_MODE=local_approved
SOURCEID_LOCAL_REFRESH_ENABLE=false
SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED=false
SOURCEID_LOGOUT_ENABLE=false
AUTO_MIGRATE=false
```

OIDC_AUTH_ENABLE、contract/email/auto/SSO/enterprise政策门禁需按实际批准开启并加载真实引用，protocol_only才可关闭。已有账号用例可先保持JIT_ENABLE=false；新员工用例必须独立批准JIT_ENABLE、JIT_POLICY_CONFIRMED、JIT_POLICY_REF及精确企业域。均不按角色分流。

SOURCEID_LOCAL_ACCESS_TTL/ABSOLUTE_TTL/IDLE_TTL/ADMISSION_MAX_AGE的60m/60m/30m/60m仅候选；EnterpriseMaxAge、valid_until由真实同步周期/延迟/恢复预算决定，不照抄Mock值。

## 服务端自动采集

启动单笔run_id监测，采集前记录账号/成员/ACL和绑定/Session/JIT计数私有基线。使用已有白名单诊断的authorize、callback_state、token、id_token、userinfo、identity_mapping、local_session、callback_result；auth_time仅记录签名Token字段存在性/固定分类，普通SSO不强制新鲜度证明。

现有 `protocol_monitor_runtime.py` 固定默认项目/479版本，且 `protocol_monitor.py` 将local_session视为协议测试违规。这两个旧worker不得直接用于独立试点业务验收。本轮已审计可复用的白名单与关联规则；获准配置及真实批次就绪后，启动前还须校准独立项目的业务采集适配：目标固定f40/DB3029，预期identity_mapping/local_session/callback_result为PASS，追加服务端账号/Session不变量与实际Portal/auth-me断言，普通SSO不强制prompt/auth_time。适配器必须先以Mock验证证据先保存再恢复、重复事务拒绝和日志体积限制，未完成不得宣布真实监测READY。这是采集器适配，不需要重写或部署OIDC认证逻辑。

只关联本次唯一新增授权事务，第二笔authorize/无法归属callback立即CORRELATION_AMBIGUOUS，不拼接历史Trace。用户看到SID页面不是结束；等待认证完成并返回或原事务期限内超时，过期不延长、不重试。不保存Code、Token、Cookie、原始身份、完整URL或HTTP dump。

身份与Session核对在受控进程内按本次Trace/事务及服务器私有关联完成，仅输出同一原user_id/角色/成员/ACL的布尔结果、新用户计数与零默认权限等固定字段。若现有阶段未记录则NOT_CAPTURED，不能用Token200推断Portal成功。

## 通过标准

已有账号：完整OIDC通过→可信历史归属→原user_id绑定→Access-only Session→Portal→auth/me原账号→原KB和文档可读→实际存在的未授权跨空间KB拒绝。原管理、owner、跨空间与成员权限逐项不变；新建用户0。不同角色同一逻辑，没有管理员额外登录流程。

新账号：确认无历史账号/归属冲突且符合正式开户资格→只创建一个tenantless普通用户→无管理员标记、无默认成员/敏感KB访问→Portal/auth-me→非法SwitchTenant拒绝→无Refresh响应/Cookie/记录。再次登录同一用户的验证需额外批准新事务。

两类均检查Access受Session约束、合法切换不延长绝对期限、本地退出旧Access401、定向撤销与绑定撤销生效。会话到期实测所需等待或合成时钟/到期注入必须明确，不将Mock到期当作真实60分钟实测。

任何签名/nonce/身份/权限异常立即停止，不切Basic、不弱化校验、不自行发起第二笔。先保存脱敏证据，再恢复关闭门禁、撤销本轮Session/Access；保留真实绑定/开户/审核审计，不自动删除真实员工。

## 恢复和停止

恢复测试前受保护配置，仅重建独立APP；核对prompt=false、protocol_only=true、contract/email/auto/JIT关闭、session=external。必要时只APP切回09e6d86db不可变镜像，DB3029、Frontend832和文件保持；不降迁、不切默认库、不恢复raw。关闭隧道/临时浏览器参数，日常Profile/Cookie保持。记录真实绑定/JIT写入与撤销结果，完成即停止。
