# OA 测试站点持续启用

2026-10-18，执行目标仅为打包机 `172.30.232.42`、`knowhub-company-test`，原域名 hosts 指向测试机的工作站可继续访问80/8080。

状态：**核心功能开发完成，真实业务验证通过，测试环境启用；正式生产发布待与飞书个人文档联合上线。**

## 实际执行

- APP保持 `5f988be51779f1a22bd9d027b7503fe6a8749f62`，镜像 `sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b`；Frontend保持832b6652a，DB3030 clean。
- 仅受控停止并重新创建APP。停止业务写入后成套备份DB、文件、完整Compose顺序、解析后配置及原环境；没有构建、拉取、迁移、切库或替换其他服务。
- 新增最后一层 `persistent-oa.json`，记录于服务器受保护的 `private/active-persistent-oa.json`。后续运维必须沿用该完整Compose顺序，不能用旧临时操作卡覆盖。
- 授权依据：本轮用户明确批准，引用 `user-conversation:2026-10-18:company-test-oa-persistent-v1`；执行人为Codex受控SSH进程（aqyfzx），不是虚构的公司生产政策或其他负责人签字。

## 进程实际配置

OIDC、SourceID、身份契约、可信邮箱、自动绑定、可信邮箱政策、普通SSO政策和JIT政策均已开启；`SOURCEID_PROTOCOL_ONLY=false`。

| 配置 | 实际值 |
|---|---|
| SOURCEID_ACCOUNT_SCOPE | enterprise_all |
| SOURCEID_ACCOUNT_LINK_MODE | verified_email_policy |
| SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED | true |
| SOURCEID_JIT_ENABLE / SOURCEID_JIT_POLICY_CONFIRMED | true / true |
| SOURCEID_JIT_EMAIL_DOMAINS | ruijie.com.cn |
| SOURCEID_ADMISSION_MODE / SOURCEID_SESSION_MODE | oidc_sso_approved / local_approved |
| SOURCEID_LOCAL_ACCESS_TTL / SOURCEID_LOCAL_ABSOLUTE_TTL | 60m / 60m |
| SOURCEID_LOCAL_IDLE_TTL / SOURCEID_ADMISSION_MAX_AGE | 30m / 60m |
| SOURCEID_LOCAL_REFRESH_ENABLE / SOURCEID_FORCE_REAUTH_PROMPT_LOGIN | false / false |
| SOURCEID_BASIC_ENCODING | oauth_form |
| SOURCEID_LOGOUT_ENABLE / SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED | false / false |
| AUTO_MIGRATE | false |

绑定、可信邮箱、准入、本地Session和JIT政策引用均为上面的持续测试授权引用。没有定时关闭OA或按一次登录自动恢复旧门禁。未开启SID统一登出；本地退出仍有效。

## 现场验证

- 80/login及静态资源200；8080/login保持后端预期401；两个端口health和OIDC配置200；无参数callback302至invalid_state，环境标记均为company-test-verified-email-db3030。
- APP网络内官方SID Discovery（`/oidc/.well-known/openid-configuration`）及JWKS通过DNS/TLS和RSA公钥检查；没有调用authorize、Token、UserInfo。
- 两端口auth/me、知识库、MCP及飞书个人/管理员API匿名访问均401。
- 未授权Docker bridge对80/8080访问拒绝，对PG/Redis连接拒绝；内部服务没有新增主机端口，网关网络与其他容器/挂载/端口保持原状。
- 原3用户、1绑定、0JIT，成员、管理员、owner、ACL和知识库基线一致；3文档、2717有效分块/向量引用完整，实际文件清单和内容校验一致。
- 本轮没有新真实身份写入或SID认证。复用同镜像已有账号真实验证、空实例真实JIT及隔离权限/到期测试，未将无凭据检查冒称新一轮真实登录PASS。

备份和APP-only恢复脚本：服务器 `/home/aqyfzx/ai/knowhub-company-entry-20261017/private/oa-persistent-20261018T043810Z/`，其中 `rollback-app-only.sh` 可恢复启用前配置；本轮未触发回滚，也未为演练再次中断服务。

## 使用边界与飞书后续

客户端hosts、页面、静态资源、API、callback需继续指向测试机；SID使用官方HTTPS。测试/生产共用域名时，浏览器同源Cookie及本地存储不能靠后端密钥实现物理隔离，应避免同一浏览器上下文交替使用生产与测试；服务器JWT/AES、DB、Redis和文件仍独立。测试新账号/绑定禁止同步回生产。

飞书现有M1/M2/M3成果已含统一应用配置、独立个人OAuth、用户Token刷新、单链接导入、手动增量重同步与租约保护。尚缺云盘分页浏览/选择、定时增量同步及完整失权处理体验；先补云盘选择。当前库飞书应用配置、个人连接、导入记录均为0，未复用SID凭据或启用旧同步任务。后续开发不重新设计OA，不改当前已验收镜像；联合候选完成后另行验收部署。

生产政策、完整公司邮箱域和身份类型、容量、完整Trace/Token HTTP观测及正式发布审批仍待并行收口；不阻塞飞书开发，本轮未操作生产。
