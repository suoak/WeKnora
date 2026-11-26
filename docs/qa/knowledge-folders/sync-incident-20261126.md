# 飞书同步停止：独立只读诊断

## 已确认事实

诊断对象仅为测试项目 `knowhub-feishu-v2-d6e99d894d3a`、Redis DB 0、namespace `legacy-feishu`。没有清锁、删除队列、修改配置、重放归档或移入生产 Canary。

- 首次出现相同原因的保留记录：北京时间 **2026-10-27 15:00:37.618**。
- 当前停止对应的最后一条 `trip` 首次触发记录：**2026-10-29 11:03:33.920**，stream ID `1793243013920-0`，原因 `registry_redis_timeout`。
- `Guard.tripUsingDetails` 用 SET NX 保留首次原因，只有新设锁时才写 `event=trip`。audit 当前 11 条，未达到约 1000 条保留上限；最后一条为上述记录，当前 GET trip 同值。
- 11 月 26 日 19:02、19:12、21:17 的 registry 失败证据也早于实际目录站点升级。选取的 trace 显示 EVALSHA 写入后 read 未在约 500ms 内返回；pool_wait 为 0，不能把这些样本解释为连接池等待。
- 目录提交相对生产基线没有修改 `internal/feishusynccontrol`、`internal/feishusafety`、`internal/datasource`、`internal/container`。时间和代码差异均不支持将这次历史停止归因于多级目录改造。

[完整只读投影](production-prep-evidence/test-feishu-forensics.json)；[队列和延迟补充](production-prep-evidence/test-feishu-extra-forensics.json)。所有日期均可由 stream ID 转 UTC 后加 8 小时复核；不把后续 stop 跟随日志当成第一次触发。

## 当前停止范围与健康

| 观察项 | 结果 |
| --- | --- |
| 注册 Worker | 两个，均有有效租约；当前采样剩余约 1.8–1.9 秒，指纹相同 |
| 心跳 | events 持续记录 controller_alive；不是当前 Worker 已丢失的证明 |
| permit / inflight | permit 不存在，inflight=0 |
| 队列 | sync、approved_sync 均由 feishu_safety_stop 暂停；active/pending/retry/scheduled 均为 0 |
| 归档 | sync=9，approved_sync=0；没有删除或重放 |
| 拓扑 | automatic=true，dedicated lane=false，normal_service 不存在 |
| 恢复配置 | 升级前与当前均未配置恢复审批公钥，也未配置在线私钥；legacy 分支按设计不装载 RecoveryManager |
| 影响边界 | marked legacy 飞书出站和这两条同步队列被关闭；普通页面、目录、QA 文档及检索已实际通过，不是全站停机 |

当前 PING 采样约 0.04ms，AOF 和 RDB 最近写入状态正常。历史累计 MISCONF 为 17,043,958,567；两次采样未增长。AOF/fsync 和 Redis latency 历史有延迟峰值，但没有与 10 月 29 日首次锁事件一一对应的磁盘/内核证据。

**确定的直接触发层级是 registry Redis I/O 超时；底层偶发延迟根因尚未完成归因。** 当前低延迟不能反证历史超时；累计 MISCONF 或 delayed_fsync 也不能直接证明本次锁的物理根因。没有据此盲目增大时限、关闭持久化保护或放松安全控制。

## 现有正式恢复流程的适用性

入口位于 `/api/v1/data-sources/:id/safety-recovery/`：

| 入口 | 权限和前置条件 | 当前适用性 |
| --- | --- | --- |
| GET plan / status / eligibility | 路由 Admin；handler 额外要求真实系统管理员用户会话、来源归属；API Key 不得充当恢复操作人 | 没有调用；新 QA 用户不提升权限 |
| POST prepare | 除上述条件外，要求 dedicated lane、精确 runtime SHA/incident/trip、无在途执行/发布、停止队列 CAS | 当前 dedicated=false，不能直接套用 |
| POST execute，operation=reset/canary | 独立签名审批绑定 actor、来源事实、版本、incident、membership、队列摘要和 15 分钟计划；reset 和 canary 分开 | Plan 本身要求恰好一个当前 Worker，当前为两个；legacy 队列 CAS 还要求 archived=0，当前为 9。因此不能直接 execute |
| normal-service / continuity / steady | 各自计划、签名、模式和边界校验；不能视作通用解锁按钮 | 不得移用生产正常服务配置或生产 Canary 来凑前置条件 |

代码依据：`internal/handler/feishu_sync_recovery.go:recoveryActor`、`internal/feishusynccontrol/recovery.go:Plan/Reset`、`recovery_prepare.go:Prepare`。这里只读检查了代码与状态，未假冒管理员调用写入口。

另外，`configureFeishuRecovery` 在此 legacy 模式且公钥配置缺失时保留 nil manager；有权主体进入后续处理器会按代码返回 `recovery_not_configured`（503）。这是配置和代码推断，没有以管理员身份实调。即使补足公钥，双 Worker 与归档队列的前置条件仍不能绕过。[前后配置只读回执](production-prep-evidence/recovery-configuration.json)。

## 独立处理建议（未执行，需单独审查）

1. 保留当前 trip、9 条归档及队列/来源/授权摘要。调查首次触发附近已有 Redis、文件系统和内核历史日志；没有确定证据不调整保护阈值。
2. 对测试站点作独立恢复设计评审：原来就未配置的审批公钥，以及标准流程的单 Worker、专用队列边界，与现有双 Worker legacy 拓扑不一致；不能靠直接删除注册记录、清归档或复用过期签名绕过。任何经批准的公钥配置、拓扑迁移或恢复代码适配应另行提交并验证精确 CAS、租约和归档保全。
3. 如批准恢复，只使用测试站点原有且仍有效的来源授权，走其适用的正式计划/签名/执行入口。不得复制生产 test03，不要求为目录验收重新授权，也不重放归档任务或伪造一轮成功。
4. 风险包括暂停范围变化、重复读取、发布代际或游标错误、双 Worker 竞争及原授权已失效。必须先完成上述前置约束审查；目录生产发布不自动授权这些恢复操作。

生产另行只读观察到独立 namespace `feishu-prod-080f7bb6a01f`，dedicated=true、scope=all、没有 trip，采样 permit TTL 为正、Worker 租约有效。生产的旧 normal_service 记录 active=false 不等于服务不可用；当前自动服务模式不应被擅自改回旧 signed-recovery 模式。此观察只证明控制状态，**不宣称全部来源在线同步成功**。
