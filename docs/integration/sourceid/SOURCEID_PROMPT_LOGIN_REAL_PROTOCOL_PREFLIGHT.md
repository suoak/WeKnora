# SID prompt=login 单次真实协议联调预检

2026-10-16。结论：**READ_ONLY_PREFLIGHT_PASS / REAL_LOGIN_NOT_AUTHORIZED**。

服务器及无凭据浏览器路由预检通过，可以准备下一笔协议事务；实际带SID SSO的登录浏览器、临时开启后的进程及最终授权请求仍必须在获批后重新核验。本轮没有启动真实授权，没有修改APP、门禁、网络规则或数据库。

## 1. 当前运行现场

北京时间10:56:18只读核对：

| 项目 | 实测 |
| --- | --- |
| APP revision | `479416c9086d91de4484dbbd2cc5f912cdcdb697` |
| APP image ID | `sha256:85f9d6c052298e7f52e7d7c4533e131ce19326d461521378e90c6df3b01fa49c` |
| APP容器 | `44cffa11c288c5f7b8a22cbd3ccd432d85fd560253016329c692bbab3c3aba2e`，healthy |
| Frontend | 1ba24ef5f；原容器ID未变 |
| 数据库 | 3027、dirty=false；用户2、成员2；绑定/审核/企业Session/local Session均0 |
| 80 / 8080 | Frontend200、APP健康200；配置代理200 |
| OIDC协议 | Client ID=knowhub，Basic=oauth_form，client_secret_basic，scope=openid，PKCE=false，UserInfo=bearer_get |
| callback | `http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback`，原审批地址 |

当前开关：

```text
SOURCEID_FORCE_REAUTH_PROMPT_LOGIN=false
SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED=false
SOURCEID_PROTOCOL_ONLY=true
SOURCEID_CONTRACT_CONFIRMED=false
SOURCEID_EMAIL_BINDING_ENABLE=false
SOURCEID_EMAIL_CONTRACT_CONFIRMED=false
SOURCEID_AUTO_BINDING_ENABLE=false
SOURCEID_LOGOUT_ENABLE=false
SOURCEID_SESSION_MODE=NOT_SET (默认 external)
SOURCEID_LOCAL_REFRESH_ENABLE=NOT_SET (保留现场)
SOURCEID_BASIC_ENCODING=oauth_form
OIDC_AUTH_ENABLE=true
SOURCEID_DIAGNOSTICS_ENABLE=true
OIDC_AUTH_PKCE_ENABLE=false
AUTO_MIGRATE=false
```

protocol_only=true继续阻止业务身份写入。OIDC及专项诊断维持既有true状态，不代表自动关联、local_approved或真实联调已经获准。

## 2. 正式Metadata / JWKS

从当前APP容器使用系统可信CA、正常证书验证、无代理的公开HTTPS请求，Discovery和JWKS均200。未调用authorize、Token或UserInfo，不携带任何凭据。

| 字段 | 实际配置 |
| --- | --- |
| Discovery | `https://sid.ruijie.com.cn/oidc/.well-known/openid-configuration` |
| Issuer | `https://sid.ruijie.com.cn` |
| Authorization | `https://sid.ruijie.com.cn/oidc/authorize` |
| Token | `https://sid.ruijie.com.cn/oidc/accessToken` |
| JWKS | `https://sid.ruijie.com.cn/oidc/jwks` |
| UserInfo | `https://sid.ruijie.com.cn/oidc/profile` |

运行配置没有单独覆盖上述端点，而是通过正式Discovery取得；Issuer、Discovery及callback与预期一致。Metadata声明支持RS256。JWKS当前1个RSA公钥，无kid、无显式alg/use；只记录公钥结构，不保存密钥材料。

Metadata未声明auth_time，**不代表实际ID Token一定不含它**；实际返回值尚未观察。本轮没有读取真实ID Token。真实联调只记录签名auth_time的存在性及固定安全分类；缺失不能补造认证时间，也不能宣称已完成独立新鲜度验证。

公共接口TLS检查只证明连通和配置一致，不证明本应用凭据有效，也不证明SID已执行prompt认证。后者要由单笔真实事务与SID服务端证据确认。

## 3. 历史授权事务

仅聚合查询 `oidc_transactions`，没有读取Key、nonce、verifier或Result内容：

| 统计 | 数量 |
| --- | --- |
| 总数 | 71 |
| 已消费 | 69 |
| 未消费但过期 | 2 |
| 未消费且有效 | 0 |
| 有效授权事务 / 有效结果事务 | 0 / 0 |

当前无需清理或删除历史事务。本轮没有变更记录，也没有用旧Code验证。

