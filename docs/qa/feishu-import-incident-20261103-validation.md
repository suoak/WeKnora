# 本次补丁验证

候选基线：实际生产 APP b874d5437302914b751b7a6d991162daa5bb1767。
候选目录：E:/code/WeKnora-worktrees/feishu-import-incident-20261103。
未 commit / push / merge / 构建生产镜像 / 部署；未改生产业务数据。

| 检查 | 结果 | 边界 |
| --- | --- | --- |
| git diff --check | PASS | 包含最终补丁 |
| vue-tsc --build frontend/tsconfig.json | PASS | 最终前端源码；复用现有 node_modules，未改 lockfile |
| feishuBatchKey、feishuOAuthFlow、feishuQuickImport 现有前端测试 | 6 PASS / 0 FAIL / 0 SKIP | 本地测试，不是生产真实 OAuth |
| TestPersonalAuthModeConflictRollsBackWithoutMutation | PASS | SQL mock 要求只读碰撞检测后 ROLLBACK，禁止任何写入；不是生产数据库运行 |
| 服务层分类/action 及现有失败详情测试 | BLOCKED，未执行到测试 | Windows DuckDB 链接 cannot find -lrstrtmgr；不能记为测试 PASS |
| 正式 Linux/native 构建、真实 PostgreSQL 交接与并发回归 | NOT RUN | 不能部署为已验证的生产恢复版本 |
| 真实失败文档导入、Chunk/Embedding、索引发布、普通用户检索 | NOT ACHIEVED | 生产身份约束没有被修改或绕过 |

Windows 默认 go.exe 是 1.24.5；项目要求 1.26.0。使用已有的 1.26.10 工具链运行。
仓库测试初次链接失败于 pg_query_go 的 __stack_chk_fail，补充本地链接参数后通过：

```
go test -ldflags=-extldflags=-lssp ./internal/application/repository -run '^TestPersonalAuthModeConflictRollsBackWithoutMutation$' -count=1
```

该参数没有写入项目或生产环境。服务层另因现有 MinGW 缺少 rstrtmgr 库无法完成链接。
新增服务测试已保留供兼容的构建环境继续验证；本轮没有修改项目依赖或编译配置来掩盖环境问题。

恢复前仍需实现并验证受控身份交接，完成单篇真实失败文档的完整生产验收，然后再扩大到相同原因的其它资源。
