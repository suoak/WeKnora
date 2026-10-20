# 最终收口索引（2026-10-20）

最终运行源码 `1e2ded5b67ce9487966210d31011cf9c5b8cdfb9`；正式 CI 9 SUCCESS / 0 FAIL / 3 SKIP，普通员工 OA、三格式索引/检索、幂等、跨导入者明确错误、越权拒绝和退出已真实复验。完整结果、来源 SHA、限制及回滚证据见 [RC3_FINAL_CI_FIXES.md](RC3_FINAL_CI_FIXES.md)。下文保留此前版本的历史快照，不应将其旧 FAIL/PENDING 状态或旧镜像误当作最终结果。

生产仍 NO-GO；尚未合并 main。新的审核报告提交只改变文档，当前 PR 检查须另行核对。Bitable 复杂汇总截断、>500 样本、首次 JIT/首次邮箱关联和未签发 Refresh 的真实验收没有因此变为 PASS。

---

# RC3 实际源码与测试部署身份

代码提交：`5156e4b47643535d6ff473c9890be814bd6f7ffe`。分支：`release/knowhub-oa-feishu-rc3-20261020`；Draft PR [#28](https://github.com/suoak/WeKnora/pull/28)。代码父版本 `5f988be51779f1a22bd9d027b7503fe6a8749f62`；审查时 main `854f3b956ae3600e12a3ffd2fc40d4fe624ced74`。

| 项目 | 实际身份 |
|---|---|
| Git 源码清单（5685 文件） | `5be1a6ed2a3778e7caf34ed161755c0b71e2c7464c918d77175b05cc94a7f8b7` |
| APP | `sha256:20497a6098669044cf78426425361a67976ba3c6a3766dc175b7f5a5c5b11234` |
| Frontend | `sha256:dfb3c7ea4f250de171a8508771e806e3a1ed091062b38b3e469f9a508e3ade48` |
| APP 二进制 SHA256 | `c3bf72a3fe67259e8e96b5bf9b83c332aa360fab6a59799b5b1faf099ffcfbc2` |
| 测试部署时间 | 2026-10-20 11:07:09 +08:00 |
| 测试服务器 | 172.30.232.42 / aqyfzx10 |
| APP / UI 容器 | knowhub-mvp-p3-app-1 / knowhub-mvp-p3-frontend-1 |
| DB / Redis | 原 WeKnora-postgres / WeKnora-redis；未重建 |
| DB 版本 | 3033 clean；本次部署无迁移 |

镜像由实际提交归档构建，两者 OCI revision 均指向该代码提交。报告补充提交不包含运行源码变更，不能将报告 HEAD 误写成镜像编译 revision。

RC3 相对 RC2 的运行源码只有 legacy Feishu 停更屏障与邮箱前缀展示；另有必要测试/编译卫生修改，详见 `RC3_SOURCE_SCOPE.md` 和清单 `changed_from_rc2`。未引入 3032、P1-3/B1/E0、Go MCP 或新认证方案。OA 方案 B 验签/声明/身份绑定核心保持原候选。

提交前工作树 Manifest `bbdf9b...` 与最终 Git Manifest 的两处差异仅为两个 TS 文件行尾。所有 Go 字节一致；最终前端从 Git 归档重新 type-check/build。未跟踪的原生构建依赖单独锁定：`libanydoc_go.a`，30,778,878 字节，SHA256 `d5543b2cf68400c048992a192475aa70e895fda0abc495b85afb68dfd699122a`；其 Rust 源码/Cargo 锁文件与已测试版本一致。首次归档构建因忽略目录未带此库而失败，补齐真实依赖后构建成功，原失败记录保留。

测试配置保留 `SOURCEID_VALIDATION_STRATEGY=code_flow_tls_userinfo`、`SOURCEID_TLS_USERINFO_LIFECYCLE=continuous_test`、expires_at 空；新增 `FEISHU_LEGACY_SYNC_SUSPENDED=true`。该配置仅用于测试；生产不得复制 continuous_test 或测试实例范围。

回滚 APP `sha256:67b78b1c5aaf72c3d8e03763bfab57bdeb47bf3cf9d771e8fba60c870246d6cb`；Frontend `sha256:1d1e2c55996ea8be260a1103b22c9a33c06631f5570507076c4504f245e780db`。部署前 DB/文件/Redis 备份已做独立恢复验证；镜像归档与受限回滚脚本留在打包机 Git 外目录。候选当前保留，未执行回滚，也未声称回滚执行演练通过。

证据：`evidence/final-test-deployment.json`、`committed-source-manifest.json`、`native-build-dependency.json`、`backup-verified.json`、`image-backup-result.json`。部署文件中 real_browser_acceptance=PENDING 是部署当时状态，后续真实结果以 `RC3_REAL_ACCEPTANCE.md` 为准。
