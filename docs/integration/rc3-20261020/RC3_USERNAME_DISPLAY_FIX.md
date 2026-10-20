# OA 用户名显示修复

2026-10-20 10:42（Asia/Shanghai）已更新现有测试环境前端。生产未操作。

原 JIT 逻辑将 username 设置为 `oa-<UUID>`，用户菜单及个人设置直接展示该内部值。此次仅调整展示：严格匹配自动生成名称的账号使用邮箱前缀；自定义用户名保持原样。自己的默认个人空间名称采用相同显示规则。

数据库 username、user_id、RJGH/RJEMAIL 绑定、会话、JIT 和 ACL 未修改。没有进行批量账号重命名或修改认证逻辑。页面“用户名”字段为只读显示，数据库内部名称仍为原值。

修改文件：

- `frontend/src/utils/userDisplayName.ts`
- `frontend/src/utils/userDisplayName.test.ts`
- `frontend/src/components/UserMenu.vue`
- `frontend/src/views/settings/UserProfile.vue`

验证：名称专项 11 PASS / 0 FAIL；Type Check、生产构建退出码均为 0。完整前端测试仍有独立 RC1 基线可复现的 5 个失败，此修复不将其记为 PASS。

部署证据：

- 测试目标：172.30.232.42 / aqyfzx10。
- 前端镜像：`sha256:1d1e2c55996ea8be260a1103b22c9a33c06631f5570507076c4504f245e780db`。
- 展示补丁及 dist Manifest：`3692f104720a4daab64ad3f29325247fe7746d745321448a199b386bf86ed3ef`。
- 这是有界前端测试补丁，不是已提交并完成所有门禁的正式 RC3 镜像。
- 页面 HTTP 200、APP health 200；运行容器内 index.html 与构建文件 SHA256 一致。
- APP、PostgreSQL、Redis 容器 ID 保持不变；未重建或迁移业务库。
- 数据库备份已在独立无网络实例恢复验证；文件归档校验和旧镜像归档完成。
- 原前端回滚镜像：`sha256:e262122048b835e27c8e523cf98118aa3440120f3ba6c19cd3651460161381b0`。
- 回滚命令保存在测试机受限目录 `private/rollback-display-name.json`；本次部署成功，未执行回滚。

原始部署 JSON、配置备份、构建日志与完整 dist 清单保存在打包机 `/home/aqyfzx/ai/knowhub-rc3-20261020/` 下的 evidence / private / artifacts 中，未写入凭据到报告。

用户浏览器仍需强制刷新后复核；未将用户未反馈的刷新结果记为浏览器验收 PASS。其他 RC3 发布门禁保持独立，不由此显示修复替代。