真实启用前及收尾再查聚合统计。若出现未知有效事务，暂停开始，不猜测归属或强行消费；先约定停止其他OA操作并等待其过期。现有本地事务期限10分钟，与SID Code签发后90秒不是同一计时点。只有另获批准、且能由受控运行器明确定位本次事务时，才可定向失效本次事务；禁止为了清理而读取原值到对话、删除全部历史或改schema版本。

开关启用前未包含强制请求证据的旧事务会被新服务端策略拒绝；不能复用旧事务跨过策略切换。一个新Code只兑换一次，回调并发保护沿用现有原子消费机制。

## 4. Compose临时开启与恢复

现有顺序均保留，路径前缀 `/home/aqyfzx/ai/`：

1. `knowhub-sourceid-business-test-db1b2cd05/compose.yml`
2. `knowhub-sourceid-phaseb-preflight-431226c3b/candidate-image-pin.yml`
3. `knowhub-oa-ui-candidate-1ba24ef5f/frontend-ui-pin.yml`
4. `knowhub-allroles-035f35e1d/app-release-pin.yml`
5. `knowhub-prompt-login-479416c90/prompt-login-app-pin.yml`

第五层固定强制false，因此仅修改shell环境变量或基础.env无法可靠覆盖。方案是在**获得单次真实认证批准后**才创建private目录0600的一次性第六层：

```yaml
services:
  app:
    environment:
      SOURCEID_FORCE_REAUTH_PROMPT_LOGIN: "true"
```

仅解析预演已通过：以stdin第六层执行 `docker compose ... -f - config --format json`，在受控进程内比较，只有上述一个环境值变化；APP镜像、其他APP字段及所有其他服务解析结果一致。**没有执行up或将覆盖应用到运行服务。**模板见 `business-test/prompt-login-protocol-test-override.yml.example`，不会自动加载。

获批后操作：

1. 备份五层文件顺序、原APP有效安全开关及元数据，记录统计基线；确认没有未知有效授权事务。
2. 追加第六层，固定现有APP镜像，只执行APP `up -d --no-deps --no-build --pull never --force-recreate app`，不改Frontend或依赖、不迁移数据库。
3. 通过 `docker exec APP printenv SOURCEID_FORCE_REAUTH_PROMPT_LOGIN` 只核对该布尔值=true；白名单核对protocol_only=true及其他身份门禁不变。容器环境证据不是授权参数证据，仍须检查本次服务端生成的实际URL只有一个prompt=login、无max_age，并核对新授权事务事实。
4. 一次事务完成或失败，停止本轮入口，移除第六层出现在命令中的引用，只用原五层受控重建APP；第五层重新生效false。不得用true覆盖层参与恢复命令。
5. 实测进程false、APP479416c90、80/8080健康、DB3027、原门禁与其他容器不变，检查没有新绑定/企业Session及遗留有效结果。

恢复正常false不需要降版本。只有APP异常且无法恢复当前版本时，才使用旧035镜像的APP-only回滚pin。回滚命令保留前四层，第五层用 `prompt-login-app-rollback-pin.yml` 替代，既不传新候选pin，也不传临时true层；恢复备份原APP配置。不降数据库、不改Frontend、oauth_form或数据卷。

现有备份：`knowhub-sourceid-business-test-db1b2cd05/private/prompt-login-app-upgrade-backups/20261016T024953Z`，最新发布前DB备份377583字节、0600。旧035镜像及回滚文件可用。未来真实试验前应再记录配置与统计，不能将本轮备份说成未来时点快照。

## 5. 浏览器路由验证

北京时间10:58:07已完成无凭据真实Chrome检查：

- 一次性浏览器用域名局部解析与 `--no-proxy-server` 到Windows临时loopback转发。
- 转发仅连接固定 `172.30.232.42:80`，不按Host重新解析，不存在去往生产IP的上游选择。
- 登录页200，OA入口可见；19个JS静态资源200；auth/config、oidc/config均进入同一测试实例。
- 无参数callback实际进入测试机业务Handler，302/invalid_state；没有Code、state或nonce输入，没有真实SID请求。
- Chrome观测到对该域名的连接均为临时127.0.0.1端口；转发端实际上游为172.30.232.42:80。未修改全局hosts、DNS、系统代理或生产网关。
- 不导出HAR、Cookie、完整查询URL。临时浏览器及转发已关闭。

**范围限制：此检查使用新的一次性浏览器上下文，没有既有SID SSO。预检转发器拒绝Cookie和查询参数，不能用于真实联调，不能用它承载真实Code。**不能据此宣称未来携带SID Cookie的浏览器已经验证。

