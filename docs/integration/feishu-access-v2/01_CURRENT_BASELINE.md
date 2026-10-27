# 当前基线与交叉审计

审计日期：2026-10-25（Asia/Shanghai）。本轮为 PR-A 和身份契约基础，**未完成 PR-B 双身份运行时接入，未达到发布条件**。

## 可复核基线

| 对象 | 本轮结果 | 证据 |
| --- | --- | --- |
| origin/main | `ce699bb5065ebaaaa5e180c589e80e5f2aa9f334` | 实际 fetch + rev-parse |
| upstream/main | `e1f9a6166a593a4804a2f99f1d942919c9cc8ea7` | 实际 fetch + rev-parse |
| [PR #50](https://github.com/suoak/WeKnora/pull/50) | open，未合并；head `2a532132eb038e0efd5c5564f2f35693577986b6` | GitHub API，见 evidence/remote-baseline.json |
| 生产版本 | 用户提供的历史确认是 ce699bb5；本轮实时复核 BLOCKED | 尚未识别获准使用的只读运行版本端点，不能把 main SHA 当运行版本 |
| 原工作树 | `feature/knowhub-sourceid-oidc`，HEAD be308d005，有大量修改/未跟踪文件 | 初始 git status；没有移动、清理或暂存这些文件 |
| 本轮工作树 | `E:/code/WeKnora-feishu-access-v2` | 独立分支 `feature/knowhub-feishu-access-v2`，起点 origin/main |

PR #50 仅包含以下五个文件：

- docs/FEISHU_P2_ASSET_DESIGN_20261025.md
- internal/application/service/feishu_legacy_staging.go
- internal/infrastructure/docparser/anydoc_candidate_test.go
- internal/infrastructure/docparser/anydoc_reader.go
- internal/models/vlm/remote_api.go

本轮新增文件与此列表交集为空。没有修改原子发布、解析或资产处理路径。文本无冲突只证明文件边界；之后 PR-B/PR-G 仍须验证行为交叉影响，不能因此宣称集成通过。

## 源码能力清单

| 范围 | 源码事实 | 真实验收边界 |
| --- | --- | --- |
| feishu_personal.go | 单一加密配置 integration.feishu.personal_app；SaveApp 仅系统管理员；AES-GCM，主密钥必须 32 字节 | 应用配置测试只调用 tenant token，不证明用户文档权限 |
| OAuth | 32 字节随机 state，SHA-256 持久化，10 分钟过期；当前 JWT 用户、配置版本及一次性 claim 绑定；前端 /feishu/callback 通过受认证 POST 完成兑换 | 企业 A/B/C 授权、刷新、撤权本轮无证据，BLOCKED |
| Token/身份 | OAuth v2 token endpoint；user_info 验证 open_id；密文内含 KnowHub owner；UserToken 校验 caller/user/app/open_id；刷新事务行锁；每次请求重新验证 | 不能把上述实现等同真实账号验收；也未证明 OA 员工身份与飞书账号属于同一个自然人 |
| Disconnect | 删除本人连接和 state；仅本人 user_oauth imports 标记 needs_attention；正在兑换的 callback 不能重建已断连接 | 本地断连不等同飞书服务端撤销 token；需分别测试 |
| 单篇安全路径 | PersonalRecovery 重建调用者/成员/KB grant；绑定 config version、grant epoch、attempt、claim、lease；逐次远端读取重新验证；candidate/manifest/outbox 交接现有发布路径 | 新功能必须复用，不能重新实现发布和队列 |
| application_grant | 独立 task type feishu:application:process:v1、application ID、first-create 限制、环境开关和测试 KB 范围；Docx 与受控 Sheet/Bitable | 不更改现有上线策略；历史应用导入不代表个人 OAuth 已验收 |
| Wiki/Drive | 已有连接器、分页、流式发现、cursor、document_deferrals、限流和受控恢复 | 目前凭据来自 datasource 配置；没有统一个人 connection 引用 |
| UI/路由 | system/admin/feishu，me/feishu，knowledge-bases/:id/feishu；JWT、系统管理员与 KB write guards；目前单模式 UI | 尚无个人资源树、批次任务和共享发布审批 UI |

互斥发生在配置校验、Status、Authorize、Complete、Disconnect、UserToken、Import/prepare、authorizeLocal、mutate 和应用发布校验中，均比较全局 `cfg.AuthMode`。只替换管理员下拉框会导致后台拒绝另一身份，或漏掉提交时权限校验。

本轮还检查了 feishu_personal_first_create.go、repository/feishu_personal_recovery.go、core/personal_resource.go、core/user_identity.go、core/application_identity.go、drive/connector.go、wiki/connector.go，以及 FeishuImportDialog.vue、FeishuAppSettings.vue。URL 解析/Wiki 解包与部分读取可共享；用户和应用身份的验证、导出与完整性边界仍须保留。批次应作为既有 prepare/attempt 上的调度层。

原目录未跟踪的 docs/integration/feishu-personal/VALIDATION.md 明确记载真实 OAuth、真实刷新和 A/B 隔离待执行。它是历史本地报告，不属于本分支可复现的正式验收证据。本轮没有确认任何新的企业用户验收 PASS。

24 项存量缺口、NTOS1.0R5-SPEC、paused/hold/revoked、既有 cursor 和正式 Knowledge/Chunk/Embedding/BM25 均未操作。
