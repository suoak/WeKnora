# 第四批阶段交付：P0 收口，M3 继续 HOLD

本批采用用户授权的 P0-A/P0-B 阶段停止点。两个原始 FAIL 均保留；新 RC3 候选的正式迁移和真实 SSE SDK 撤销回归分别通过。**M3 整体未完成，不批准公共知识开放或生产发布。**

## 交付导航

- [24 RC3 对齐、完整快照及逐文件移植](24_RC3_BASELINE_TRANSPLANT.md)
- [25 正式 PostgreSQL / SQLite 迁移验收](25_FORMAL_MIGRATION_ACCEPTANCE.md)
- [26 SSE 根因、修复、四项安全结论及 SDK 证据](26_SSE_KEY_REVOCATION_FIX.md)
- [27 实际核心服务缺口与开发顺序](27_M3_CORE_SERVICE_INTEGRATION_STATUS.md)
- [28 检索驱动与模型输入验证边界](28_RETRIEVER_MODEL_VERIFICATION.md)
- [29 REST/Python/Go/Agent/文件/Wiki 覆盖与剩余 HOLD](29_READ_SURFACE_COVERAGE_MATRIX.md)
- [30 测试命令、环境、真实退出码及保留 FAIL](30_FOURTH_BATCH_TEST_RESULTS.md)
- [32 用户授权的打包服务器清理](32_SERVER_BUILD_ARTIFACT_CLEANUP.md)
- [交付索引](evidence/batch4/delivery-index.json)、[源码逐文件 SHA256](evidence/batch4/source-manifest.json)、[交付文件哈希](evidence/batch4/artifact-sha256.json)

## 状态分层

| 类别 | 结论 |
|---|---|
| 已完成设计/核对 | RC3 正式链、SSE GET/POST 与 SDK dispatcher 根因、最小鉴权错误/关闭方案、核心业务剩余接入顺序 |
| 已实现源码 | RC3 语义移植、3034/3035 复用、正式启动器回归与故障恢复测试、Python SSE 错误关联和清理、跨主体/工作区负向测试及真实协议观察器 |
| 已验证功能 | PG/SQLite 正式迁移与旧数据保持；Go/race/vet；Linux Python 55 项；完整 service/agent；SDK 2.2/2.3 真实 HTTP/SSE；有限合成 ACL/SQLite 检索 |
| 需要业务决策 | 审核角色映射、企业员工资格、用户级 deny 语义；维持拒绝默认，无生产 verifier |
| 尚未完成/测试 | 实际 Knowledge/Chunk 全读取面、PG/ParadeDB 候选前文档 ACL、RAG/Agent 模型网络/输出、真实文件/CDN/旧链接、持久化衍生与来源 ACL；M3 HOLD |
| 仍失败 | Windows 完整 Python 套件 3 errors；历史两个 FAIL 与本批环境失败原记录不改写 |

## 可复核候选

新工作树 `E:/code/WeKnora-worktrees/knowhub-mcp-b4-rc3-20261021`，Git 基线 `e62b17e2d305358467821aeff1cce63ce1d61254`。冻结测试候选为 `04bed643e30d6a7e9bf6bd071e919722dfbe7e0eb6b73784f07fcd25eeafd3e4`，运行源码指纹为 `f2d281a90a70d8f417bc2b666bdbdc17f94ee99f528a6d853c2e06b99f40708e`。当前源码与冻结运行集合逐文件一致；新增编排/报告独立记录，不用 Git HEAD 冒充新代码版本。

原工作树 6,704 个文件复核未变；完整归档、tracked patch、untracked 文件、此前 00～23 报告和原始证据保留。可从完整快照恢复旧候选；新分支未提交，源码和文档仍可审阅。

## 下一阶段验收建议

接受 P0 在明确隔离范围内关闭；不要据此开放混合敏感 KB。下一批从 Knowledge/Chunk 服务和直接读取开始，之后接生产所用 PG/ParadeDB 候选前过滤，再接模型网络/响应复验，最后衍生 lineage 与文件/来源安全。每个实际入口都要有正反测试和撤权并发证据，细节见 27～29。

Windows 文件路径测试先纠正 cwd 清理次序，并在具备真实 symlink 能力的受控环境验证；没有能力时继续 FAIL/BLOCKED，不跳过授权断言。代理多 worker 下 SSE 必须有正确会话路由与再次实测，不能继承单 worker PASS。

所有生产能力继续默认关闭；生产 Python、数据库、Key、成员、Portal 历史状态没有改变。不进入 M4/M6，不删除旧 Python。未执行 commit、push、merge、release、deploy 或生产迁移。
