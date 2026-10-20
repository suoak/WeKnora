# KnowHub OA 登录 UI 实际前端发布报告

日期：2026-10-15。状态：**FRONTEND_DEPLOYED / ACCEPTANCE_PASS / NO_ROLLBACK**。

已按用户授权只替换打包机 `172.30.232.42`、Compose 项目 `knowhub-sourceid-business` 的 frontend。没有重建 app、PostgreSQL、Redis、DocReader，没有迁移数据库、真实 SID 授权或企业绑定。原有 OIDC/诊断开关及企业放行门禁保持不变。

## 1. 实际运行版本

| 项目 | 发布前 | 发布后 |
| --- | --- | --- |
| Frontend revision | `431226c3b1bd5338ec06c1bd0520e637d5fed398` | **`1ba24ef5f9a730bb98d9ade6a5a5d141ea49292b`** |
| Frontend 容器 | `309f4982cf11…` | `7b624ec2ee90…`，running、restart_count=0 |
| Frontend 端口 | `0.0.0.0:80` | 不变 |
| App revision | `431226c3b1bd5338ec06c1bd0520e637d5fed398` | 不变；实际 system/info 返回 `431226c3b` |
| App 容器 | `ceac394ee463…` | **同一个容器，启动时间/镜像/挂载不变，healthy** |
| App 端口 | `0.0.0.0:8080` | 不变 |
| 数据库 | `3027 / dirty=false` | 不变 |

最终 Frontend image ID：

```text
sha256:63146a134fc3be75b663a1ac6ddba205be2b0a3c881891a3ba29a28d6a04f48a
```

App image ID 保持：

```text
sha256:b2751ba9c8797b0c2440b9435ad92ba9ab2c30d4a95b0c28a2b1b34e4a4c3af1
```

**APP/Frontend 是不同 revision**，属于已验证且获批的纯前端 UI 发布；没有伪称前后端同版本。两者的 OCI revision、实际运行 image ID 和 app 二进制版本均已核实。[候选与 CI 来源](KNOWHUB_OA_LOGIN_UI_RELEASE_REPORT.md)。

发布开始：北京时间 **23:53:42**（UTC `15:53:42`）；服务验收完成 **23:53:54**；独立收尾复核 **23:54:50**。

## 2. 前置核查、备份及实际命令

前置核查精确通过当前 app/frontend 容器 ID、旧 image ID、revision、DB3027 clean、oauth_form 和各项安全开关；候选 image ID/revision 与审批一致，旧镜像及 rollback pin 存在。本地合成账号在发布前登录/退出验收也通过。

受保护备份目录：

```text
/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05/private/oa-ui-frontend-backups/20261015T155342Z
```

已备份基础 Compose、阶段 B pin、前端升级/回滚 pin、实际文件顺序、容器白名单元数据和原 frontend nginx 模板/API proxy 配置。目录 0700、文件 0600。未输出 env、完整 inspect、compose config 或敏感认证值。数据库未修改 schema 或数据结构，不为本次 UI 发布重新备份/恢复数据库。

实际使用以下**三个文件，顺序完全一致**：

```text
1. /home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05/compose.yml
2. /home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b/candidate-image-pin.yml
3. /home/aqyfzx/ai/knowhub-oa-ui-candidate-1ba24ef5f/frontend-ui-pin.yml
```

执行用户批准的 `docker compose --project-name knowhub-sourceid-business --project-directory ... -f <1> -f <2> -f <3> up -d --no-deps --no-build --pull never --force-recreate frontend`。最终 frontend 容器标签实际记录同样三个文件，最后一个 pin 已覆盖旧前端镜像。没有运行其他服务的 up/restart/down、build/pull 或迁移命令。

## 3. 实际页面与缓存验证

Windows 专用自动化 Chrome **直连打包机 IP**、`--no-proxy-server`、新浏览器上下文、禁用浏览器缓存、阻止 Service Worker；外部域名及 OIDC 授权 URL/callback 请求禁止访问。没有点击 OA 按钮，不发 code 或 Token 请求。所有浏览器窗口已关闭，没有建立 SSH 隧道。

| 场景 | 发布前真实页面 | 发布后真实页面 |
| --- | --- | --- |
| 桌面 1440×1000 | [前](../../qa/oa-login-ui-deployment-20261015/before-desktop-card.png) | [后](../../qa/oa-login-ui-deployment-20261015/after-desktop-card.png) |
| 手机 390×844 | [前](../../qa/oa-login-ui-deployment-20261015/before-mobile-card.png) | [后](../../qa/oa-login-ui-deployment-20261015/after-mobile-card.png) |
| 暗色桌面 | [前](../../qa/oa-login-ui-deployment-20261015/before-dark-card.png) | [后](../../qa/oa-login-ui-deployment-20261015/after-dark-card.png) |

真实页面检查发布前 3/3、发布后 3/3 PASS：[前收据](../../qa/oa-login-ui-deployment-20261015/before-results.json)、[后收据](../../qa/oa-login-ui-deployment-20261015/after-results.json)。

