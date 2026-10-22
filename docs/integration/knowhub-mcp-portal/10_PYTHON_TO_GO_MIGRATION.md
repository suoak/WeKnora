# M1 迁移准备方案（M6 尚未执行）

状态：NEEDS_DECISION；生产 Python 继续运行，未改认证模式/密钥/代理/客户端。当前生产确认为 weknora_api_key，具体版本见 00。新旧协议均需独立验证，不能自动迁移凭据而扩大 scope。

## 业务映射

| 旧 Python 工具 | 新 Go 入口 | 不等价点与验收 |
| --- | --- | --- |
| list_knowledge_bases、list_shared_knowledge_bases | list_knowledge_bases | 旧为选定空间及组织共享；新个人为 public∪personal，返回 ownerTenant/provenance |
| get_knowledge_base | list_knowledge_bases 的授权投影 | 新 Go 没有完全同名 detail 工具；不返回管理配置/模型密钥 |
| hybrid_search(kb_id,query,config) | search_knowledge(query,知识库选择、搜索模式等) | 不能透传未经约束 config；质量/阈值/partial 状态比较 |
| list_knowledge | list_documents | 分页、上限、类型、标题/metadata 投影需核对 |
| get_knowledge、list_chunks | read_document | 旧详情/分块分页与新正文/context 截断不同；统一 ACL 与引用 |
| wiki_search | wiki_search | 多 KB 目标与旧单 KB 路径不同；来源闭包不可省略 |
| wiki_read_page | wiki_read_page | slug 选择、重名和 owner tenant；历史 page/revision 也校验 |
| wiki_index_view | wiki_index | 结构化目录投影与 KB 选择；不能泄露受限页标题 |
| 无独立 grep 工具 | grep_chunks | 精确路径/函数/错误码检索新增体验，仍用现有搜索实现 |
| chat/agent_chat | 初期无；ask 禁用 | 创建会话/Agent 调用非纯检索，另行安全评审 |
| create/delete/update/model/session 管理 | 个人入口无等价工具 | 如有机器依赖，另用明确服务身份与授权，不静默切换 |

完整旧 31 工具/REST 方法盘点在 02 与 evidence/legacy-tool-routes.json。参数字段以实际工具 schema 和候选版本为准，M5 冻结后生成客户端可验证配置，本批不假设 Codex/WorkMate 当前 UI 字段。

## 分阶段迁移清单

1. M0 完成事实采集；补业务审批/来源 ACL/客户端依赖清单，解决未明项。
2. M1 评审通过；M2–M3 服务层 ACL、文件与衍生安全全部通过。对旧 Python 相同 REST 做无权 canary 测试，确保无旁路。
3. 在打包服务器隔离网络/数据库/存储/凭据部署固定 SHA/digest；禁止候选连接生产队列、生产源同步和真实外部模型。
4. 管理 Go 新路由/反向代理/SSE-HTTP 生命周期，保留生产 Python URL。新建仅测试凭据，逐工具按上表对照。
5. 使用 A–F 合成主体验证 P/D/C/S/M 矩阵；在三客户端验证一次配置、错误/部分失败、断线恢复；建立质量与延迟基线。
6. 真实生产数据发布审批：确切范围、责任人、历史审批证据、来源 ACL、衍生闭包、现有链接处置、回滚方案。不全量公开未知历史。
7. 经单独生产批准后灰度指定员工/KB，监控拒绝、范围变更、partial/latency。无审批不部署、不迁移数据库、不更换 Key。
8. 观察窗口后盘点旧服务调用与机器依赖；确认无人依赖再另批退役 Python、撤除代理与回收对应凭据。不把“零请求一小段时间”当全部依赖清零。

生产影响与回滚见 11；M6 仅准备工作，未满足上线门禁。迁移不是继续裁剪旧 Python 工具数量，也不引入第二个 RAG 引擎。
