# 第五批 Windows Python 完整回归

## 结论边界

Windows 原完整命令由 55 tests / 3 errors 改善为 **55 tests / 1 error，exit 1，仍为 FAIL**。剩余错误为真实 `os.symlink` 的 **WinError 1314**，操作系统未授予当前进程创建符号链接的权限，记为 **BLOCKED_OS_PRIVILEGE**。没有跳过测试、模拟 symlink、删断言、开启系统权限或改注册表。

受控 Linux Python 3.11.16、MCP SDK 2.2.0 已完整通过相同 55 项测试，包括真实 symlink 及越界文件拒绝。Linux PASS 不覆盖 Windows FAIL。最终候选 Linux 重跑记录见 [38](38_FIFTH_BATCH_DELIVERY.md)。

## 原命令与修复

运行时：`E:/AIData/CodexTemp/knowhub25-personal-f1-20261010/venv/Scripts/python.exe`，Python 3.12.11、SDK 2.3.0。设置进程环境 `PYTHONUTF8=1`、`WEKNORA_API_KEY=`，在当前独立 worktree 执行：

```powershell
python -m unittest discover -s mcp-server -p 'test_*.py' -v
```

`mcp-server/test_file_path_security.py::working_directory` 使用 `try/finally` 恢复 cwd，并将其作为 `TemporaryDirectory` 后进入、先退出的 context manager。即使测试断言或 `os.symlink` 抛错，也会在清理临时目录前离开该目录。

原问题是 cwd 仍处于待删除目录，Windows 清理报 WinError 32/5，不能把它解释成授权断言失败。原三个 ERROR 分别为：

| 用例 | 原根因 | 修复后 |
| --- | --- | --- |
| `test_active_transport_without_env_blocks_path_outside_cwd` | 离开 cwd 前清理 TemporaryDirectory，WinError 32/5 | PASS，原路径拒绝断言保留 |
| `test_network_transport_blocks_path_outside_cwd` | 同上 | PASS，原网络上传路径限制保留 |
| `test_stdio_rejects_outside_files_and_symlinks` | 真实 symlink WinError 1314，随后还触发 cwd 清理错误链 | 只剩 WinError 1314；BLOCKED_OS_PRIVILEGE |

stdio 的外部绝对路径拒绝断言移到真实 symlink 创建前执行，原有两项安全断言均保留；无 symlink 权限时仍明确报错，不伪装 PASS。

证据：[修复前原输出](evidence/batch5/windows-before.log)、[exit 1](evidence/batch5/windows-before.exit.txt)、[修复后原输出](evidence/batch5/windows-after.log)、[exit 1](evidence/batch5/windows-after.exit.txt)。这些记录及第四批 Windows 原 FAIL 全部保留。

## 上线影响与后续

本项只修改候选测试的 cwd 生命周期，没有改生产 Python MCP。要关闭 Windows 门禁，需在已具备 symlink 权限的受控 Windows 环境原样运行完整 55 项；本批 Linux 能力证明不能充当这一 Windows 结果。
