# 飞书 P0 出站保护与自动停止实施前方案

基线是 PR35 已验收候选 70c7d5676；在后续分支开发，不修改 PR35。P1 独立分支，不合并到此分支。不重新调查生产，不调用生产模型或飞书接口。

审计确定：后台模型 governor 是 fail-open，且在线 query 不受其限制；不能改该通用行为来限制飞书。直接 staging 的 BatchEmbed、protocol 子批及重试都必须在真实 HTTP transport 处统一计费。飞书源端 HTTP 也需要独立紧急门禁。SSRF transport 保留。仅 ProcessSync 中明确标记的旧飞书后台上下文启用本方案，未标记在线调用继续原路径。

采用现有 Redis，不新增常驻服务。飞书专用 guard 用 Redis TIME 和 Lua 原子共享 in-flight、滚动60秒 RPM/TPM；每实际请求预留保守 token 上界，失败不退 RPM/TPM。缺配额或 tokenizer 上界契约时不给许可，不能把本地预算说成供应商额度。in-flight 不自动过期回收，崩溃后必须证明远端排空才处理；网络取消不能证明供应商计算已结束，网关硬限额仍是发布外部条件。

APP 内控制器只有在完整预期 Worker 清单、唯一进程身份、scope/源码/预算指纹一致、各 Worker 心跳及外部监测样本新鲜、无异常时才续发短期出站许可。控制器停止/Redis断联/指标缺失，许可失效，transport拒绝新请求并取消在途请求。配置不完整只关闭飞书同步，不拖垮在线问答。配置不一致、429/配额错误、5xx/超时及监测异常持久触发 trip，不自动重启或清锁。

停止闭环：guard本地关闭并持久化 trip → 暂停全局 sync（不暂停 low、不删除任务）→ 取消本进程活动同步上下文 → DB 按 datasource→synclog 锁序取消 running、generation+1、claim=false → 结构化日志及 Redis 审计。DB不可用时取消网络、保持冻结，不能报告fencing已完成。其他实例也必须检测trip并取消各自执行；无Redis时仍本地取消、数据库fence并报警，Redis恢复后再次落实暂停。scope=none 是准入而非排空，所有源状态、cron、cursor、知识、Chunk/向量和权限不因停止操作改写。

启用为显式部署配置；默认保持已验收 P0 路径兼容。发布核验必须要求启用且实际心跳能力齐全，不能把未启用看成GO。所有 Worker 声明应由运维CMDB核实，代码不能发现未登记的其他Redis集群或旧版本；旧版绕过本保护的风险必须由排空旧部署、配置清单及外部网关隔离解决。

隔离验证使用临时 Redis7 + ParadeDB/PostgreSQL、合成源及本地 HTTP fixture；覆盖双 Worker 竞争、坏配置、滚动预算、指标/许可失效、Redis断联、请求中trip、故障响应、在线路径不受影响、旧排队任务取消、DB fence 拒绝旧代写入，以及APP启动scope=none。发布包和生产接口不在本轮执行范围。

## 网关限额确认后的实施细化

uniapi 管理员确认 text-embedding-v4 无独立频率/并发限制；上游百炼北京/香港 1800 RPM、120 万 TPM，新加坡 1800 RPM、100 万 TPM。[官方说明](https://help.aliyun.com/zh/model-studio/rate-limit)按主账号和模型汇总 RAM、空间及 API Key。生产区域、共享账号范围、当前占用/余量及批准的飞书 TPM 仍 UNKNOWN，不把区域标称额度当作飞书可分配额度。

首轮配置要求 in-flight=1、RPS=1、RPM 不超过 60、每请求最多 20 条；TPM 由负责人批准后显式填写，没有自动额度默认值。Token 预算按实际输入 UTF-8 字节数加逐条 overhead 预留，必须确认其对选用 tokenizer 的保守上界契约；未确认不给许可。该数不是上游实测 Token，快照明确将上游计量标记 UNKNOWN。

复用原有 protocol 及模型调用路径，在 transport 处逐个 wire attempt 共享预算。Embedding 429 最多三次请求，1s/2s 指数退避加抖动，遵守 Retry-After 秒数或 HTTP 日期，总等待最多 30 秒；无效或超出等待边界时锁存停止。每次重试重新核验门禁及预算。其他非成功 HTTP、超时、响应异常立即停止。预算繁忙等待最多显式配置的 65 秒，每 100ms 检查许可；Redis 或状态不可确认时不放行。

控制器使用 2 秒 Worker 租约、750ms 出站许可及最多 2 秒监控样本；双实例必须同 scope、版本、预算和清单。独立 5 秒停止上下文确保指标超时不会取消 DB fencing。仅扫描仍有 running 执行的旧飞书源，不改变业务配置。scope=none 启动拒绝旧投递，暂停 sync、不暂停 low，不删任务。

新增 FEISHU_SYNC_SAFETY_ALERT_URL，要求 HTTPS 接收器。结构化停止事件必须收到 2xx 回执；正常状态每秒探测告警接收链路，失联撤销许可并锁存。默认客户端保留 SSRF 保护。隔离使用真实 TLS 接收器，生产路由和 ACL 尚未接线验收，不能据此宣称生产告警已生效。

stop-only CLI 为 cmd/feishu-sync-control，默认 status；stop -execute 执行独立出站锁存和暂停 sync，不恢复消费、不清锁、不删除任务。它不代替数据库 fencing 回执；生产操作仍需审批。没有旧实例清单或远端排空证明时不回收遗留 in-flight。
