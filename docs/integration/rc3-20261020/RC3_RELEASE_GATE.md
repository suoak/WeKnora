# RC3 发布门禁

结论：**测试候选已部署，核心普通员工业务通过；NOT_READY_TO_MERGE / NOT_READY_FOR_PRODUCTION。**

| 门禁 | 状态 | 实际范围 |
|---|---|---|
| 3023→3033 真实克隆迁移 | PASS（RC2） | 不重复全量迁移、不补造3032；生产仍3023 |
| 物理备份恢复 ANN 等价性 | PASS | 18/18与生产一致；双方自召回17/18，保留原失败点 |
| 旧来源短期停更业务方向 | CONFIRMED | 2Wiki+5Drive、354知识；窗口/期限/负责人待指定，上线后独立修复增量 |
| 旧来源停更防绕过 | PASS（新候选隔离/测试） | API/scheduler/queue，真实PG/Asynq/Race；不约束旧二进制，不代表生产已暂停 |
| 测试 RC3 源码/镜像/备份 | PASS | 代码5156、实际归档构建、备份独立恢复、APP/UI健康200 |
| 普通员工真实 OA / ACL / 退出 | PASS（已执行范围） | 同user_id两次登录、授权KB、8项403、退出旧Access401 |
| application_grant 三格式首导 | PASS | 普通员工真实导入最终解析/索引、检索/来源、同用户重复KID稳定 |
| 旧文档与数据保护 | PASS | 原三篇9组摘要一致；旧三格式Portal/Hybrid正常，352Wiki保留 |
| RAG | PARTIAL | Docx/Bitable与Sheet简短事实通过；Sheet长回答截断保留 |
| 跨导入者重复提示 | FAIL / OPEN | generic400未达到具体重复错误断言，未确认根因，不伪报PASS |
| 真实新员工RC3 JIT / Refresh | NOT_EXECUTED / NOT_AVAILABLE | 不创建虚假身份；当前账号无Refresh |
| 大表/高级字段能力 | DEFERRED | >500真实样本不足，首发不承诺 |
| Python MCP 真实调用专项 | WAIVED | 用户明确移出门禁；保留原镜像/服务/接口，未迁移Go MCP |
| GitHub CI / lint / 本地完整Go | FAIL / OPEN | 见RC3_MAIN_CI_REVIEW与RC3_VALIDATION，不具备合并条件 |
| HTTP OA生产风险接受 | PENDING | 测试许可不等于生产风险接受 |
| 生产OA有效配置 | PENDING | 独立审批引用/范围/time_limited期限；不复制continuous_test |
| main外部自动部署核实 | UNKNOWN | main/v*推镜像已确认；外部拉取需运维明确 |
| 正式生产执行批准 | NOT_GRANTED | 未暂停生产来源、未迁移/重启/替换生产服务 |

## 最短后续顺序

1. 处理实际硬 CI：OA 既有试点并发误拒绝、Frontend 3项历史审计、lint；纠正隔离DNS测试条件后重跑完整Go。不能删除断言、仅提高基线或将已知失败写成PASS。跨导入者错误提示至少需查明并明确首发影响；不扩大认证或索引架构。
2. 对涉及产品代码的最小修复单独审查、补定向/Race、锁定新提交并按新源码构建；重新部署测试，执行必要 OA/三格式/权限/退出回归。当前5156镜像不冒充之后的新提交。
3. 明确生产窗口、停更紧急联系人及允许陈旧期限；取得HTTP生产风险接受、OA生产time_limited配置授权和正式发布批准，确认main镜像发布无意外自动部署。
4. 关键门禁通过后申请合并，不强推；用实际合并SHA构建并锁定镜像，按正式Windows上传/维护窗口/一致备份/旧Worker排空/正常迁移/小范围冒烟执行。保留原PythonMCP。生产步骤见RC3_RELEASE_OPERATION.md。

当前没有批准生产执行。已执行的生产活动仅为用户专项批准的一次物理备份与只读检查；物理恢复全部在无业务网络的隔离副本。测试实例保留RC3，专用普通员工浏览器已退出关闭；无身份/权限/数据异常需要紧急回滚的证据。CI与功能限制不因测试服务健康被消除。
