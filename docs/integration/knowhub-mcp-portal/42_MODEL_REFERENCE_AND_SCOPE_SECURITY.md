# 第六批模型输入、结果与权限边界

搜索范围在任何正文候选进入模型前由服务端解析。真正的 PostgreSQL 查询包含精确资源及 canonical owner 过滤；模型调用前再次验证当前 Grant、Key、员工状态和 Portal。响应输出前再次检查原授权证明，不能以新 Grant 给旧正文背书。

集成测试使用项目现有 `embedding.NewEmbedder`、`rerank.NewReranker`、`chat.NewRemoteChat` 发送真实 HTTP 请求到受控 mock upstream；并非只用计数 callback 假装模型输入验证。仅为测试用 SSRF_WHITELIST 显式允许 loopback spy，没有关闭生产 SSRF 检查。spy 记录请求路径/次数及敏感 canary 是否缺失，不导出真实密钥或生产内容。

两个公共 KB 使用不同维度的合成模型，私人 KB 独立 owner；HTTP Embedding 输入只含查询，Rerank/LLM 正文仅来自已授权结果。索引同时包含敏感 canary、伪造 owner、stale body，负向断言需证明这些字节不进入模型 wire。模型 consumer 之间撤销 Portal，下一网络 consumer 必须不执行。具体 PASS/FAIL 绑定最终候选见 [44](44_SIXTH_BATCH_TEST_RESULTS.md)。

LLM 仅作为可信模型边界验收 consumer，不向个人入口增加 ask 或生成答案工具；当前返回文本检索结果及 knowhub 受控引用。消费接口是显式注入的受信 adapter，生产 disabled，不把测试模型配置自动绑定到生产。

Portal 公共与独立私人授权分开；收回公共范围不会删除用户已有成员/Key/私人授权。撤销成员或 Key 后旧 scope 必须失效。通过 `X-User-ID`、X-From-MCP、UA、任意 KB ID 不能扩权。普通 full_access 机器 Key 不进入个人身份入口，原 M3/M5 文档明确拒绝组件继续保留。

文件安全仍 HOLD：不返回对象直链、原始文件、图片、附件、预览、匿名签名 URL。knowhub 引用不是 bearer capability，不支持匿名换取正文。既有 CDN、对象存储和旧一天 URL 在本批没有被撤销，相关资源不能公开。Wiki/FAQ/graph/历史缓存/引用的来源关系未完整闭环，因此 Wiki 三工具不暴露。MVP 输入安全 PASS 不能扩展为完整 RAG/Agent/流式/文件 ACL PASS。

沿用 M2/M3 的保守文本规则：正文中的任意 HTTP(S)、storage/resource/file marker 或图片引用也会阻断，尚不区分普通外链与匿名资源直链。这会降低合法资料覆盖率；没有 broker/链接可信度证明前不自动放宽，也不将“去掉链接后返回剩余片段”充当完整原文读取。
