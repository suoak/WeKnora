# 统一候选开发与验收执行清单

本轮只在 `E:/code/WeKnora-worktrees/knowhub-v1-wiki-dynamic-20261022`、分支 `integration/knowhub-v1-wiki-dynamic-20261022` 开发。RC3 HEAD `e62b17e2d305358467821aeff1cce63ce1d61254`；继承 b470 的完整候选快照，逐文件校验见 `evidence/v1-wiki-dynamic/baseline-copy-manifest.json`。原工作树、冻结包和实例不修改。

| 顺序 | 任务 | 状态 | 证据 / 剩余问题 |
|---|---|---|---|
| 1 | 校验包、Manifest、独立快照 | PASS | b470 包 SHA256 `8ba08087207516dac79842e2939ebe726b4ee30576fb205827047a8201c68edf`；5943 个业务文件核对；完整快照6540文件 |
| 2 | 来源级失败隔离与部分结果语义 | PASS | 新候选真实PG/race及APP/REST/Worker联合验证：HOLD拒绝、健康来源继续读取、partial、请求中撤权安全重试；旧未知RAG保守拒绝范围见最终报告 |
| 3 | 保留撤权能力的安全回滚门禁 | PASS | 兼容镜像 `knowhub-safe-rollback/app:662485ffc851ac83` 九项真实克隆库门禁全过；许可绑定候选与证明SHA，不 down3036/3037；无生产回滚授权 |
| 4 | 动态全部个人授权模式 | PASS | 新 Key 显式 dynamic_all；两合成员工、未来私人空间新增及撤权、历史 fixed 不扩权；真实APP与浏览器验证通过 |
| 5 | Wiki 3只读工具与可信来源依赖 | PASS | 八工具实际Codex调用；真实APP负向验证归档、未知/混合来源、旧Writer及ABA、缺索引无写入；历史未证明页面继续HOLD |
| 6 | 开发基本质量门禁 | PASS | 源码 b702755c：完整Go、完整相关包race、vet、构建、真实PG/race、正式3033→3037迁移退出0；前端45项/类型/构建0。原始失败保留 |
| 7 | 完整源码冻结及同源镜像 | PASS | 完整候选 c2f260317fc151fb，逐文件回读；APP/前端同源镜像ID、二进制SHA与构建Manifest见最终报告；e53仅为保留的中间失败候选 |
| 8 | 新独立部署与集中业务验收 | PASS | c2f260317fc151fb独立实例；26项集中命令全0；八工具、动态Portal/私人授权、HOLD、Wiki、飞书Worker、回滚、迁移、Python、浏览器、Codex实测通过。性能与未配置SKIP边界见测试报告 |
| 9 | 报告及停止 | PASS | `V1_WIKI_DYNAMIC_FINAL_DELIVERY.md`、`V1_WIKI_DYNAMIC_TEST_RESULTS.md`、需求矩阵与逐文件Manifest；真实SID/模型/云端/生产积压未验收，生产关闭/停更保持，不提交推送发布 |

不混淆飞书 Wiki 来源同步与 KnowHub 生成 Wiki。保留已通过的飞书状态机、原子切换、Cursor和旧Worker围栏；仅发现具体缺陷才修改。不得以 Unknown 为健康、放宽授权或关闭断言来获得通过。没有生产访问或写入授权。

## 集中验收后的候选修订记录（2026-10-22）

上述 e53 候选保留为中间候选，不能作为整体 PASS：集中验收的 tagged PostgreSQL 迁移测试退出1。其旧 Schema/行摘要夹具将3037新增字段和表也纳入“旧结构完全相等”比较。只修正对应测试夹具，仍比较全部既有字段，并新增历史 Key 为 fixed、历史 Wiki 未自动获得证明的断言；业务代码、迁移和授权断言未放宽。原始 FAIL 证据保留。

修订后业务源码清单指纹（含测试）为 `b702755c033328560bbb28802c93a3a1abce31a43352870b43d8f1fee2d74cdb`。开发完整 Go、vet、构建、真实PG与正式 tagged migration/race 已退出0；完整相关包 race 尚在执行。待其通过后冻结新完整候选，使用新的独立 Compose 项目重新集中验收，不覆盖 e53。

e53 已验证范围：真实 APP 八工具、两用户动态私人授权新增/撤销、浏览器创建/重新查看/轮换/配置、Codex 实际八工具、Wiki 归档及原始写入导致证明失效、飞书 Wiki/Drive 合成服务与真实 Worker/PG 联合状态机均退出0。兼容回滚仍在执行，不能提前视为允许镜像。真实 SID、正式模型、飞书云端及生产积压未验证。

新增 Portal 管理路由测试已实际通过发布新空间和空 KB 自动发现；随后合成 Chunk 克隆夹具重复 seq_id 导致退出1，原始结果保留。已修正夹具分配新 seq_id，在新候选完整重跑，不修改产品检索或授权逻辑。

修订源码的完整相关包 race 已于06:06:52Z退出0（access/repository/service/middleware/mcpserver/router，25分钟上限，实际约718秒），开发门禁全部通过。兼容回滚首次探针因容器名超过DNS单标签63字节而在发出HTTP请求前失败，未获得回滚许可；脚本已改用短且候选绑定的名称，新候选重新实测，原始 FAIL 保留。即将冻结的候选包含这些测试修订，不修改既有冻结包。
