# 回退方案

P6生产前门禁、重建配置生效及精确错误绑定回滚分别见 [SOURCEID_P6_RELEASE_GATE.md](SOURCEID_P6_RELEASE_GATE.md)、[SOURCEID_REAL_INTEGRATION_PRECHECK.md](SOURCEID_REAL_INTEGRATION_PRECHECK.md) 和 [SOURCEID_P6_ACCOUNT_MAPPING.md](SOURCEID_P6_ACCOUNT_MAPPING.md)。以下均为待审批操作，本轮未执行。

优先配置回退：关闭 SOURCEID_ENABLE、SOURCEID_LOGOUT_ENABLE、SOURCEID_CONTRACT_CONFIRMED 和 OIDC_AUTH_ENABLE，保留本地应急登录。先在授权环境撤销 SourceID 令牌/会话，确保停用后没有残留有效 SID 登录。
已发行 SID 令牌仍要求数据库会话检查，不能通过关闭适配配置绕过撤销。禁用开关不自动注销本地用户，也不修改 API Key。

应用回退到旧二进制前，必须撤销 auth_tokens 中全部非空 session_id 的令牌，防止旧二进制忽略新增会话状态。
优先保留新增表/列实现兼容回退。确需 down 时，在备份和确认后使用 `003024_sourceid_oidc.down.sql`；down 首先撤销 SID token，再删除新增表/列，用户/空间/知识库不删除。
SQLite down 需要支持 DROP COLUMN 的 SQLite 版本；迁移测试执行实际 up/down 文件。

恢复或改变 external identity/enrollment 前需要单独审核，不能用邮箱自动修复、批量重建账号或静默转绑。
保存审核 CSV 和 reviewer/reference 的安全备份，回滚数据库时同时恢复身份绑定和会话撤销证据，不能只恢复部分表导致旧会话恢复。
