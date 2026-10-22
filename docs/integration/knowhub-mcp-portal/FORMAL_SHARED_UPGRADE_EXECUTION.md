# 现有80/8080正式测试升级执行清单

依据用户附件 `069a9f12-7d0c-44d3-858a-098e2f254753`：允许覆盖打包服务器P3测试APP/前端、测试数据库3033→3037迁移、必要恢复及验收；全部门禁通过后允许功能分支、PR、CI和main合入。生产仍无部署、迁移、停更解除或DNS/SID变更授权。

原候选c2f260317fc151fb在真实恢复库暴露3037迁移缺陷，经3文件最小修复后重新冻结：完整源码 `0a7374bbbdbd8c4a802733811ad3ecbeb70293ba33b89eefa9bd05c36ecbd371`，业务源码 `090216bc1a7d0c547235df30eef6f425050b9260fadba9791329b6c267a24783`，目标3037。原5468、独立飞书补丁、b470与c2回环实例、所有冻结包和历史证据保留。

| 顺序 | 工作 | 状态 | 证据/边界 |
|---|---|---|---|
| 1 | 受控备份、独立恢复、原密钥/数据保全 | PASS | backup-a3恢复3033，关键原表数量/指纹相等；原6条加密Key解密及hash验证通过。文件/Redis/配置/AES受控保存于数据盘；缺失宿主绑定配置从运行容器恢复 |
| 2 | 维护窗口、停旧Worker、正式迁移与关闭启动 | PASS | 原失败及dirty恢复库保留；新恢复库及真实测试库正式3033→3037退出0，dirty=false；维护时入口503、旧APP/Worker停止、数据库遗留连接0；原有数据指纹不变 |
| 3 | 现有80/8080同源候选部署与启用 | PASS | deploy-a4：同源APP/前端与实际二进制SHA一致；关闭403、启用后未认证401、健康200；原Python容器及原冻结实例保留，无新MCP端口 |
| 4 | UI、Codex八工具、Portal、dynamic_all、Wiki授权 | IN_PROGRESS | business、portal-dynamic、真实浏览器PASS；Codex八工具已实际返回，正在补充飞书更新正文与错误语义复核；全部使用新合成身份，不触发生产SID回调 |
| 5 | 飞书状态机/并发/Cursor、恢复、硬化回滚 | IN_PROGRESS | Wiki/Drive真实Worker与PG合成云端故障注入执行中；真实飞书应用BLOCKED；旧测试签名示例值被正确拒绝，仅配置独立测试签名密钥，AES不变 |
| 6 | Python/OA/Portal/飞书个人导入/Docx/表格兼容 | IN_PROGRESS | 完整Go/race/vet与构建已PASS；补充PG规模及Python全套/SDK回归；SID沿用既有实现，正式模型凭据BLOCKED |
| 7 | 逐项审查P0、固化提交树、PR/CI/审查/main | TODO | 门禁有关键FAIL/BLOCKED则不合入；不得以旧合成PASS代替新目标验收 |
| 8 | 最终交付和停止 | TODO | 报告源码/镜像/Schema/测试/PR与生产剩余门禁，不自动生产发布 |

所有新检查/操作证据保存 `evidence/formal-shared-upgrade/`，私有备份/环境变量只留打包服务器受控目录，不提交Git。
