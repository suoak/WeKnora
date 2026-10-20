# KnowHub OA 登录 UI 独立发布报告

日期：2026-10-15。本次范围仅为前端 OA 入口展示；实际 frontend 替换必须等待用户下一次确认。

最终状态：**CODE_AND_CI_PASS / CANDIDATE_IMAGE_READY / NOT_DEPLOYED**。

## 修改与提交

功能提交：`888234eafc8abda2f82137e90aecd2f367388cec`。后续只修正视觉测试工具与测试夹具，最终候选源提交：`1ba24ef5f9a730bb98d9ade6a5a5d141ea49292b`。两者 `frontend/src` 完全一致，不把测试脚本修正称为应用行为修改。

| 文件 | 修改 |
| --- | --- |
| `frontend/src/views/auth/Login.vue` | 复用已有绿色 OA 次级按钮；办公楼图标、浅绿背景、绿边框与文字、Hover/Active/Focus/Loading/Disabled；flex 分隔线避免覆盖文字 |
| `frontend/src/views/auth/oidcLoginPresentation.ts` | 依据现有 API 的 SourceID 专属标记选择 OA 展示，不按自由文本 Provider 名称猜测 |
| `frontend/src/api/auth/index.ts` | 补充已存在的 sourceid_basic_encoding 返回字段类型，不改请求或协议 |
| 五种 locale 的 `auth` 部分 | 中文“使用 OA 登录”“或使用以下方式登录”及英文/日文/韩文/俄文对应文案 |
| `oidcLoginPresentation.test.ts`、`tests/oa-login/oa-login-ui*.mjs`、OA UI workflow | 展示选择、生产构建前后截图与事件/布局/状态回归 |

原工作区语言文件中的 MCP 改动通过 Git index 单独排除，其他未提交 MCP、飞书、后端、文档和实验文件没有混入功能提交。所有应用修改均为上述前端文件，没有更改锁文件、后端、迁移或服务器认证配置。

SourceID/OA 判断使用后端 `GetOIDCConfig` 仅在 SourceIDEnable 时返回的非空 `sourceid_basic_encoding`（oauth_form/raw）。其他 Provider 继续使用 provider_display_name；无名称时保留原 OIDC fallback。已部署 431226c3b 已提供该字段，不需要后端配套发布。未来若移除此标记，需同步更新此展示判断，当前不会根据“企业 SID”文字误识别其他 Provider。

点击事件仍调用原 handleOIDCLogin / getOIDCAuthorizationURL，callback、Token、state、nonce、PKCE、Basic、绑定和 Session 全部不变。没有重画公司 OA Logo或引入图片依赖；使用项目已有 BuildingIcon，统一绿色，未添加红色主视觉。

## 页面效果与本地验证

截图来自真实 Vue 的生产构建、本地隔离浏览器、全 Mock API，外部请求阻断。不是设计稿，不接触员工账号。

| 场景 | 修改前 | 修改后 |
| --- | --- | --- |
| 中文桌面卡片 | [前](../../qa/oa-login-ui-release-20261015/before-desktop-zh-card.png) | [后](../../qa/oa-login-ui-release-20261015/after-desktop-zh-card.png) |
| 中文手机卡片 | [前](../../qa/oa-login-ui-release-20261015/before-mobile-zh-card.png) | [后](../../qa/oa-login-ui-release-20261015/after-mobile-zh-card.png) |
| 英文桌面暗色卡片 | [前](../../qa/oa-login-ui-release-20261015/before-desktop-en-dark-card.png) | [后](../../qa/oa-login-ui-release-20261015/after-desktop-en-dark-card.png) |
| 英文手机卡片 | [前](../../qa/oa-login-ui-release-20261015/before-mobile-en-card.png) | [后](../../qa/oa-login-ui-release-20261015/after-mobile-en-card.png) |

[修改前结果](../../qa/oa-login-ui-release-20261015/before-results.json)、[修改后结果](../../qa/oa-login-ui-release-20261015/after-results.json)。本地截图构建使用 3edd434ec，其应用源码与最终候选完全一致，之后两个提交只改测试夹具；最终候选的独立云端截图另外保留对应 SHA 证据。

| 检查 | 结果 |
| --- | --- |
| 前端类型检查 | PASS；干净 worktree，vue-tsc --build |
| 登录/刷新/回调/注册策略/i18n/SourceID 展示测试 | 45/45 PASS |
| 两次前后生产构建 | PASS；存在原有大 chunk 警告，未扩大范围拆包 |
| 生产页面浏览器检查 | 修改前 4/4、修改后 10/10 PASS |
| 中英桌面/移动端、英文暗色 | 按钮边缘、宽、高、圆角一致；分隔线不重叠，无横向溢出 |
| 绿色主题 | 背景渐变与主按钮颜色前后计算样式严格相同，截图人工检查通过 |
| 交互 | Hover 反馈、键盘可见焦点、密码登录期间 Disabled、OA Loading 文案、防重复点击、原授权事件通过 |
| 其他 OIDC Provider | 自定义名/无名 fallback、错误反馈后恢复可点击通过；不显示 OA 图标/样式 |
| OIDC 关闭 | 只保留原密码入口，OA 按钮/分隔线隐藏 |
| ESLint / Prettier | NOT_CONFIGURED：当前项目无对应脚本/配置及已安装 CLI，不虚构 lint 通过；git diff --check、TS/Vue 检查与构建通过 |

早期视觉工具依赖 Vite 控制台标签而等待超时，后改为生产构建 + 有界 HTTP readiness；还修正 SourceID Mock 标记遗漏与测试预期 UTF-8 编码。未因此改变应用认证。旧超时/取消运行不算 PASS。合成前端回归不能证明历史真实 Chrome STATUS_BREAKPOINT 已解决，本轮未真实员工复测。

