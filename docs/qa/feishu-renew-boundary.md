# 飞书自动服务续租的读取边界回归

## 问题与改动

控制器在 Step 开头读取真实 Consumer，并维护既有连续稳定窗口。原实现随后执行 SQL receipt、Canary 对账及审计，最后用早先的原始字节摘要和 root 快照续租。期间正常 Asynq 心跳的 protobuf 编码变化或合法来源登记都可能使快照过时，最终 CAS 正确拒绝，但错误被合并成 `runtime_or_queue_changed`。这既会撤回 permit，也无法区分正常读取竞争、真实故障及不确定状态。该源码窗口不等于已经确定某次历史事故的唯一原因。

修复保留 Step 开头的完整稳定性判定；在所有 SQL、Canary 对账和审计之后，重新通过 `current()` 验证 SQL receipt／witness 并读取 root，再进行一次完整健康读取。前后 Worker、incarnation、membership revision 和唯一 Consumer Server ID 必须一致，随后立即执行原子续租。没有增加重试、等待、超时或新的审批入口。

最终 CAS 仍核对实际租约、注册表期限、元数据、队列暂停标记、root、trip 及 phase。最终读取之后再次变化仍然拒绝，不能保证消除所有并发冲突。缺失、过期、身份漂移、人工停止、Redis／SQL 不可判定及审计失败仍失败关闭。不会覆盖并发登记的 Canary 来源、自动清除其他命名空间或恢复旧签名授权。

诊断仅输出白名单类别和固定阶段：区分元数据读取竞争、真实缺失／过期、root 竞争、身份、租约、队列暂停，以及 Canary 的 SQL、取消、证据和解码类别。未知错误只记录固定通用类别，不输出原始 Redis 数据、SQL 参数、业务内容或凭据。同一失败去重保留；不同阶段／类别不会被同一通用原因吞掉。

另外，原 Canary 对账在读取运行状态前先等待 source 行锁，而真实发布事务也通过此锁保护 Chunk、向量及索引写入。观察仍在执行的任务可能因此阻塞控制器自身续租。现在先在同一事务内验证 SQL epoch，再无锁读取精确 run/source 及未删除来源；只有原有运行中判定为 pending 时返回，不签收、不创建 witness、不升 phase。潜在终态、来源缺失或查询错误仍进入原完整验证或失败关闭。运行状态与 300000ms 超时判断由同一个函数复用，没有改变判定规则、发布锁或计数。

## 验证与证据边界

- 基线分支只在已部署源码上增加 SQL 读取边界回归用例；基线失败必须以实际 Linux 运行和具体断言为证，不能从源码推断成已复现。
- 两个正例分别在 SQL 查询与最终 CAS 之间注入真实 SDK 元数据的语义等价编码，以及经过实际 SQL claim、权限和登记 Lua 的合法来源绑定。正例只有一次 Step，不以循环、放宽超时或跳过校验获取成功。
- 元数据编码 fixture 明确属于独立 Redis 内的受控写入；另外使用实际 Asynq 队列长任务与自主心跳验证持续执行，不将 fixture 冒充自主心跳或远端飞书授权。
- 覆盖末端健康检查前的 membership、incarnation 和真实 Consumer 更换；覆盖最终 CAS 前的元数据变化／缺失、root 阻断、membership、租约、人工队列暂停和取消。所有负例继续拒绝，不覆盖新状态或删除人工暂停。
- Windows 缺少隔离 SQL／Redis时的 SKIP 只表示编译或纯函数验证，不能记为 SQL／Asynq 验收通过。专用 `feishu-automatic-service.yml` 才启用实际原生 Redis 和迁移后的 PostgreSQL fixture；通用 diagnostics workflow 不能替代。
- 远端 OAuth、复制／导出权限及实际发布仍须通过合法账户、正常业务 API 和真实文档核验。CI 合成文档／向量不能证明生产来源恢复。
- 来源锁探索用例在早期补丁上实际失败，不能仅靠刷新元数据解决。回归既使用独立 SQL 连接的真实 source 行锁，也在真实 `FeishuLegacyStore.Publish` 取得原执行栅栏后设置有界测试屏障，要求实际 Canary poll 推进但保持 pending；随后继续完成原文、Chunk、向量、索引、receipt、签收、ramp 和 normal 的完整正例。未知 SQL 查询失败和已删除来源另有负例。

## 开销、发布和回滚

每个正常 Step 增加一次有界健康采样和一次当前 SQL receipt／witness 验证；不增加远端模型或飞书调用，不改变任务并发预算。连续稳定窗口和最终 CAS 仍使用原有时限，性能需用实际 Linux／完整 APP 证据核验。

发布前须绑定最终源码、原生镜像和完整 APP 验收。本次不含 Schema 迁移。按切换前实际 APP 镜像准备专用回滚，不能复用更早版本脚本；Frontend、数据库、对象存储和业务数据不随版本回退。保留所有既有 trip 审计、不可变 SQL epoch、system journal 和执行栅栏，不恢复旧授权，不重放归档任务。来源失败及新积压继续如实登记，通过新的合法增量补齐已经证明缺失的内容。
