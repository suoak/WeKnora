# 企业统一模型配置：第一阶段核查与实现

核查日期：2026-11-19（Asia/Shanghai）。通过 `git fetch origin main` 重新确认基线为 `5aa926d022a01a71ba54f8cede5ae110660b0fbe`（PR #208）。本项在独立 worktree `E:/code/WeKnora-enterprise-models`、分支 `feat/enterprise-model-defaults-20261119` 开发；原工作区的 OA、飞书、Confluence 和 MCP 未提交改动未纳入。

## 精准差异核查

| 范围 | main 已有能力 | 本次处理 |
| --- | --- | --- |
| 管理员模型目录 | 分层目录、校验、历史版本；builtin 由系统管理员维护；凭据独立端点及脱敏 DTO | 复用，不另建模型系统 |
| 企业默认模型 | 六类 model.default.* 系统设置、管理员策略 API、成员有效默认 API；新 KB 和会话部分继承 | 补空间覆盖与后台辅助模型统一选择 |
| 上游 #4822 | 空间 default_settings、能力校验、目的元数据、后台默认选择 | 移植 helper 和测试，扩展现有企业策略 |
| XLSX / #4423 | 合并区域、样式兼容修复、只读行读取、大型工作簿优化 | 第二项单独开发，不改解析、分块或 checkpoint |
| MCP / #4463 | Go MCP 范围/文档权限检查，Personal MCP/Portal 统一检索、并发、统计、#208 保真修复 | 第三项单独增加搜索 knowledge_ids；不改 Key 和入口 |
| Migration | PostgreSQL/SQLite 最新 003048，历史冻结及清单治理 | 追加 003049，不覆盖历史迁移 |

XLSX 必须保留的优化提交：`6632bc950`、`4f014c661`、`68ef4c8f4`、`339d46d60`、`5dbcb48a0`。当前预处理仅在有合并区域时进入 openpyxl，不可用上游百分比实现覆盖现有优化。真实 XLSX 的显示语义、行关联、准确率、内存和耗时对比留在第二项。

核查时相关未合并 PR：Wiki #127/#129/#130；飞书 #61/#62/#67/#83/#106；MCP 文档 #140。它们不当作 main 已有能力，不移入本 PR。

## 移植与扩展

- DefaultSettings 和 helper 测试移植自上游 #4822 head `cc59e94ed1be1e9bb537583dbdabf350e46a5821`，合并提交 `dacb9c2643bdfe7b3f95e0d96e1584513b27f3a2`。企业保留 summary 命名，空间辅助槽沿用 utility。
- 企业继续使用现有系统设置；空间仅存模型 ID，空值动态继承企业，新空间不复制凭据。覆盖只能选择现有授权范围内、活动且能力相符的模型。
- default_for 只投影到列表，不持久化，不改仓储返回对象；企业投影复用已加载列表。视觉支持原生 VLLM 和具有视觉能力的 KnowledgeQA。
- 新 KB 创建时采用有效默认并固定 Embedding ID。已有显式资源配置保留。Wiki 的 summary/synthesis 优先链及源授权、发布、恢复保护不变。新智能体预选模型，不自动开启上传；运行时空配置继续走现有策略。
- 标题、MCP 使用说明、记忆提取共用 PickUtilityModel。后台只有执行 tenant ID 时也能读取空间默认，不接受其他 tenant 的缓存信息。
- /tenants/kv/default-settings 复用 Viewer 读、Admin 写和现有 API Key 能力门禁；服务层额外绑定原始 Caller 与执行空间。
- 保存携带 settings/previous，陈旧快照返回 409。默认列修改和包含新旧模型 ID 的审计原子提交；审计失败回滚。通用空间更新忽略默认列，避免其他设置页旧对象覆盖新配置。
- 历史失效默认保留并告警，允许逐项修复；加载未完成/失败禁止保存，切换空间忽略旧响应。显式无效资源选择仍由原校验报错。
- Embedding 编辑 API 禁止通过已有 ID 原地改变维度和生成身份；同维度也不保证向量空间兼容。允许显示名和凭据轮换。已有 KB 不能直接改绑其他 Embedding ID，重建须另获批准。

## 数据库影响与回滚

003049 仅新增可空 tenants.default_settings（PostgreSQL JSONB、SQLite TEXT），无历史回填、向量写入、索引重建或飞书同步。PostgreSQL ALTER TABLE 仍有短时元数据锁风险。生产迁移必须先取得明确批准。

应用回滚优先保留新增列，旧应用不会读取。配置回滚：从 model.workspace_defaults_changed 审计取 old_value，以当前 GET 为 previous，通过同一 PUT 校验并恢复，追加审计。企业策略通过现有 system.setting_changed 旧值和模型策略 PUT 恢复。

schema down 前先停新版本写入，导出空间模型 ID/审计，确认无应用依赖后执行。down 会删除空间覆盖，不能代替日常配置回滚。up/down/up 仅在隔离库验证，未对生产执行。

## 验证与交付

PR：<https://github.com/suoak/WeKnora/pull/209>，验收前保持 Draft。

已通过前端类型检查、90 项相关测试和上游模型选择 helper 的 Go 测试。Windows 大服务包有原生链接依赖限制，后端按用户要求转到打包服务器。

打包服务器使用独立 SATA 目录和无外网的临时 PostgreSQL/构建容器，不改已有服务。首轮六个后端包定向测试及新增 PostgreSQL/SQLite up/down/up 已通过；原迁移夹具固定 3048 的问题已修复，正在验证新提交。新增 Enterprise Model Defaults CI 显式提供一次性 PostgreSQL，避免事务测试缺 DSN 而跳过。

最终 CI、HTTP/数据库验收和构建结果待更新。未合入 main，未发布生产，未执行生产迁移、向量重建或批量同步。第一项验收前不开始第二/三项编码。
