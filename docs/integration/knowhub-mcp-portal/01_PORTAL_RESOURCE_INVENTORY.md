# M0 Portal 资源清单

状态：统计 PASS；敏感性与开放审批 NOT_TESTED / NEEDS_DECISION。数据来自 [只读生产快照](evidence/production-readonly-20261020.json)，采集时点见 observed_utc。以下全部是聚合或必要空间 ID，没有标题、正文、员工信息或来源 URL。

| 项目 | 数量 | 口径 |
| --- | ---: | --- |
| Portal published / 有效空间 | 17 | pc.status=published，tenant active 且未删除 |
| Portal draft / archived / 非有效空间配置 | 0 / 0 / 0 | all_portal_statuses；不代表平台不存在未配置 Portal 的空间 |
| 有效非临时 KB | 48 | 全为 document 类型；不代表全部支持同一种索引 |
| 已发布空间内临时 KB | 0 | 过滤 KB 软删除前提下统计 |
| 有效 Knowledge 记录 | 2718 | document KB 下包括 file、manual、url；不等于均为非敏感普通文档 |
| completed / finalizing / failed | 2711 / 2 / 5 | pending、processing 本快照为 0 |
| 至少一条 embedding 的文档 | 2716 | EXISTS embeddings；仅索引存在性，不证明完整性、可召回或 ACL 正确 |
| file / manual / url | 2699 / 3 / 16 | 由 published_documents 汇总 |
| 全库 Wiki 页 / 有效 chunks | 71668 / 741169 | 全数据库范围，非上述 17 空间专属数量；未读取其内容 |

注意：failed 文档仍可能有历史 embedding；2716 不是“索引成功验收数”。finalizing 可已有可检索主索引。不得把 parse_status 当作发布审批。

| TenantID | 有效 KB | Knowledge | 有 embedding |
| ---: | ---: | ---: | ---: |
| 10002 | 2 | 85 | 85 |
| 10003 | 8 | 207 | 207 |
| 10005 | 5 | 164 | 164 |
| 10006 | 5 | 123 | 123 |
| 10007 | 1 | 10 | 10 |
| 10008 | 2 | 4 | 3 |
| 10009 | 1 | 864 | 864 |
| 10010 | 1 | 9 | 9 |
| 10011 | 6 | 31 | 31 |
| 10012 | 1 | 339 | 339 |
| 10013 | 3 | 0 | 0 |
| 10014 | 2 | 299 | 299 |
| 10015 | 1 | 2 | 2 |
| 10016 | 3 | 10 | 9 |
| 10017 | 4 | 15 | 15 |
| 10018 | 1 | 290 | 290 |
| 10019 | 2 | 266 | 266 |

来源 channel 汇总：feishu 680、feishu_drive 231、feishu_personal 3、gitlab 1055、web 736、api 13。外部来源至少 1969 条（914 飞书系 + 1055 GitLab）；web/api 不保证源权限已知。channel 仅用于盘点，不能作为授权。

## 审批分类

| 分类 | 当前结论 | 下一步 |
| --- | --- | --- |
| 已批准企业开放 | NOT_TESTED，未取得审批证据，数量未知 | 资源负责人确认确切 KB/文档和批准快照 |
| 需受限 | NOT_TESTED，不能仅由文件名/Tag 判定，数量未知 | 在受控管理端审核，混合 KB 标 HOLD |
| 未知、待审核 | 本批保守将全部 48 KB / 2718 文档作为开放候选待核查 | 这是本报告分类，不写生产状态，不撤销现有合法私有访问 |
| 禁止 MCP 发布 | NOT_TESTED，数量未知 | 明确策略记录与责任人，覆盖衍生内容 |

无法从聚合统计得出“可疑敏感资料”的精确数量，不能填写 0。来源 ACL 未知文件暂不自动纳入公开试点。未选定任何生产试点，未迁移/拆分知识。后续审核需保留 source resource ID、导入主体、版本、索引、原文件、引用与同步关系，但本批不导出它们。

复用 repository/portal.go:resourceCountMaps 的软删除/临时过滤；新增盘点明确区分 completed 与 embedding 实际存在性。现有 Portal 聚合只统计 file/file_url 指定状态，不能与本报告所有 Knowledge 口径混用。回滚及生产影响：无写入，无需恢复。
