# P1 最终候选脱敏证据

日期为 UTC；本轮北京时间为 2026-11-11。仅包含合成测试的状态、计数、内容摘要和制品身份，不保存真实知识正文、请求原文、Key、证书私钥或原始日志。

| 证据 | 含义 |
|---|---|
| `source-manifest.json` | 23个Go改动文件本地/测试机摘要一致，固定main参考基线 |
| `build-results.json`、`build-done.json` | 最终core、embedding隐私、fixture编译、完整APP编译、race、vet六阶段成功 |
| `deployment.json`、`test-runtime-metadata.json` | 专用测试资源、环回端口、镜像ID、挂载只读属性、Nginx SSL支持及配置验证 |
| `fullapp-start.json`、`final-runtime.json` | 最终候选健康，磁盘制品与运行挂载二进制SHA256一致 |
| `fullapp-acceptance.json`及`-done.json` | 完整APP 107检查全部通过，八工具、双入口、权限、拒绝与日志等 |
| `faults.json`及`-done.json` | 25项撤权、故障、慢body、TLS和回滚通过 |
| `fullapp-side-effects.json`、`fault-side-effects.json` | 九张业务表、文件存储和12类Redis任务前后相同 |
| `codex-result.json` | 实际Codex CLI 0.146.0完成8工具，TLS校验开启，无Shell/文件操作 |
| `final-log-proof.json` | 最终测试和Codex调用后，八类测试日志源无敏感标记或测试Key，APM标记导出0；真实模型调用发生 |
| `service-security-result.json` | 既有权限/PortalM6/M7/V1/Personal服务定向测试成功；测试源未修改业务服务 |
| `final-*-exit.json` | 最终重启、验收、故障运行进程退出码 |
| `evidence-index.json` | 本目录JSON证据的SHA256索引 |

最终二进制 SHA256：`1cb5e70df5b31f8c96c28c71f0a339251117154d3f53b40c5417a44f5c290c9e`。

原始合成运行目录和先前失败/中间候选记录留在打包主机受控目录，不能自动继承为最终PASS。测试CA/叶证书短期有效，测试Key为新建且短期有效。服务只监听环回，临时模型/APM测试接收器有有限生命周期；后续复测应通过受控测试脚本启动，不把它作为长期生产服务。

本目录不证明生产版本、实际隔离区网络、生产完整证书链、WAF/外部APM或对象存储桶策略。详细未运行项见 [30项矩阵](../../P1_TEST_RESULTS.md)。
