# 剩余阻塞与执行权限

当前：IMPLEMENTED_LOCAL / BLOCKED_EXTERNAL；P6文档准备完成；真实联调NOT_TESTED；READY_FOR_RELEASE=false。所有开关保持关闭。本轮没有运行代码改动、真实凭据使用或数据库操作。

| 阻塞 | 状态/负责人 | 解除所需证据与下一步 |
|---|---|---|
| 正式issuer/元数据/签名/端点/client auth/Profile/nonce | BLOCKED_EXTERNAL / SID | [Q01–Q04](SOURCEID_SID_QUESTIONS.md)；只读元数据核验与必要配置差异 |
| 自然人工号、状态/岗位身份、sub生命周期 | BLOCKED_EXTERNAL / SID+HR+应用管理员 | Q05–Q06；可信path/字典及审核测试员工/原UserID |
| OIDC会话 ↔ SessionIndex、设备范围/生命周期 | BLOCKED_EXTERNAL / SID | Q07–Q08/Q13；两独立会话相关性、正式事件范围 |
| 通知来源/编码/ACK/送达 | BLOCKED_EXTERNAL / SID+平台安全 | Q09–Q12；真实入口信任、直接peer/不可绕过、正式HTTP契约 |
| 敏感日志排除 | BLOCKED_LOCAL_GATE / KnowHub+网关+APM | 最小敏感路由排除和后端专项/云端验证；proxy/APM脱敏配置与synthetic验收。当前未修复，不能开始真实测试 |
| 受控callback/网络/环境隔离 | NOT_TESTED / 平台部署 | 本机DNS/TLS不替代实际容器/发送节点检查；正式域名分流若需要变更，先提方案批准 |
| Secret安全注入/服务重启 | NOT_TESTED / 用户+运维 | 独立受控服务环境；Agent不读凭据。启动读取验证仅bool。重建测试实例需要相应环境授权 |
| 测试用户绑定/权限基线 | NOT_TESTED / 用户+审核人+DBA | dry-run差异、用户再次确认绑定、隔离库apply与权限核对；无邮箱自动绑定 |
| 真实schema诊断 | NOT_TESTED / SID+KnowHub | 优先正式脱敏字段契约；现公开接口无schema诊断。若需诊断补丁，完成验签/nonce/subject后只输出类型，不绑定/签发本地令牌，并验证/审阅后授权首次登录 |
| HTTP传输风险/安全验收 | BLOCKED_EXTERNAL / 企业安全+SID | 保持批准HTTP；若需HTTPS，新审批、固定callback校验适配和全链路验证 |
| A–I与真实报告 | NOT_TESTED / 用户+测试/SID | 具备上述条件后单独确认首次员工登录与真实SID登出，逐项执行及受限证据归档 |
| 生产备份/回退/发布审批 | NOT_TESTED / 发布人+DBA | 不以CI或准备完成代替上线；独立演练/审批后才能变更生产 |

## 已完成与未执行

2026-10-12 23:28+08:00补充：用户指定Linux测试机的交互SSH和Docker/目录/部署只读检查成功；80/8080已有实例占用，需专用隔离部署。没有读取秘密或改变服务；实际主机/容器SID出站仍未测。详见 [环境报告](SOURCEID_TEST_HOST_ENVIRONMENT_REPORT.md)。此前“未执行服务器检查”仅指应用网络/业务接口等剩余项目，不再指主机Docker清单。

完成：锁定CI/代码基线和原工作树；读已授权协议快照、复核公开登出文档；更正工作表计数；本机有限DNS/TCP/TLS及批准两路径无凭据HEAD（401，不能证明handler）；源码端点/代理/门禁/应急入口/secret加载检查；正式契约、占位模板、SID最终问题、审核映射/回滚、A–I与发布门禁；创建六份真实报告并明确缺失证据。

未执行：服务器容器出站/SID节点入站、真实HTTP回调、任何实际环境启用/重建/代理调整、实际员工登录/Token/Profile、数据库dry-run/apply/迁移/身份删除、真实SID登出、生产备份/回退/部署/主分支合并。没有把Mock结果填为真实PASS。

## 用户后续确认边界

可继续只读源码/模板、Mock、已知端点的有限非破坏性检测；已获用户“先用本机”指示。本轮已有密钥由用户保管，不索取任何密钥/员工密码。

**必须再次确认**：开始真实员工登录、真实SID登出、真实身份绑定、正式域名路由、生产数据库、生产部署、合并主分支。每次提交可审阅环境、契约、影响范围和回滚步骤；不到条件不生成登录URL/不设置真实开关。

优先收口顺序：SID正式非秘密契约 → 普通测试员工/原账号审核 → 隔离callback与网络 → 日志/secret安全验收 → 映射dry-run差异/绑定确认 → 登录确认/A/B/C → 通知契约与登出确认/D–I → 真实证据结论。若缺会话字段，可先评审独立只读schema诊断方案，但不能关闭SourceID认证走自动邮箱开户。
