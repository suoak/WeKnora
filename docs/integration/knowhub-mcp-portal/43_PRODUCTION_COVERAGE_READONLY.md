# 第六批生产 Portal 脱敏覆盖统计

审计时间：**2026-10-21 09:13:58（Asia/Shanghai）**。实际只读 SSH 使用固定主机公钥；SQL `BEGIN READ ONLY`、15 秒 statement_timeout。未修改生产数据库、Python MCP、Key、成员、Portal 或 schema；未读取正文、标题、URL、源路径或个人身份明细。

原始证据：[production-coverage-readonly.json](evidence/batch6/production-coverage-readonly.json)。可复核脚本：[batch6_production_coverage.py](tools/batch6_production_coverage.py)。

另于 **2026-10-21 09:32:25（Asia/Shanghai）** 补充只读 KB 规模聚合，保留首份证据、不覆盖；见 [规模审计](evidence/batch6/production-coverage-shape-readonly.json)。空间/KB/知识总数未变。

| 实际生产聚合 | 数量 |
| --- | ---: |
| published 且 active、未删除的空间 | 17 |
| 其下未删除 KB | 48 |
| 临时 KB | 0 |
| 其下未删除知识记录 | 2718 |
| 当前正式迁移版本 | 3033，dirty=false |
| 本方案分类/版本四表 | 均未存在 |
| 非 document 类型 KB | 0 |
| 超过本 MVP 256 文档 allow-set 上限的 KB | 4 |
| 单 KB 最大知识记录数 | 864 |
| 有可核验版本化安全分类依据、可进入本次公共 MVP 的空间 / KB | 0 / 0 |
| 仍需首次分类确认或维持受限的 KB | 48 |

“0”表示缺少本方案要求的可核验安全确认，**不能据此推断 48 个 KB 都敏感，也不能推断它们安全**。没有读取生产正文，不能给出敏感/普通 KB 的臆测比例。未把 Portal published、现有账号能读或生产 API Key 权限作为资源分类证据。

当前可核验公共覆盖率为 0/48；没有实际生产公共授权。本地/隔离环境的合成 PASS 不能反向登记生产资源为安全。

## 覆盖率主要阻碍与整改

1. 历史资源首次安全分类/来源确认未完成，现有生产仍在 3033；本批不执行迁移或自动回填。
2. 需要明确内容负责人、正式审核/启用角色及可信企业员工资格 Provider。不能用普通 Space Owner/SystemAdmin 名称自动赋予新职责。
3. 将确认安全且整体可共享的 KB 与混合/未知来源 KB 可靠隔离；对后者保留私人权限，公共分支 HOLD。
4. 确认全部写入和重同步的分类/epoch 维护、文本中历史匿名链接、附件/图片/对象直链和缓存撤权风险。
5. 先验证实际生产版本/模型/索引配置和灰度策略，再提出受审的迁移/启用方案；本批没有上线许可。
6. 至少 4 个 KB 还需处理当前 256 文档上限：在可信整体分类和真实 PostgreSQL 预过滤前提下实现更大范围的受控过滤/分页与性能验证，或可靠拆分 KB。不能仅修改常量或去掉 allow-set 让它们进入公共范围。未检查这些 KB 的正文，不能判断其中普通/敏感比例。

达到完整 Portal 公共覆盖并非只部署个人入口：需要逐 KB 可追溯安全分类、持续版本治理、外部来源 ACL 与资源访问闭环。未达门槛的 KB 不以“100% 覆盖”为由默认放行。
