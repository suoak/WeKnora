# 第四批：RC3 基线对齐与安全移植

本批按用户允许的 P0 阶段交付，M3 整体仍为 HOLD。没有执行生产连接、写入、迁移、发布、凭据轮换或 Git 提交。

## 基线与保全

- 原工作树：`E:/code/WeKnora-worktrees/knowhub-mcp-portal-20261020`，分支 `audit/knowhub-mcp-portal-20261020`，基线 `be308d0058db4e647cd5103ae27b39da3a20eace`。
- 新候选：`E:/code/WeKnora-worktrees/knowhub-mcp-b4-rc3-20261021`，分支 `audit/knowhub-mcp-b4-rc3-20261021`，基线 `e62b17e2d305358467821aeff1cce63ce1d61254`。
- 完整快照：`E:/AIData/Codex/knowhub-b4-snapshot-20261021-062811/complete-worktree.tar`，包含 6,704 个实际文件、134,858,157 字节。Git 工作树指针不作为可恢复源码复制；HEAD、状态、tracked binary patch、逐文件哈希另行保存。
- 归档 SHA256：`51918a42764a1e267bcae8675803e83157c2fd9c7eb1bb46560818f9e7b229eb`。已从归档逐文件读取并验证哈希。
- 之前 00～23 报告和文档目录内 1,198 个文件全部按字节保留，包括被 Git ignore 的原始日志。原第三批两个 FAIL 不覆盖、不改写。

证据：[快照校验](evidence/batch4/old-worktree-snapshot-validation.json)、[原文件哈希](evidence/batch4/old-worktree-sha256.json)、[保全复核](evidence/batch4/old-worktree-preservation-check.json)、[移植记录](evidence/batch4/transplant.json)、[历史证据清单](evidence/batch4/preserved-prior-evidence.json)。

## 合并方式

原 tracked 文件只向 RC3 应用上下文校验通过的差异补丁，不以旧文件覆盖生产新版本。新增文件仅在 RC3 不存在同名文件时复制；没有碰撞覆盖。两个冲突显式处理：

| 文件 | 最终处理 |
|---|---|
| `internal/database/migration_sqlite_versioned_schema_test.go` | 保留 RC3 飞书个人恢复、应用身份全部列和表断言，新增企业策略及文档授权表，预期最新版本 3035 |
| `migrations/migration-map.json` | 保留 RC3 历史、来源 checksum 与逻辑映射，仅更新两种方言 latest=3035、增加 3034/3035 逻辑项 |

移植 JSON 的 CONFLICT 是首次补丁尝试结果，最终解决如上，并由新候选数据库测试验证。SID/OA、飞书和 Portal 的 RC3 源码没有被旧版本整体覆盖；最终变更清单见 source manifest。

## 正式迁移来源

RC3 的四份 003033 原文件均保留。所有既有版本 ≤3033 的 SQL 均与 RC3 Git 内容一致；记录 raw SHA256 和仅规范化 CRLF/LF 的 SHA256，未将行尾差异当业务修改。没有新增 3032、重编号、空 3033 或临时拼接迁移来源。

证据：[RC3 迁移逐文件校验](evidence/batch4/rc3-migration-checksums.json)。正式运行时仍按文件数字排序，manifest 用于校验与来源记录，不改变启动迁移器。

## 回滚与影响

旧工作树和完整快照均可直接用于复核前三批；新候选保持未提交。生产无影响。数据库仅验证隔离合成库的 3035→3033→3035；未授权低于 RC3 的历史 down，不把隔离 down 当作生产回滚批准。进入后续 M3 开发时使用本新候选，避免重新引入旧基线缺口。
