# KnowHub 原测试地址蓝绿替换部署报告

日期：2026-10-17，北京时间。**原测试地址已切换至 APP f40b1602f／Frontend832b6652a／DB3029 新副本，80/8080基础路由验收PASS。无需18090入口、SSH浏览器隧道或重启日常Chrome。真实OA业务门禁仍关闭，尚不能开始真实OA登录。**

## 实际部署与入口

| 项目 | 结果 |
|---|---|
| 服务器 | 仅172.30.232.42；生产未访问 |
| 原测试地址 | `http://knowhub.ruijie.com.cn`，沿用用户已配置的测试机hosts |
| 对外入口 | `knowhub-company-entry`网关，80→新Frontend，8080→同一新APP；原路径/Host/callback保持 |
| 新业务项目 | `knowhub-company-test`；目录 `/home/aqyfzx/ai/knowhub-company-entry-20261017/green` |
| APP | `f40b1602f69ce5d88e1e87dc3cefae36e9bde356`；`sha256:742c7ce56ef38ad32548900c8e7f8b264f9f6cd6138043674b91b27557443413` |
| Frontend | `832b6652adf541892545a7eac8b68733f846ab95`；`sha256:ab7845c89fadeb17ad2b68bd924e892f948b893781a50ab79e48385c319ddbe1` |
| 数据库 | 新PG容器中的`knowhub_oa_sso_pilot`，3029、dirty=false；不是旧DB3027 |
| 新数据卷 | `knowhub-company-test-pgdata`、`knowhub-company-test-files`；独立Redis、DocReader临时卷 |
| 保留旧栈 | `knowhub-sourceid-business` APP479416c90／Frontend1ba24ef5f停止并保留原容器，旧PG3027/Redis/DocReader继续运行，数据卷未删除 |
| 独立试点 | 原f40／832／DB3029及文件保持；未将其容器切换到新入口，使用同代干净快照建立另一套副本 |

APP/Frontend使用既有不可变镜像，没有构建、拉取或迁移。APP与Frontend源提交不同是已验收的明确组合，未伪称相同revision。PG、Redis、DocReader、APP均无主机公开端口；Frontend仅保留宿主机loopback18091作为维护入口，不要求用户使用。

## 依赖、备份与切换

切换前检查了端口、Compose顺序、容器/卷、环境依赖、有效认证计数及近期可用日志。最初两个相关TCP连接随后未观察到；没有有效Access、API Key、MCP OAuth客户端或MCP使用记录，容器环境未发现明确依赖旧80/8080的工具。日志窗口没有可用请求分类，**不能因此证明不存在未登记使用者**。另一个测试项目仍使用相同SID callback，保留原状；后续真实验收需独占认证窗口，防止其请求混入新入口。

已知业务差异：旧DB3027有2个用户、0个有效KB、6条MCP端点配置；新干净副本有3个原用户、1个有效KB、0条MCP端点。旧端点/凭据不会自动迁入新库。未发现活跃使用凭证，不代表所有潜在外部工具已确认；旧端点仍完整保留于旧库，需使用方另行批准在新库正常重新配置和签发。8080保留API/MCP兼容路径，并限制为当前获准工作站来源；不承诺其他来源无需授权即可访问。

备份目录：`/home/aqyfzx/ai/knowhub-company-entry-20261017/private/pre-switch`，目录0700、文件受umask077保护；含原Compose各层、运行配置/镜像元数据、旧DB3027与文件、干净DB3029与同代文件。快照时短暂暂停两套APP写入，完成后恢复，正式切换只停止旧APP/Frontend。

- 旧DB dump 379114字节、文件归档10240字节。
- 干净DB3029 dump 21687581字节、文件归档2570240字节。
- 新库恢复仅发生在新建绿环境，未覆盖原库、旧3027或独立3029。
- 新JWT与旧环境、独立试点均不同，新Redis不导入旧缓存；候选AES保留以维持候选加密配置兼容，不导入旧默认环境凭据。新副本历史Access/API Key均失效，无Mock身份/归属/JIT记录；`sessions`是历史对话表，不是认证Session，予以保留。

准备阶段曾发生新库恢复、复制的出站防护内部网段不匹配、网关临时目录只读和预检callback分类问题，均在旧入口仍运行时修正并复验；没有把这些失败计为成功。正式切换前loopback网关检查通过；切换后曾发现Docker桥接的userland-proxy绕行，已加针对新入口目标地址/端口的DOCKER-USER和INPUT规则，真实拒绝来源复验通过。没有修改其他项目的全局规则或清空防火墙。**本次未触发正式入口回滚。**

## 验收及证据边界

