# KnowHub 第三批交付：M3 部分实现，整体 HOLD

本批源码和六份报告保存在原独立 worktree，保留 00～17 全部此前报告和原始证据；未 commit、push、merge、release、deploy。**不批准据此开放混合敏感 KB、历史 Portal 知识或生产公共授权。** 新组件默认关闭，生产 Python MCP、数据库、凭据、成员和飞书/OA/Portal 业务配置均未修改。未进入 M4/M6。

## 交付分类

| 分类 | 内容 |
| --- | --- |
| 已完成设计 | 文档 deny 优先、public/private/inherit/restricted/mcp_blocked 区分；可信主体和 Key 能力；SQL epoch、CAS/撤权/审计；候选前 allow-set 与未知驱动拒绝；来源依赖、broker/旧链接/CDN HOLD；飞书/GitLab ACL 接入要求 |
| 已实现源码 | DocumentReader / opaque grant；复用当前 KB/个人 Key 权限的 LiveDocumentBaseVerifier；3035 双库 migration；SQL DML/TRUNCATE epoch；主体 Grant CAS/原子审计；真实 SQLite 过滤及计划检查；模型/输出重验；可选隔离 HTTP 屏障；内存衍生 proof 和内容 hash broker；合成/真实网络测试夹具 |
| 已验证功能 | 仅 [22](22_M3_TEST_RESULTS.md) 与原始 exit_code 对应的组件、数据库和隔离夹具场景；不能扩展成完整应用验收 |
| 需要人工审批 / NEEDS_DECISION | 正式内容负责人/审核人角色、企业员工资格；新公共凭据目标 scope；用户级 deny 语义；生产旧链接/CDN/对象存储收口方案及任何生产执行 |
| 尚未完成或未测试 | 全部现有 REST/Go MCP/agent 路径接通；PG 和其他检索驱动认证；真实模型 HTTP spy；Wiki/FAQ/graph/历史消息持久化来源；文件 broker 真实存储适配；云缓存/匿名链接撤销；源 ACL 同步；性能与多客户端完整矩阵 |
| 明确 FAIL | 旧 Python + SDK 的 SSE 撤 Key 后等待超时；当前分支正式迁移从生产 3033 升级缺少历史 source。后端拒绝/组合迁移通过均不能覆盖这两个 FAIL |

## 报告与代码证据

- [18 文档权限与内容版本](18_M3_ACCESS_IMPLEMENTATION.md)
- [19 检索与模型边界](19_M3_RETRIEVER_SECURITY_VERIFICATION.md)
- [20 资源、缓存、衍生和来源 ACL](20_M3_RESOURCE_LINK_AND_DERIVED_ACL.md)
- [21 旧 Python/REST 验证及绕过清单](21_M3_PYTHON_REST_COMPATIBILITY.md)
- [22 测试、退出码和完整门禁状态](22_M3_TEST_RESULTS.md)
- [候选 / 逐文件 SHA256 / 新增修改清单](evidence/batch3/source-manifest.json)
- [运行索引与原始测试目录](evidence/batch3/delivery-index.json)
- [本批报告、证据、测试工具逐文件摘要](evidence/batch3/artifact-sha256.json)

最终覆盖文件候选 `531b9be978c07aed89814edf4d1951dc74d8b7e8d3d2786806449edd46cb85c0`；运行时指纹 `b071aec3ede5b70c033ae694ff4eab27369a4f942b15208f346db2a21b67dc49`。完整套件候选及最后仅测试夹具差异见 22。新增源码/迁移/测试工具 23 份，修改 9 份；另列复用 M2 13 份和未改模块输入 2 份，逐文件分类与 SHA256 见 manifest。Git HEAD 仅表示旧基线，不能用来替代本批指纹。源码与测试在任务快照中固定，后续修正使用新任务，不覆盖早期 FAIL；最终文件另有 artifact hash。完整命令保存在 results.json，测试二进制只保存 SHA256，不提交体积庞大的可执行文件。

## 下一批 M3 开发顺序

1. 先解决实际生产分支/迁移来源对齐：保留直接 3033 升级失败断言，不能用空迁移、Force 版本或测试组合目录代替正式来源。然后完成完整后端内的可信适配与精确响应 contract。沿 knowledge detail/list/batch/search、Chunk detail/list/revision/neighbor 逐个接入，分页计数/标题/标签/summary 同样受控。屏障未认证入口继续拒绝；角色/企业资格未映射继续 nil/default-deny，不由管理员身份代替。
2. 将实际 Hybrid/Rerank/chat/agent/grep 的候选、补全和模型网络调用接通相同授权上下文；先 PG/ParadeDB 的真实 allow-set/计划/超限验证，再逐个其他驱动。没有驱动证明就阻断，不允许结果后过滤替代。
3. 为 Wiki/FAQ/graph/历史引用/缓存建立不可缺省的持久化来源版本关系、生成输入证明、删除与撤权事务边界；未知历史来源不自动回填开放。保持敏感混合 KB HOLD。
4. 接现有文件权限及可信内容 hash/版本，完成 broker HTTP/存储适配；单独证明旧 HMAC URL、短链、对象直连、CDN stale/range/cache 都可收口。当前没有证明，禁止开放相关资源。
5. 建立飞书/GitLab 稳定来源身份、实际授权 feed/重新验证、失联 HOLD、权限变更先拒绝后索引清理；重导入不继承审批。不要改无关集成功能。
6. 单独修复/验证旧 SSE 撤 Key 后未结束请求的问题，同时保留原模块基线回归；不能通过让 auth/me 对 revoked Key 返回成功来规避问题。生产模块仍保持原状。
7. 在完整隔离后端正常 Auth/业务 Handler 上复跑 D/C/P/S 全矩阵、真实 HTTP 模型 spy、缓存/重试/并发/多 KB、性能和客户端回归。所有门禁有真实证据后再讨论下一阶段；当前不自动进入 M4。

## 回滚与生产影响

生产影响：没有生产连接、迁移、发布或业务写入。所有数据库写入只在合成测试库；容器 network none，临时 PG 销毁不会触及生产。没有公开历史知识、自动补公共审批、轮换 Key、批量改成员或向外部模型发送正文。

回滚源码按 manifest 逐文件 review，不执行 blanket reset/clean，避免损失前两批未提交工作。运行时保持新 DI provider 不注册、无路由认证、无企业资格/角色适配。隔离数据库先停 Worker/请求，再备份审计与策略，down 3035；业务文档保留，授权元数据消失。再次 up 生成新 incarnation，需要重新审核；不能承诺旧 Grant 无条件恢复。现有政策/主体 FK 会约束硬删除，完整业务删除/保留流程还须核对，不能直接部署。

候选基线与生产 3033 源存在此前已记录的版本差异。PG 测试使用冻结生产迁移源只在空测试库重放；不表示可以直接用当前旧基线应用替换生产。任何后续发布前必须完成与实际 3033 分支的源/迁移兼容评审。本批停止在所有提交与生产操作之前。