- 三场景均显示 **“使用 OA 登录”**、“或使用以下方式登录”，带现有办公楼图标；浅绿色次级背景、绿色边框/文字，原密码登录按钮仍为绿色实心。
- 按钮边缘、宽度及高度一致；分隔文字不重叠；无横向溢出，pageerror=0。
- 页面渐变背景与主按钮颜色的前后 computed style 完全一致；桌面/移动/暗色截图人工可复核。
- Windows 实际加载新的 `/assets/main-CJrpCTow.js`、`/assets/Login-BwWSdVPg.js`，均 HTTP200。浏览器收到的字节 SHA256 与新容器内文件逐一相等，与发布前资源不同，排除本次验证命中旧缓存。
- 未在现场切换服务器 Provider 配置。其他 OIDC Provider 保留名称/fallback/原事件的结论来自**该候选精确 SHA 已通过的专项 CI**；本轮没有把现场 SourceID 页面截图当成其他 Provider 的真实现场验收。

## 4. 接口、本地账号及安全门禁

| 实际检查 | 发布前后结果 |
| --- | --- |
| `/`、`/login` | HTTP200 |
| 经 frontend 代理 `/api/v1/auth/oidc/config` | HTTP200，oauth_form、enabled=true，原批准 callback 完全不变 |
| 既有专用合成本地账号登录 | HTTP200，未创建新账号 |
| `auth/me`、知识库列表、MCP endpoints、飞书本人配置 | HTTP200 |
| app `/api/v1/system/info` | `431226c3b` |
| 本地退出 | HTTP200 |
| 退出后 `auth/me` | HTTP401，证明该本地令牌失效；不是 SID Token 401 |

合成账号密码及本地 JWT 只在受控进程内使用，不打印、不持久化。上述本地登录/退出会产生正常认证记录；“数据库未变化”指 schema、原用户/租户/权限/业务资源及身份绑定未改，不是声称 auth_tokens 等认证记录字节完全不变。

| 运行配置 | 最终状态，均与发布前相同 |
| --- | --- |
| SOURCEID_BASIC_ENCODING | **oauth_form** |
| SOURCEID_PROTOCOL_ONLY | **true** |
| SOURCEID_CONTRACT_CONFIRMED | **false** |
| SOURCEID_EMAIL_BINDING_ENABLE / EMAIL_CONTRACT_CONFIRMED | **false / false** |
| SOURCEID_AUTO_BINDING_ENABLE | **false** |
| SOURCEID_LOGOUT_ENABLE | **false** |
| SOURCEID_SESSION_MODE | 未设置，默认 external；没有启用 local_approved |
| OIDC_AUTH_ENABLE / SOURCEID_DIAGNOSTICS_ENABLE | **true / true**，沿用现场，不代表真实业务放行 |
| OIDC_AUTH_PKCE_ENABLE / AUTO_MIGRATE | **false / false** |

users、tenants、tenant_members、knowledge_bases、外部身份、审核映射、SID/local Session、飞书表等核心行数均与发布前相同。PostgreSQL、Redis、DocReader、停止状态的 sid-mock 的容器 ID、创建/启动时间、镜像、端口、挂载与重启次数相同；所有原数据卷均保留。

收尾受控内存扫描本轮 app 日志610字节、frontend日志10665字节，未发现完整 Authorization、JWT、SID code/AccessToken 模式；未保存原始日志。本轮无真实员工资料输入，此检查不宣称对任意历史个人数据完成全面审计。

## 5. 回滚与交付状态

**本次没有失败，没有触发回滚。**旧 frontend 镜像 `sha256:5aa70b6378d4fdbcb173d7a3bd4e4494b1218cafd148e9414684014ca90a3ac1` 和以下回滚 pin 仍保留：

```text
/home/aqyfzx/ai/knowhub-oa-ui-candidate-1ba24ef5f/frontend-ui-rollback-pin.yml
```

如后续另行批准回滚，仅将三个 Compose 文件中的最后一个换成上述 rollback pin，仍只执行 frontend 的 `up --no-deps --no-build --pull never --force-recreate`。保留 app431226c3b、DB3027、oauth_form；不调用阶段 B 整体回滚脚本。本轮验证文件和旧镜像可用，没有为演示回滚而额外中断服务。

[部署执行收据](evidence/oa-ui-frontend-deployment-20261015.json)、[最终独立复核与静态资源摘要](evidence/oa-ui-frontend-deployment-closeout-20261015.json)包含实际版本、容器、配置顺序、验收及备份位置。

最终为 **APP 431226c3b / Frontend 1ba24ef5f / DB3027 clean**，可访问打包机新版登录页。本轮不验证真实 OA 认证，不消费授权码，不执行邮箱绑定或 local_approved 放行，不操作生产服务器。部署工作在此结束，等待下一阶段指令。
