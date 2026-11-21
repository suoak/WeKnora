# 门户申请与消息通知审计

审计日期：2026-11-21。初始本地 main 为 `f5d022d9692e2beb179f03decb2458e6173dc1cc`，与需求中的 `2a29e08e2f727e8ee6d93b1bddfd0d757c50405f` 不同。核实远端后，功能分支已 rebase 到 GitHub main `11dce08198b928fa5caf6648fe0304fc3440d5e7`。原工作区位于 `feature/knowhub-sourceid-oidc`，有 37 个已修改文件及其他未跟踪文件，未覆盖它们。开发使用独立 worktree `E:/code/WeKnora-portal-notifications`，分支 `feature/portal-access-notifications`。

## 现状和原因

- `SpaceAccessDialog` 已有申请表单，依赖服务端 `can_request_access`。配置编辑器及数据库原默认值均为 false，所以默认新配置没有申请入口。生产空间是否因该配置关闭尚未核查，不能用源码代替真实数据库证据。
- 后端根据已发布配置、当前成员、暂停状态和 pending 申请计算申请资格；原弹窗有 pending 提示，但暂停成员和未开放申请均落入“联系管理员”兜底。
- 已有申请创建、本人历史、本人取消、Owner 列表、通过及拒绝 API。前端缺本人历史和取消封装，不能再新增重复业务 API。
- `AccessRequestsPanel` 集成在 Settings 的 members 区域、`TenantMembers` 的 access-requests Tab。前端仅 Owner 显示；服务端要求真实有效 Owner、当前空间与路径空间匹配。系统管理员没有目标 Owner 成员关系也不能审批。
- 审批通过授予 Viewer，拒绝不创建成员关系；暂停成员被拦截；pending 唯一索引及带原状态条件的更新防止重复申请和重复审批。
- 申请、取消和审批已有事务和 AuditLog。通知加入创建和审批的现有事务，任何通知写入失败都必须回滚业务写入和审计。
- `GlobalInvitationBell` 原来仅存在于 platform 布局，有邀请时才显示；门户是独立布局，没有该入口。`MyInvitationsDialog` 复用原接受、拒绝逻辑。
- `App.vue` 已有登录立即执行、每两分钟刷新的邀请轮询，通知复用该定时器。
- 当前没有可复用的通用持久化用户通知收件箱。用户与成员、AuditLog、本人申请历史可复用。新通知按接收 user_id 查询，tenant_id 只表示业务上下文。
- `APIKeyGate` 对未声明 API Key 策略的路由默认拒绝。新增个人通知路由保持未声明，并在 handler 中再次拒绝 API Key 上下文。

## 迁移编号与开关策略

当前 main 的 PostgreSQL 门户基础迁移是 `000093`，SQLite 已重排为 `000015`。远端已使用 `003038` 至 `003049`，本次两套迁移均使用 `003050_user_notifications`，包含 up/down。

新配置默认允许申请；显式 false 保留。更新请求省略开关时保留已有配置，新配置省略时取 true。未保存过配置的管理页也返回 true。迁移改变未来插入记录的列默认值，**不批量修改任何既有空间的开关**。SQLite 重建配置表时保留配置、阶段及申请数据，回滚恢复默认 false。

## 测试方案

1. 复用 Portal service/repository/router 中关于权限、暂停、重复申请、并发审批、Viewer 授权和审计的测试。
2. 增加所有有效 Owner 的通知、无效 Owner 过滤、审批结果幂等、通知失败后业务和审计回滚测试。
3. 增加收件箱认证、API Key 拒绝、分页、未读过滤、单条及全部已读、跨空间本人收件与跨用户隔离测试。
4. 使用项目真实 SQLite 迁移器执行 3049 → 3050 → 3049 → 3050，核对历史数据摘要、默认值、唯一事件约束与外键完整性。
5. 前端运行申请状态、门户回归、安全路由测试、类型检查和生产构建。
6. 测试环境部署后，再用指定 Owner/普通用户真实账号验收通过和拒绝两条闭环；本地自动化测试不能替代该验收。

## 尚需环境证据

生产只读连接、测试部署目标及真实测试账号尚未由本次会话明确提供。已在开发期间询问获取方式。旧 SourceID 部署文档描述其他开发任务，不能据此选择或覆盖运行实例。本次不修改生产数据，也不发布生产。
