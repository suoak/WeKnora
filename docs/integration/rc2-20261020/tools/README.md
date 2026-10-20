# RC2 独立迁移工具

这是发布候选工具，不是生产执行授权。它只加载 database/sql 与 golang-migrate，不启动 APP、Worker、模型或来源连接器。默认 check 仅读取数据库身份与迁移状态；仅显式 up 执行迁移。禁止 Force、Down、自动 dirty 恢复。

在候选仓库根目录的 Linux 构建环境执行：

```sh
CGO_ENABLED=0 go build -trimpath -o knowhub-migrate ./docs/integration/rc2-20261020/tools/migrate
sha256sum knowhub-migrate
```

实际隔离验证二进制 SHA256：`ae0e4d6ea26ceb88f4fe0e48ca38bb9a5e227ba8e62163734d63f58af2eb27f3`；工具源码 SHA256：`d0025cbf1fac9c0033019968feb7d97c4f7d4277bf333ca1a9c034bd70077145`。构建链、参数或 Go 版本变化可能改变二进制，必须重新记录，不能沿用摘要。

通过受控运行环境注入以下变量，不将 DSN/密码放入命令行、Git、终端输出或普通日志：

| 变量 | 值与检查 |
|---|---|
| KNOWHUB_MIGRATION_DSN | 受控 PostgreSQL DSN，不在本文提供真实连接信息 |
| KNOWHUB_MIGRATION_DATABASE | 确切目标数据库名；生产为小写 weknora，须经发布批准 |
| KNOWHUB_MIGRATION_DIR | 已核验原始 migrations/versioned 的容器/主机绝对路径 |
| KNOWHUB_MIGRATION_MODE | check（默认）或 up，其他值拒绝 |

按审批实例先执行只读检查，再升级，再检查；每步必须成功才继续：

```sh
KNOWHUB_MIGRATION_MODE=check ./knowhub-migrate
KNOWHUB_MIGRATION_MODE=up ./knowhub-migrate
KNOWHUB_MIGRATION_MODE=check ./knowhub-migrate
```

工具核对 3024–3031、3033 共九个版本，拒绝 3032/更高版本、错误数据库、异常迁移行数、dirty 或不支持的起始版本。仍须在运行前核对全部迁移文件 SHA；该工具不替代签名构建 Manifest。输出脱敏 JSON 阶段/版本/dirty/耗时，不输出 DSN 或原始 SQL 错误。失败后停止 APP 发布，保留失败库和受控日志；不手动改版本号。完整恢复预案见上级 `RC2_PRODUCTION_RUNBOOK.md`。

实测 9 PASS/0 FAIL/0 SKIP；其中完整数据升级由应用同版本 RunMigrations 另行完成，本工具的初次 up 使用生产 Schema 空数据副本。不能将两组验证混为工具已重跑完整业务数据首次升级。
