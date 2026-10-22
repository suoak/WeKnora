# 第四批：读取面覆盖与剩余门禁

表中的 PASS 限定于明确列出的合成适配器和已执行测试；HOLD 不会因相邻组件通过而自动转为 PASS。M3 整体 HOLD。

| 读取面 | 已实现/已测 | 尚未实现或未验收 | 状态 |
|---|---|---|---|
| 迁移 3033→3034→3035 | RC3 正式来源、实际启动器、PG/SQLite、down/up、故障快照恢复 | 生产库/真实备份恢复 | P0-A 见 25/30；生产 NOT_TESTED |
| Python SSE 凭据撤销 | 真 HTTP/SSE + SDK 2.2/2.3；错误、EOF、清理、重连、业务拒绝 | 代理多 worker/其他 SDK | P0-B 已测范围 PASS |
| REST knowledge detail | Gin 合成适配器执行 DocumentReader 实时授权 | 实际 KnowledgeHandler/Service 统一接入 | HOLD |
| REST list/batch/count/title/tag/summary | 组件级授权和空集拒绝 | 实际分页数量与元数据无泄漏证明 | HOLD |
| Chunk detail/list | 合成 MCP REST 调用 + canonical ownership | 实际全部 detail/revision/neighbor/batch | HOLD |
| 旧 Python MCP weknora_api_key | 合成旧 Key/full_access 不扩权，敏感/未知/mcp_blocked 拒绝 | 实际 rich REST 路由一致性 | HOLD，transport 修复不替代业务 ACL |
| Go MCP | 保留原实现及相关既有回归 | 真实工具到统一文档 ACL 接入 | HOLD；没有进入 M4 |
| SQLite 检索 | 实际 FTS/vector/Hybrid 候选前 allow-set 和 synthetic callback spy | 生产业务 RAG 适配 | 组件实测范围 PASS，整体 HOLD |
| PostgreSQL/ParadeDB 检索 | 未认证受保护请求拒绝 | 真实 BM25/vector/Hybrid allow-set、性能与网络 spy | HOLD/NOT_TESTED |
| grep、多 KB | 合约拒绝不安全授权集合 | 实际检索候选前过滤 | HOLD |
| RAG/Agent、重试与流式输出 | 局部 callback 撤销复验 | 实际模型网络前复验/响应/身份隔离 | HOLD |
| Wiki/FAQ/graph/生成摘要问题 | 内存 DerivedText 全部来源 Grant 复验组件 | 持久化来源依赖及真实读取入口 | HOLD |
| 历史消息引用/多文档结果/搜索缓存 | 未知依赖拒绝合约 | 实际缓存复用/引用读取/失效链 | HOLD |
| 文件 broker | 合成 storage、精确 file Grant、内容摘要、版本、I/O 后复验 | HTTP 端点和真实对象/文件存储 | HOLD |
| 旧 HMAC URL/对象直链/CDN | 威胁及阻断设计保留；不允许公共开放 | 历史已签发 URL 即时撤销、真实 CDN 缓存回收证明 | HOLD |
| 飞书/GitLab 来源 ACL | 未验证来源维持拒绝 | 来源撤权检测、重同步/重复导入/索引一致性 | HOLD |
| 企业审批与员工身份 | 合成 verifier；nil 拒绝 | 正式角色/资格/用户 deny 映射 | NEEDS_DECISION |

## 对应既有矩阵

- D01～D06：组件、SQLite、合成 REST/Python 有局部证据；实际业务直接读、历史衍生、生产驱动未闭环，整体 HOLD。
- C01～C04：实时凭据撤销、不同主体/工作区/会话不注入、旧 Key 无新能力有测试；全业务工具范围仍 HOLD。
- P01～P04：合成批准、撤销、未知来源和旧 Key 无扩权重新回归；正式角色与企业资格未决，Portal 公共开放不批准。
- S01～S05：真实 SQLite、组件 race、失败恢复及 SDK 有界错误有证据；生产后端检索/网络 spy、代理拓扑、完整性能与长时间稳定性 NOT_TESTED。

没有将现有 P0 PASS 汇总成整组 D/P/C/S 或 M3 PASS。原第三批两个 FAIL 留作历史记录，新候选对应 P0 新结果单列。完整命令/退出码和本批额外 FAIL 见 30。

## 风险与停止点

实际 rich Handler、模型调用、匿名资源链接、历史来源的授权闭环未完成；不能启用公共授权或开放混合敏感 KB。普通 full_access Key 不能作为新文档授权实现的豁免，客户端来源 Header 也不能作为 MCP 可信身份。

当前停在 commit/push/merge/release/deploy 之前。未执行 M4、M6、生产 Python 切换、生产数据库迁移、历史公开或成员批量修改。
