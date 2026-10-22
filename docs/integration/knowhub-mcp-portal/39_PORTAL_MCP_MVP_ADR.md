# 第六批 ADR：Portal 企业公共知识 MVP

日期：2026-10-21（Asia/Shanghai）。本批业务优先级替代此前“完整 M3 后才能开发个人 Go MCP”的顺序，**不替代默认关闭、敏感隔离和历史首次确认**。

## 决策

正常业务发布入口使用现有 Portal。公共范围为：`有效 Tenant + Portal published → 整体安全、已确认普通 KB → 当前版本合格文档`；再与员工及个人 Key 独立允许的私有 KB 取并集。客户端 KB IDs 只能对这一集合取子集。公共分支不给员工建立目标成员资格，不产生管理、写入或原始文件下载权。

Portal draft / archived / tenant disabled 或 deleted、KB deleted / temporary、文档停用、未知来源、缺安全确认、显式拒绝或版本过期均不进入公共分支。RC3 KB 没有独立 `disabled` 状态列；本批不臆造字段，以真实 KB 有效性和文档 enable 状态拒绝，后续若新增 KB 开关须纳入该门禁。

混合敏感 KB 在候选检索前**整库排除公共范围**。独立私人范围复用现有 Key scope、成员/share 基础和文档 Grant；为避免未经完成的混合隔离，本 MVP 的个人入口只接受调用者整库均有有效读取/搜索授权的 KB。部分文档授权、未完成分类的旧私人 KB 在这个新入口 HOLD；其原有默认关闭的 Web/Python/团队入口没有因此被改写。不能宣称个人入口已经覆盖所有旧私人知识。

## 对 M2 的兼容调整

保留 enterprise_retrieve、enterprise_public Grant、三个策略表、revision/CAS、事务审计、资源 epoch 和默认关闭。`PortalMCPResolver` 自动发现当前合格 KB，不需要为 KB 创建团队 MCP 或为员工手动指定全部 KB；Portal 发布状态每次读取，在请求和模型/输出边界复验。

兼容期空间策略的 approved/ordinary 字段继续作为“允许扫描该空间中的安全 KB”的功能准入检查，不承诺整个空间所有 KB 都普通。合成空间同时包含普通公共 KB 和敏感混合 KB，只有合格 KB 入选；不能因为空间准入通过而忽略 KB/文档分类。

策略角色调整为首次资源安全确认、功能灰度/启用及风险撤销，不再把每次 Portal published 当成另一套 MCP 上线审批。初次确认仍由现有 `EnterprisePolicyAuthority` 约束，并保留独立审核人与操作者；Portal 状态变化不自动写策略、回填历史审批或抬升角色。

已批准普通 KB 随 Portal 发布自动进入新请求范围，撤回即移出公共分支；独立私人授权仍可保留。内容/文档集合变更造成安全快照过期时保持 HOLD，不能把 `inherit_new_ordinary` 或缺记录当成公开证据。未来“新增普通内容免逐项人工复核”依赖可信的写入分类/来源 Provider 及版本覆盖，本批没有实现或启用自动分类审批。首次分类的批量 UI/事务编排可复用同一 PolicyAuthority/CAS，不能直接 UPDATE 成 approved。

## 三类权限

| 权限 | 当前职责 | 正式绑定 |
| --- | --- | --- |
| Portal 业务发布 | 现有 Portal 管理权限与审计，空间进入企业目录 | 保留现状，不改权限 |
| 企业 MCP 功能启用 | 企业范围、灰度及员工 Provider、凭据能力准入 | NEEDS_DECISION；容器显式 disabled，不提供生产 enable 环境变量 |
| 内容敏感审核 | KB/文档分类、来源可信度、历史首次确认、撤权 | NEEDS_DECISION；复用 PolicyAuthority，隔离测试 owner/reviewer |

普通 Space Owner 不自动获得跨空间启用/审核权。SystemAdmin 也不自动成为本方案的审核人。员工身份必须来自可信 KnowHub 用户并通过企业资格 Provider；缺正式 Provider 时仅合成测试可用。旧 Key 不自动补 enterprise_retrieve，普通 tenant/platform/mcp_ 机器主体不能进入员工入口。

## 验收分轨

整体安全 KB 的 MVP 按真实 PostgreSQL 检索、个人身份、Portal 动态状态、模型输入、直接读取和资源输出单独验收。混合敏感 KB 的单文档预过滤、Wiki/FAQ/graph 来源、旧缓存/引用、文件/CDN、外部来源细粒度 ACL 继续 **M3 HOLD**。本批 MVP 的任何 PASS 均不代表生产已获准启用。