最终 SHA 的 [OA UI CI 38761245408](https://github.com/suoak/WeKnora/actions/runs/38761245408) 全部 SUCCESS，包含类型检查、45 项测试、生产前后构建、10 项最终视觉/事件检查和完整构建。其 [桌面中文卡片](../../qa/oa-login-ui-release-20261015/ci-1ba24ef5f/after-desktop-zh-card.png)、[移动端卡片](../../qa/oa-login-ui-release-20261015/ci-1ba24ef5f/after-mobile-zh-card.png)、[完整结果](../../qa/oa-login-ui-release-20261015/ci-1ba24ef5f/after-results.json) 是最终候选精确提交的云端证据。

同一 SHA 的 [SourceID 综合 CI 38761245288](https://github.com/suoak/WeKnora/actions/runs/38761245288) 亦全部 SUCCESS：frontend、PostgreSQL、真实业务 Handler + Mock SID browser、Linux race/认证/MCP/飞书回归与构建。两项 CI 的 SHA/job 结果见[机器收据](evidence/oa-ui-ci-20261015.json)。本轮不以 CI 通过宣称真实 OA 业务放行或真实浏览器崩溃已修复。

## 独立发布和回滚约束

打包机已完成仅 frontend 候选构建，沿用阶段 B 的 Node/离线 SheetJS/npm 缓存及 runtime，无锁文件变化、不构建 app。[构建启动收据](evidence/oa-ui-candidate-build-start-20261015.json)、[构建阶段记录](evidence/oa-ui-candidate-build-status-20261015.json)、[候选镜像与兼容性实测](evidence/oa-ui-candidate-inspection-20261015.json)。

```text
候选 tag: knowhub-sourceid-phaseb/frontend:oa-ui-1ba24ef5f
image ID: sha256:63146a134fc3be75b663a1ac6ddba205be2b0a3c881891a3ba29a28d6a04f48a
OCI revision: 1ba24ef5f9a730bb98d9ade6a5a5d141ea49292b
独立目录: /home/aqyfzx/ai/knowhub-oa-ui-candidate-1ba24ef5f
```

镜像内 237 个文件与该次构建 dist 逐一摘要相同，Login chunk 同时包含 OA 展示判断和其他 Provider 名称保留逻辑。临时 nginx 容器未发布宿主机端口，通过桥接地址检查 `/login` HTTP 200、到现有 app 的 `/api/v1/auth/oidc/config` HTTP 200，SourceID 标记/oauth_form 兼容。临时容器已精确移除；默认项目容器及挂载前后不变，DB 为 3027 clean。没有真实登录、Token 兑换或数据写入。

已生成 0600 的 `frontend-ui-pin.yml` 和 `frontend-ui-rollback-pin.yml`，只含 frontend image/pull_policy，不包含环境或凭据。它们位于上述独立目录，均未应用。当前 frontend 仍是 `309f4982cf11…`、app 仍是 `ceac394ee463…`，实际 UI 尚未改变。

北京时间 **23:46:42** 的[最终只读核查](evidence/oa-ui-current-services-unchanged-20261015.json)再次确认 app/frontend 仍为 431226c3b、DB3027 clean、oauth_form、protocol_only=true、contract_confirmed=false、自动/邮箱绑定关闭、local_approved 未启用。OIDC 和专项诊断沿用原现场 true，没有因候选准备改变。

保持 app 镜像 `sha256:b2751ba9c8797b0c2440b9435ad92ba9ab2c30d4a95b0c28a2b1b34e4a4c3af1`（代码 431226c3b）、DB 3027、oauth_form 与现有身份门禁不变。UI 候选 frontend 的 revision 为 **1ba24ef5f**，与 app 不同；这是已限定的纯前端 UI 差异，不声称两者 revision 一样。

当前基础 Compose 加阶段 B candidate-image-pin.yml 同时固定 app/frontend。未来获批时保留它作为第二个文件，再把仅含 frontend 新 image ID 的 `frontend-ui-pin.yml` 放在最后；只执行 `up -d --no-deps --no-build --pull never --force-recreate frontend`。不修改 app 服务、不改阶段 B pin 内容、不改变数据库、依赖服务或数据卷。单用基础 Compose 或忘记最后 override 都可能选到错误镜像。

回滚使用同样的基础/阶段 B pin，再追加仅含旧 frontend ID 的 `frontend-ui-rollback-pin.yml`，只重建 frontend。旧镜像保留为 `sha256:5aa70b6378d4fdbcb173d7a3bd4e4494b1218cafd148e9414684014ca90a3ac1`，不能调用会回退整个 app 的阶段 B 回滚脚本。

获批发布时先备份现用配置和容器元数据，确认 app/frontend image ID、DB3027、门禁仍与本报告相符；发布后检查实际 frontend image ID/revision、/login 及 OA UI、config 代理/本地登录/回调行为，并比较 app/PG/Redis/DocReader 的容器 ID 和卷。失败仅回滚 frontend。真实 OA 登录仍需另行授权。

获批后拟执行命令（**本轮未执行**）：

```sh
docker compose --project-name knowhub-sourceid-business \
  --project-directory /home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05 \
  -f /home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05/compose.yml \
  -f /home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b/candidate-image-pin.yml \
  -f /home/aqyfzx/ai/knowhub-oa-ui-candidate-1ba24ef5f/frontend-ui-pin.yml \
  up -d --no-deps --no-build --pull never --force-recreate frontend
```

回滚只把最后一个 `-f` 改为同目录 `frontend-ui-rollback-pin.yml`，其余相同；仍只操作 frontend，不调用 app 回滚，不执行迁移。未来运维应保留这三个 Compose 文件的顺序，避免下次基础 Compose 操作误选旧 UI。
