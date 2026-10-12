# 真实 SID 联调预检查

状态：**BLOCKED_EXTERNAL / NOT_TESTED（真实登录）**。已完成本机/源码及用户指定Linux测试机的Docker、目录、部署只读检查，没有启用服务、真实登录、登出、映射或生产变更。完整只读结果见 [P6检查报告](SOURCEID_P6_PRECHECK.md) 和 [测试机环境报告](SOURCEID_TEST_HOST_ENVIRONMENT_REPORT.md)，正式契约见 [契约清单](SOURCEID_CONTRACT_CHECKLIST.md)。

## 基线与当前结果

代码SHA `8ae91bc4439dcc93be11844ada5ec1479f2730bc`；本轮起始HEAD `39bfe24ddc4f24971a01b906c4c4656b362ed73a`；分支feature/knowhub-sourceid-oidc。两者运行代码一致；[CI](https://github.com/suoak/WeKnora/actions/runs/38242327216)成功。P6文档变更不等于新的代码验证。

| 检查 | 当前结果 | 需要收口 |
|---|---|---|
| 功能/配置/路由 | STATIC_CHECKED | 正式值未提供，配置未部署 |
| 本机及Linux测试主机SID DNS/TCP443/TLS | Windows本机19:41、Linux主机23:37均PASS，TLSv1.3 | 实际应用容器网络命名空间未测；详见环境报告 |
| 本机KnowHub DNS/TCP80/有限HEAD | DNS/TCP PASS；批准两路径各一次HEAD为401、无重定向、请求ID存在 | HEAD不能证明GET callback/POST通知handler；运行版本、隔离数据库、SID注册一致性NOT_TESTED |
| issuer/Discovery/JWKS/Token | BLOCKED_EXTERNAL | 只有企业SID基地址，不从示例猜完整URL；未扫描发现路径 |
| Profile编码/client auth | BLOCKED_EXTERNAL | 当前支持Bearer GET/两类POST；Token仅urlencoded POST与post/basic认证 |
| nonce/state/PKCE | MOCK验证存在；SID兼容NOT_TESTED | SID方确认state/nonce、S256支持，不关校验 |
| SID通知入站/来源/ACK | BLOCKED_EXTERNAL | 实际发送节点、可信入口、会话等价、编码/重试契约均缺 |
| 正式域名安全用于测试 | NOT_TESTED | 无生产域名拓扑和隔离证明，不能视为本机能访问就可联调 |
| 审核员工/原ID/映射 | NOT_TESTED | 优先普通测试员工；真实绑定先dry-run差异再确认 |
| 日志安全 | BLOCKED_LOCAL_GATE | 通用form-body采集与默认代理访问日志有风险，真实测试前须窄补丁/部署脱敏及验收 |
| 应急管理员 | 源码/CI可用；现场NOT_TESTED | 独立授权后测试真实入口/凭据/权限，不修改管理员字段绕过 |

本机当前进程未设置四个开关；检查时工作区不存在.env/.env.local/.env.production；这不说明用户另一个受控服务进程是否已配置Secret。本轮没有读取真实凭据。默认四开关false，全部保持关闭。

## 安全配置与加载方式

配置源为 `internal/config/config.go:LoadConfig` 的 config.yaml 和进程环境；OIDC/SID专用环境覆盖由applyOIDCEnvOverrides读取。Go服务没有自动dotenv加载。Docker Compose app使用env_file及environment透传；只挂载secret文件无效，当前无OIDC_AUTH_CLIENT_SECRET_FILE或SOURCEID_LOGOUT_SECRET_FILE处理。

使用 [正式占位模板](SOURCEID_P6_CONFIG.env.example) 提供非秘密值与变量名；Secret仅 `OIDC_AUTH_CLIENT_SECRET`，内部网关凭据仅 `SOURCEID_LOGOUT_SECRET`，审核工具DSN仅 `SOURCEID_ENROLL_DSN`。这些不进入浏览器、公共config、URL或CI。Issuer与端点保持正式签署值；OIDC_AUTH_ENABLE和三项SOURCEID门禁暂不启用。**不要仅把SOURCEID_ENABLE=false而开启泛用OIDC来探测**：泛用路径可能按邮箱开户，违反审核映射约束。

由用户/运维在独立受控终端、服务管理器或批准的secret manager配置真实密钥；不在聊天提交、不把密钥放命令参数。直接运行Go时应由受控启动器传递环境；Compose按实际部署方案注入，禁用shell trace，不输出展开后的compose config或docker inspect。不要把模板中的 `<...>` 当正式有效参数。

文件边界：如必须落地凭据，放仓库外的受限目录；Unix文件0600/目录0700、Windows移除无关继承授权，仅指定服务身份与受控管理员可读，备份同样受限。**同一用户运行的Agent通常也能读同用户文件；Agent具有管理员权限时能绕过或改变ACL。ACL和gitignore不能承诺对本地Agent保密。** 真正隔离应使用独立服务身份、不可由Agent任意查询的外部secret托管与授权执行器。任何被同权限进程继承的环境也可暴露，不把Secret配在Codex宿主进程。本轮Agent不打开凭据文件，由实际服务进程读取。

OIDC配置是启动快照，没有已实现热更新。修改文件后运行实例仍可能使用旧环境；Compose仅restart不更新创建时环境，需批准后重建受控实例；裸进程需受控停止/重启。回退关闭四开关并重建、撤销已有SID会话，不能以关闭配置当撤销，详见 [回退门禁](SOURCEID_P6_RELEASE_GATE.md)。本轮未进行任何启动/重建。

## 开始员工浏览器操作之前

必须先具备：正式可信端点与签名/nonce契约；回调到隔离测试实例/库的证据；员工普通权限与审核资料、已确认工号/状态/会话字段；映射dry-run差异及绑定确认；日志安全通过；安全密钥注入、应急入口和退出方案。未知会话字段会让当前SID业务登录安全失败，不能绕过取信息。

当前还没有可执行的真实登录链接。准备工作完成后，再向用户提交：测试环境与commit、批准callback的路由证据、测试员工权限、执行一次登录的动作范围/会话影响、日志/密钥验证结论、回滚方案；请求**开始真实员工登录**的单独确认。

获批后的浏览器步骤：

1. 用户自行打开批准的KnowHub隔离测试入口，核对域名/环境；不要用已有生产管理员会话参与。
2. 点击已有“企业SID”OIDC按钮，由后台生成state/nonce（如确认支持则S256 PKCE）并跳转正式SID。
3. 用户仅在正式SID页面手动输入账号密码；Agent不模拟密码流程、不读浏览器保存密码/cookie、不导出HAR。
4. SID自动回调固定URI；后端立即交换短TTL code，验签并比较nonce/subject，再验证审核映射。不要复制code进聊天。
5. 观察成功落地或安全错误类别；仅记录请求ID/校验结果。按批准的诊断机制由服务进程输出字段名/类型/层级、bool结果；现有公开接口没有完整schema诊断输出。
6. 若校验失败、字段缺失或未知映射，停止并记NOT_TESTED/FAIL，不关闭校验或重复无限尝试；必要最小适配另审阅并专项/云端验证。

若需要先发现字段再做审核绑定：优先取得SID官方脱敏契约；否则需评审一个在完成state/nonce/ID Token验签及UserInfo subject一致性后仅输出schema、不开户/不发行本地token/不创建身份绑定的受控诊断通道。当前未实现，不以泛用OIDC或原始日志替代。诊断代码和首次真实交互同样要经过审批与验证。

真实SID登出有独立确认门禁，不能由一次登录批准推定。生产路由/数据库/绑定/部署/主分支合并均分别等待确认。
