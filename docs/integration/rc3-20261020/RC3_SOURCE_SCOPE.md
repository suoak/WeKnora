# RC3 源码收口范围

联合分支：`release/knowhub-oa-feishu-rc3-20261020`。
冻结父版本：`5f988be51779f1a22bd9d027b7503fe6a8749f62`。
本次只读拉取核实 main：`854f3b956ae3600e12a3ffd2fc40d4fe624ced74`，是冻结父版本的祖先。

候选继承已部署 OA 方案 B continuous_test + 飞书 v7 / RC1，再继承 RC2 克隆迁移与诊断测试。未将 P1-3/B1/E0 实验或 3032 纳入候选；未整体合并上游。

`evidence/source-manifest.json` 固定 5685 个源码文件，SHA256：`bbdf9b880ec27277bb0c2b7ea385d9d23fd1e28e7bc2bc1d29e7d9e0958c391d`。文件清单含 RC2 继承库存，新增 RC3 Go/TS；报告、构建产物及凭据不属于源码清单。清单内 `changed_from_rc2` 明确 18 个文件。

RC3 相对 RC2 的产品改动只有旧来源停更屏障（service、scheduler、新 datasource 策略文件）和邮箱前缀展示（UserMenu、UserProfile、工具函数）。其余为真实 PG/Asynq、调度/配置测试及 CI 编译卫生处理：

- 重启测试通过原构造器重建 service，避免复制含 sync.Map 的实例，不改原恢复断言。
- 两个同目录独立诊断 main 文件加 ignore build tag；显式 go run 仍已验证，不混入普通包编译。
- 能力键测试解析先去 CRLF 空白再去逗号；新增 LF/CRLF 相等及真实错误键检查，不修改后端能力或前端能力业务逻辑，不删除失败断言。

`application_grant` 是管理员统一应用及源作者授予应用文档访问的路径，不是提交链接员工的个人飞书身份。KB 写权限仍需单独校验，入库内容遵守 KnowHub KB ACL。业务已接受凭链接导入应用可读资源的发布策略。

`user_oauth` 有历史实现，但真实 A/B、HTTP 平台回调接受等验收不齐，首发关闭，不能用 application_grant 的通过结果替代。个人变化版本仍安全拒绝，不使用先删后建 Replace。

本轮源代码扫描未发现新增私钥或 JWT；凭据样式命中已人工复核为样例/配置引用/错误码，不发布真实受控配置。实际数据库备份、文件、Token、Cookie、SSH 凭据及原始请求仅留在受限 Git 外路径。

最终提交、APP/Frontend 镜像及测试环境生效状态由交付记录另行锁定。此报告不把源码 Manifest 等同于提交 SHA 或已完成全部上线门禁。
