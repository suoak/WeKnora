# 开发与验证报告（2026-10-12）

基线：`ac7dbe5e2607af5ab4e74c062137e0cad48d0e50`；功能分支：`feature/knowhub-sourceid-oidc`。
代码交付仍处于验证阶段，最终 SHA 和云端运行链接将在检查结束后补充。

## 范围

复用 OIDC 和本地 JWT；独立企业身份/审核映射/会话/登出 tombstone；一次性 state、nonce、可选 S256 PKCE；固定批准 URI；登录结果 cookie 兑换；XML 与显式压缩解析；审核 CSV 工具；PG/SQLite migration；Compose/env 和六份文档。
未知员工自动开户、Token Response 扩展字段读取、原生 SourceID 通知签名、真实压缩/响应兼容性尚未实现或未经真实协议证明；保持启用门禁关闭。

## 本地结果

前端 `npm run type-check` 通过。XML parser 用例通过。
初次 Go 运行被旧 MinGW 缺少 __stack_chk_fail/RstrtMgr import library 阻塞；在工作区外补充原系统 DLL 的 import library 并链接 libssp 后，部分测试可运行。
完整回归出现既有 PostgreSQL parser 原生崩溃及无关用例失败，不能声称 Windows 全量通过。最终以本分支云端 Linux 结果作为验证依据。

新增测试覆盖：审核登记冲突与旧 User ID/管理员属性保持；邮箱变化；禁用账号；SQLite 实际 up/down；状态/结果一次性消费；会话精确撤销/不同设备隔离/晚到 callback；刷新条件消费；Mock OIDC nonce/iss/aud/exp/工号/状态失败；UserInfo sub 一致；空间派生令牌保持会话；XXE/畸形/超大/重复字段/压缩炸弹；直接 peer CIDR 验证不信任 forwarded header；CSV dry-run、幂等和整批回滚。
保留所有既有测试，不删除失败回归。

## 云端

用户明确授权 GitHub 云端验证。工作流 `.github/workflows/sourceid-oidc.yml` 在功能分支执行 Linux race、安全用例、认证/MCP 回归、临时 PostgreSQL migration 和前端检查；不含任何部署步骤或生产凭据。
所有 Mock 使用本机 httptest/临时测试库；云端 PostgreSQL secret 是一次性测试夹具，与真实 SID/生产无关。

## 外部状态

`BLOCKED_EXTERNAL`：企业完整 OIDC 文档访问尚需只读授权；正式端点、员工身份/状态、SessionIndex 关联、来源认证、压缩/响应契约尚待确认。
尚未执行真实 SID 登录/登出，因此不声明 SID_INTEGRATION_VERIFIED、READY_FOR_RELEASE。
最终 IMPLEMENTED_LOCAL/READY_FOR_SID_INTEGRATION 状态必须由相应测试和外部契约证据补齐后确认。
