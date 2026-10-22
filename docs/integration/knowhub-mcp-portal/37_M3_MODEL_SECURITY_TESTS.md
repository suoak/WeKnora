# 第五批模型与衍生内容安全边界

**M3 模型输入门禁 HOLD。** 本子批没有真实业务 Rerank/LLM HTTP spy 正向召回证据，没有完整 RAG/Agent、流式响应、历史引用、Wiki/FAQ/graph 或真实文件存储撤权验收。

已实现并加入测试的范围：

- 实际 Knowledge/Chunk 服务在精确文档授权后读取正文；原始 Grant 保留到响应 flush，撤权后缓冲正文不输出。
- 实际 KB service 在未认证检索/embedding 模式拒绝，测试中缺少模型 service 也不会调用到模型。
- 试点 HTTP 隔离边界在 chat/search/Wiki/file 等未认证路由 Handler 之前拒绝；计数 spy 断言 downstream handler 调用次数为零。这是 **HOLD 阻断测试**，不是实际模型网络输入验证。
- Python 通过实际核心 REST 验证敏感拒绝与公共文本读取；拒绝 Hybrid/Wiki 工具不等于它们已经具有安全正向实现。

以下继续 NOT_TESTED / HOLD：真实 ParadeDB 候选预过滤后调用外部模型；外部 Rerank/LLM 输入 canary 证明；模型网络调用前并发撤权；Agent 工具异步上下文、重试和流式 chunk 边界；模型响应引用、历史缓存命中；跨文档衍生依赖持久化及完整性。

文件、原始附件、图片、预览、导出及历史匿名链接不放行。现有一天签名 URL、`/r/` bearer 引用和对象直链/CDN 在外部发放后是否能立即撤销，没有真实证明。本批仅在受控试点 router 中阻断相关路径，**不能撤销外部对象存储或 CDN 中已签发的 URL**，对应资源继续 HOLD；不以缩短有效期替代撤权。

飞书/GitLab 应用账号权限不代表员工权限。外部来源权限再同步、删除/撤销、重复导入、来源版本治理仍待闭环；没有 source verifier 的资源拒绝读取。用户级 deny 优先级尚未决策，不用临时放宽替代。下一子批按 [36](36_M3_REAL_RETRIEVER_ACL.md) 的真实检索顺序完成后，再验证这些衍生与文件路径。
