# M1 部署、监控与安全回滚

状态：方案 NEEDS_DECISION，部署 NOT_TESTED。本批只有只读审计，不执行生产迁移、部署、重启、密钥变更、批量授权或历史发布。

## 环境和版本门禁

生产当前 app/frontend 为 e62b17e2d，MCP 854f3b956，DB 3033/clean；不能按审计 HEAD 或 GitHub main 覆盖。下次准备版本时重新采集运行镜像/DB 版本，识别 SID/OA、Portal、飞书、个人 Key、成员及现有 RAG 定制差异，明确 release base。新迁移编号与部署时 schema 校验，拒绝跳过未知迁移。

隔离环境使用单独 DB、Redis namespace/队列、object storage、模型配置、SSH/客户端测试凭据；不得复用生产密钥或触发生产同步。固定镜像 digest，数据库与文件备份在隔离环境验证可还原；不要执行已有 release_common、pilot-support 等含写操作脚本来做“只读检查”。

## 发布前清单

| 门禁 | 证据 | 当前 |
| --- | --- | --- |
| G0 事实基线 | 00/01/02 版本、范围、模式 | PASS（只读事实）；内容审批仍未知 |
| G1 发布策略与企业资格 | 签署模型、主体来源、审批角色与迁移设计 | NEEDS_DECISION |
| G2 试点资源 | 精确 KB/doc 审批快照；混合 KB HOLD | NOT_TESTED |
| G3 文档安全闭环 | REST/Python/Go/文件/匿名链路/衍生资源 + 外部模型输入 canary | NOT_TESTED |
| G4 Go 与客户端 | 八工具白名单、跨空间/跨模型、并发、三客户端 | NOT_TESTED |
| G5 性能运维 | P50/P95/recall/error、限流/观测/备份与回滚演练 | NOT_TESTED |
| G6 生产变更 | 单独审批、具体 SHA/digest/迁移/灰度范围 | NEEDS_DECISION |

## 可观测性

以 user/key/tool、resource counts、public/personal grant counts、policy revision、denial reason、partial/error、timeout、fan-out、P50/P95 记录。metric 不用文档 ID/用户 ID 等高基数标签；ID 只进受控审计。禁止 prompt、正文、查询原文、token、client_secret、完整源 URL。对授权解析失败、策略版本不一致、异常 allow-all、scope 激增、来源失权传播延迟、匿名受限资源访问报警。

灰度告警阈值应基于隔离负载基线与业务风险评审设置，不虚构性能数字。发现越权 canary 或外部模型收到无权候选立即停止公共入口，并保留证据；不要先扩大权限解决“搜不到”。

## 回滚顺序

1. 关闭个人公共入口/新前端连接入口，停止新增公开审核与新策略放宽。保持受限策略生效。
2. 保留或回切已验证的应用安全读取版本，确保旧 Python REST 同样执行 ACL。不能退回无文档守卫的旧 app 来恢复可用性。
3. 企业 public grants 暂停，不更改用户原 private TenantScopes、成员资格或 team endpoints。旧 Python 使用原透传配置继续提供已验证安全范围；若不满足 ACL，需经批准受控隔离受限资源/入口。
4. 撤销新生成的可撤销链接，完成 CDN/cache 失效并核对对象存储直链风险；清缓存不能替代同步授权拒绝。
5. 已创建的策略表和审核数据保留，不在事故中执行 destructive down。尚未激活的开发环境才做 down/up 演练；生产恢复数据库必须有独立方案、备份核对和批准。
6. 对候选版本、数据变更与文件签名做恢复验证，重复 A/C/D/F 拒绝矩阵，确认 Portal/成员/Key/SID/Feishu 兼容。

生产 Python 退役必须在客户端/机器依赖清零、观测窗口、回收计划及有效回滚全部验收后另行批准。本批未进入上述流程。审计文档本身回滚只需丢弃本 worktree 新增文件，无运行影响。
