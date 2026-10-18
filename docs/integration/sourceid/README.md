# SourceID/OA 当前状态

2026-10-18最新状态：核心功能开发完成，已有账号真实关联和独立空实例JIT真实验证通过，打包测试环境已**持续启用OA及JIT**。正式生产发布待飞书个人文档联合上线。

- 当前站点：172.30.232.42，knowhub-company-test，80/8080；APP5f988be51、Frontend832b6652a、DB3030 clean。
- [持续启用及恢复说明](SOURCEID_TEST_SITE_PERSISTENT_ENABLEMENT_REPORT.md)：这是当前运行基线，旧“一次测试后关闭门禁”的历史操作卡不再代表当前授权。
- [核心功能验收及证据缺口](KNOWHUB_OA_LOGIN_MODULE_FINAL_ACCEPTANCE_REPORT.md)：真实Trace仍有PARTIAL，不能追认为完整捕获。
- [飞书个人文档后续开发](../FEISHU_PERSONAL_DOCUMENT_PROGRESS.md)：复用已有能力继续，不重新开发OA。

此状态不是生产发布授权，不操作10.51.134.114，不将测试用户或绑定同步生产。原域名仅在hosts指向测试机的获准工作站使用；不修改生产DNS。
