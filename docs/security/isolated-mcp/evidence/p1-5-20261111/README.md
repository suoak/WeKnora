# P1.5 合成环境复测证据

范围：经批准的打包主机专用测试目录、测试账号/Key、合成知识库、完整 APP 与独立 Nginx/PG/Redis。未使用正式证书、员工 Key 或生产数据库。证据只保留检查状态、摘要和数量，不包含原始 JSON-RPC、知识正文、凭据或私钥。

`source-manifest.json` 比对本地 17 个 Go 改动文件及 2 个测试配置文件与打包主机实际源码的 SHA256。`build-results.json`/`build-done.json` 记录 core、embedding privacy、fixture build、APP build、race、vet 六阶段全部通过。`final-runtime.json` 证明最终运行的二进制与构建产物 SHA256 一致；该散列**不是生产镜像 digest**。

`p15-nginx-reload.json` 记录固定转发到隔离只读路由的配置验证及重载。`fullapp-acceptance.json`/`fullapp-acceptance-done.json` 是修正后完整 APP 的 107/107 项通过记录；`faults.json`/`faults-done.json` 是 25/25 项故障、撤权、回滚相关检查。`fullapp-side-effects.json` 和 `fault-side-effects.json` 对比业务表、合成对象文件和任务队列摘要，均无业务写入变化。

`p15-output-preservation.json` 与 `p15-legacy-contract.json` 记录同一测试 Key 的旧 HTTP、新 HTTPS 知识范围一致；旧入口原正文契约保留，新入口遮盖物理路径并保留路径后的正常文本。`codex-result.json` 记录真实 Codex MCP 客户端八工具全部成功调用且无 Shell/文件操作。`p15-log-audit.json` 在该调用后复查 APP/PG/Nginx/受控模型与 APM 测试范围，合成查询、物理路径、签名链接标记及测试 Key 未进入可见日志，LLM Debug 文件数为零；未保存日志原文。

这些结果不能替代真实隔离区网络、生产 TLS、生产 WAF/APM/模型留存、对象存储桶策略、生产容量和生产回滚验收；[P1 30 项矩阵](../../P1_TEST_RESULTS.md) 的 PARTIAL/NOT_RUN 状态继续保留。
