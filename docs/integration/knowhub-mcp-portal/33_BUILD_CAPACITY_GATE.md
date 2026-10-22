# 第五批容量门禁

本批使用 RC3 独立 worktree，基线 `e62b17e2d305358467821aeff1cce63ce1d61254`。只访问隔离打包测试机，未连接生产执行写操作。本批没有删除任何历史文件、镜像、容器、卷、缓存或备份。

## 实测与执行选择

首次根盘可用 12,922,834,944 bytes，低于建议的 30 GiB 重型构建空间；数据盘 `/mnt/sata0` 可用 1,656,281,227,264 bytes，Docker Root Dir 已在 `/mnt/sata0/lib/docker`。普通 SSH 用户不能直接在数据盘根目录创建目录。

因此创建本批专用、带 batch 标签的 Docker named volume：`knowhub-mcp-b5-20261021-168402-data`，挂载到容器 `/work`。源快照、复制的 Go build cache、Go 临时目录、测试二进制及 SQLite 测试数据库均放在此卷/数据盘 Docker 文件系统。原 Go cache 只读复制，原路径保留；module cache 只读挂载。没有移动 DockerRoot、系统临时目录或既有缓存，没有变更服务拓扑。

用户通知根盘空间足够后重新只读检查：根盘可用 **44,132,802,560 bytes（约 41.1 GiB）**、空闲 inode 30,758,376；数据盘可用 **1,644,895,858,688 bytes**、空闲 inode 207,186,881。继续使用隔离卷，以保留候选与证据边界；没有继续清理文件。

原始证据：[首次检查](evidence/batch5/capacity-before.log)、[摘要](evidence/batch5/capacity-summary.log)、[数据盘可写性](evidence/batch5/data-mount-check.log)、[根盘复核](evidence/batch5/capacity-root-recheck.log)。最终环境及采样汇总见 `evidence/batch5/delivery-validation.json`。

最终执行完毕只读检查：根盘可用 **55,952,384,000 bytes（约 52.1 GiB）**，数据盘可用 **1,641,625,022,464 bytes**，本批没有仍在运行的带 batch=5 标签测试容器，专用卷保留。见 [结束检查](evidence/batch5/environment-after.log)。本批全过程共保留 **1,461 个采样**，任务卷观测高水位 **14,981,165,056 bytes（约 13.95 GiB）**；采样内根盘最小余量 18,641,092,608 bytes、数据盘最小余量 1,641,436,585,984 bytes。没有触发容量阻断、采样错误或 ENOSPC。初始重型工作由数据盘承载，根盘最小余量不代表重型构建仅分配该空间。

## 门禁实现

`tools/batch5_linux.py`：每个命令启动前检查根盘至少 8 GiB 保留空间、数据盘至少 40 GiB、两者空闲 inode 至少 100,000。重型工作全部在数据盘；根盘保留额不是把 8 GiB 当作重型构建空间。

运行中约每秒采集文件系统余量，每 15 轮尝试采集任务卷 `du`；任务卷使用超过 30 GiB、空间低于阈值或采样异常时 fail-closed，停止本批当前测试容器并记录 `BLOCKED_CAPACITY`。每个容器有本批唯一名称与标签，网络为 none，仅临时测试容器使用 `--rm`。卷、源候选与日志保留。超时停止本批容器，不触碰既有容器。

日志目录 `evidence/batch5/linux/results-<candidate>-<phase>/` 保存 `capacity.jsonl`、`capacity-final.json`、每个命令的参数、开始/结束时间、退出码及原始输出。采样高水位只是**观测值，不是精确瞬时峰值**。前三批及第四批 ENOSPC 时的精确临时构建峰值没有完整采样，保持 **NOT_MEASURED**；不能从事后 `du` 推算为已测峰值。

## 保留与回滚

本批专用卷和 `/home/aqyfzx/ai/knowhub-mcp-b5-20261021-168402` 均保留，可供复现。源 tar 本地也保留在 `E:/AIData/Codex/knowhub-mcp-b5-20261021-168402/`。本批未执行卷删除或历史清理。将来回收前先核对 manifest、日志落盘和无运行任务，只能针对明确列出的本批资源；本报告不授权删除。