真实联调采用已验证的专用Chrome+SSH通路：Windows localhost:80 → 加密SSH → 打包机127.0.0.1:80，正式域名仅在实际浏览器局部映射至localhost，绕过Windows系统代理。启动前确认localhost:80可用、SSH对端确为172.30.232.42；业务API及静态资源同一Origin，callback也走该隧道。SID域名保持正常HTTPS解析，不局部映射、不伪造Host/Origin/Referer。

实际登录上下文应复用经确认仍有SID SSO的专用profile；不得导出/复制浏览器Cookie或借用个人profile数据。若该profile没有有效SSO，本次“已有SSO仍重新认证”前提不成立，需暂停并由用户另行确定准备方法，不能悄悄增加一次员工认证。

同一真实profile在点击前重新进行无凭据路由探测、静态资源/API检查和无参数callback，确认请求实际进入隧道及测试机。若该profile触发HTTPS升级、代理或HSTS使callback无法保持批准HTTP路径，应停止，不关闭浏览器或服务端安全机制强行通过。身份数据、完整URL及Header不落盘；不允许以“用户已绑hosts”替代实际路由证明。

## 6. SID人员现场配合

只需配合本笔事务，不重复询问已确认身份字段或prompt支持：

1. 在已有SSO上下文中确认收到本笔prompt=login，确实忽略已有SSO并执行认证和员工准入，没有走普通静默授权。
2. 根据北京时间/UTC及允许公开的关联信息，对照authorize和Token请求；确认Code只兑换一次及客户端认证结果。
3. 说明本笔是否返回签名auth_time及其语义；不向聊天或普通报告提供原ID Token、Code、Basic头、Cookie、工号或邮箱。
4. 继续归档D的可承诺传播上限。Code90秒、停用前未过期Code仍可用已确认，不在本轮执行真实员工停用实验。

SID服务端确认与客户端观察分别记录，不能把“服务端无错误日志”当成强制认证执行证据。

## 7. 一笔协议事务预期结果

获单独授权后：原门禁保持 → 临时开启强制选项 → 校验实际进程及真实路由 → 服务端生成prompt=login授权 → 用户一次认证 → callback/state及单次事务通过 → Token一次兑换 → RS256/JWKS、Issuer、Audience、时间声明、nonce → UserInfo sub一致 → protocol_only返回identity_contract_pending。

预期结果是**协议验证通过并安全停在契约门禁**，不是进入Portal。签名auth_time如有，仅记录SIGNED_AUTH_TIME_VERIFIED或INVALID_OR_STALE；缺少时记录NOT_PROVIDED_CONTRACT_REQUIRED，结合SID执行确认表述证据边界。

前后只读比较账户ID/权限的受控一致性结果及计数，确认无用户或成员修改、无企业身份绑定、无local_approved Session。允许仅新增必要OIDC事务/消费记录及脱敏诊断；不得将“没有企业写入”错误扩大为“数据库完全没有任何认证事务写入”。

## 8. 失败停止及恢复

路由、Token、state、nonce、签名、UserInfo或强制策略不满足，立即停止本笔事务；不自动第二次authorize、不切换Basic/PKCE、不重用Code、不降低验签。

保留允许的脱敏Trace、HTTP分类、固定新鲜度分类及SID确认状态；缺失项写NOT_CAPTURED/UNKNOWN。随后关闭临时入口、以原五层恢复false并核对实际进程和健康。关闭本轮浏览器与SSH隧道，检查本地端口释放。有效未消费事务仅按本次可证明归属的定向方式或等待到期处理，不通过发Token请求清理。

现有历史偶发signature失败继续严格诊断；出现失败不能自行补做第二笔。恢复异常则按APP-only旧035回滚，保留Frontend/DB3027/所有卷和oauth_form。

## 9. 结论及证据

当前环境满足服务器配置、公开接口、回滚及无凭据路由准备前提；没有发现需要修改运行配置的预检故障。还未完成：真实profile已有SSO的确认、临时true进程核验、真实服务端授权URL及SID执行确认、实际auth_time观察。这些依赖下一次单独授权，不能预先记PASS。

- [服务器、事务及Compose只读预检](evidence/prompt-login-protocol-preflight-20261016.json)
- [Windows无凭据Chrome路由证据](evidence/prompt-login-browser-route-preflight-20261016.json)
- [单次真实协议操作卡](SOURCEID_PROMPT_LOGIN_REAL_PROTOCOL_OPERATION_CARD.md)

本轮到此停止，等待用户明确授权单次真实SID认证。没有启用强制选项、身份放行、local_approved，没有切库、停用员工实验或生产操作。