| 检查 | 结果与边界 |
|---|---|
| 80/8080、页面、静态资源、API | PASS；Windows临时无凭据Chrome直接原域名访问，全部远端172.30.232.42:80，并有新环境标记；8080健康同标记。未操作日常Profile/Cookie，无隧道、无全局网络变更 |
| callback | 两入口均302→本地`/#oidc_error=invalid_state`，不带认证数据，不发起authorize |
| PC/手机 | 登录页可见，390px布局无水平溢出；[桌面](evidence/company-entry-login-desktop-20261017.png)、[手机](evidence/company-entry-login-mobile-20261017.png) |
| 用户与权限、文件 | 原3用户、管理员标记、原tenant_id、5条成员及ACL选定数据签名与干净基线相同；文件副本相同 |
| 知识数据 | 1个有效KB、3份原文档、2717条非空有效分块及2717条embedding保留；数据库总历史chunks19786不等于有效分块数量 |
| 原管理员业务API | 临时副本中原user_id/home tenant/管理标记保持，auth/me与原KB200；原文件读取、已有向量召回通过（3个命中，无模型调用/原文导出），退出旧Token失效 |
| 本站真实管理员旧密码登录 | **NOT_EXECUTED / 不可直接使用旧密码**：干净副本已按此前要求禁用历史密码。未读取员工密码、未重置新站真实账号。前述API验收使用临时副本中的受控认证，不冒充新站真实密码登录PASS。若确需本站密码验收，需单独批准安全密码设置/恢复流程 |
| 跨空间拒绝 | 临时数据库普通viewer可读允许KB；另一确实存在且未共享的空间KB403，非法SwitchTenant403；夹具不写入上线副本 |
| 自动关联/JIT | 同一镜像、同代数据临时副本Mock enterprise_all及JIT PASS：重复登录同一user_id、原权限保持；JIT仅一个无特权用户，无默认空间、无Refresh，SwitchTenant不延长期限，多设备退出隔离/撤销通过 |
| MCP/飞书 | 本站未认证接口401，认证边界保持。旧6条MCP端点未迁移；未使用真实MCP Key、未调用飞书业务或真实外部模型，不宣称已完成这些外部业务的端到端验收 |
| SID网络 | 新APP内DNS、TLS1.3证书/主机名验证、官方Discovery/JWKS200，存在RSA公钥；未调用真实authorize/Token/UserInfo |
| 网络允许/拒绝 | 当前Windows工作站访问80/8080成功；不批准Docker桥接源访问80/8080、PG5432及Redis6379均拒绝。入口仅IPv4，无新增IPv6监听；不批准来源不能借代理或Docker直连绕过 |

证据：[部署/最终核验JSON](evidence/company-entry-deployment-20261017.json)、[Windows浏览器路由JSON](evidence/company-entry-browser-20261017.json)。Mock/API完整结果仅在受保护的`validation`目录，普通报告只保存计数/布尔值；未保存员工原值、Token、Cookie、完整认证URL或原始HTTP dump。没有重新进行容量压测。

## 实际门禁与真实OA前置

运行中：`SOURCEID_ACCOUNT_SCOPE=enterprise_all`、`SOURCEID_ADMISSION_MODE=oidc_sso_approved`、oauth_form、prompt=false、Refresh=false；**OIDC=false、protocol_only=true、contract/email/auto/JIT/enterprise/SSO批准开关全部false，Session仍external**。这只是部署代码和目标模式，未启用local_approved。账号归属、JIT、外部身份、企业Session和有效Access/API Key均为0。没有伪造政策引用或提前加载60/60/30/60分钟候选。

准备状态及剩余事项：

1. 已按本轮配置准备授权，在远端受控进程中将旧测试APP当前SID配置转入新环境，未输出凭据。APP实测Client ID及Secret与旧测试有效配置相等（仅布尔结果），原callback/协议参数保持；OIDC仍关闭。这是配置准备PASS，**不是新的真实Token200证据**。
2. 数据负责人提供被测已有账号原user_id、可信RJGH/历史归属证明及原权限基线；真实批次先dry-run，apply另行批准。未读取或展示员工身份原值。
3. 确认公司级SID账号类型、完整精确企业邮箱域、历史账号范围完整性及JIT政策；已有账号验收时可继续关闭JIT，之后另批新员工用例，不按角色分流。
4. 批准会话期限、同步新鲜度/恢复预算、普通SSO残余风险及真实政策引用。60/60/30/60分钟仍是候选。
5. 将单笔业务采集器目标从原独立试点校准为新`knowhub-company-test`及新入口；不能用固定旧项目的采集器直接开启本次业务。确定独占窗口及A已有账号/B新员工分别授权的事务笔数。

用户现在可直接通过原地址检查页面和基础路由。**尚不具备立即点击OA完成真实业务登录的条件**；剩余是历史归属、政策审批、新项目采集校准与单笔授权，不再需要独立18090或Chrome隧道操作。真实认证获批后仍严格验证state/nonce/RS256/issuer/audience/时间/UserInfo-sub，不因入口替换降低校验。

## 快速回滚与维护限制

受保护的一键入口回滚：`/home/aqyfzx/ai/knowhub-company-entry-20261017/private/rollback-entry.sh`（已通过shell语法检查，本轮未实际执行回滚）。它停止新入口，移除仅此次新增的入口防火墙规则，启动保留的旧APP，最长等待90秒健康后启动旧Frontend并reload。恢复原479/1ba/DB3027及原配置；不降迁、不恢复raw、不删除新旧数据库或文件，不触碰独立试点。

新入口使用独立网关Compose；变更或重建网关导致IP变化时必须重跑受保护的定向防护脚本和允许/拒绝来源测试。宿主机防火墙未声明重启持久化，因此公开入口设为`restart=no`，主机/Docker非计划重启后需运维先重施规则再启动并复核，避免无防护自动恢复。应用内部服务仍可独立重启；此维护限制不隐瞒为高可用。

新环境产生真实业务写入后回滚入口不会合并两库数据，需冻结写入、保留新库/文件并核对差异，不可把旧dump覆盖新写入。源schema79原业务库及原文件卷未变。正式生产容量、发布与公司全员开放仍需单独授权，本次未操作生产。
