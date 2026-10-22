> **Superseded policy:** First-approval and real-time employment requirements in this historical report have been replaced. See [55: final dynamic Portal policy](55_DYNAMIC_PORTAL_PUBLIC_REQUIREMENTS.md). Historical test passes do not validate the new candidate.

# 第七批：生产脱敏首次确认清单

2026-10-21 的只读核查执行 `BEGIN READ ONLY`、15 秒 statement timeout、查询后 rollback，只导出脱敏标识、计数、渠道分组、配置维度及索引维度。未取正文、标题、文件链接、用户、模型凭据或数据库密码；未发起生产登录/鉴权/模型请求。

生产保持 schema **3033 / dirty=false**，四个分类/epoch 表均不存在。未执行迁移、public 回填或其他写入。两次早期只读脚本查询失败（聚合 SQL 语法）保留在证据目录，最后结果 exit 0。

| 分类 | 数量/结论 |
| --- | --- |
| Portal Published 且 active 的空间 | 17 |
| 对应有效 KB / Knowledge | 48 / 2718 |
| 已完成可核验首次确认 | 0 KB |
| 当前可进入生产公共 MCP | 0 KB |
| 缺少可核验来源/公开分类依据 | 48 KB，待审核 |
| 外部或未知渠道线索 | 12 KB；是线索，不等于敏感结论 |
| 已确认敏感/混合 | NOT_ASSESSED，未检查正文，不能伪报为零敏感 |
| 超原 256 上限的 KB | 4，最大 864；生产安全/性能仍待验证 |
| 空 KB | 5 |
| 未就绪 Knowledge | 5 |
| 其他生产阻塞 | 48 KB 均受正式 Provider、角色映射及 3033 未迁移影响 |

上述分类互相重叠，不能相加得 KB 总数。普通 web/api 渠道也不是安全公开批准或当前来源 ACL 的证据。

逐项待审表：[48 个 KB 脱敏确认清单](evidence/batch7/first-confirmation-worksheet.md)，机器可读版：[JSON](evidence/batch7/first-confirmation-worksheet.json)，原只读结果：[production-inventory-readonly.json](evidence/batch7/production-inventory-readonly.json)。业务负责人需在受控生产管理端根据真实资源映射核对脱敏项；报告不含可反查的正文或责任人个人信息。

## 试点及待审批步骤

1. 选少量 Published 空间、资料来源明确且整体普通的 KB；由真实内容负责人确认范围、风险和理由。未知来源或混合资料不纳入首批。
2. 确定正式 Portal 内容发布审核职责，与内容负责人分离；在审批系统记录真实对象、版本和审计引用。不能把本报告的“推荐”当批准。
3. 用等价脱敏/合成数据通过隔离环境权限、撤权、客户端及规模门禁；大 KB 补充真实模型和多 Chunk 性能。
4. 准备独立审批的生产灰度变更单。批准前保持所有生产能力关闭；禁止把 48 行清单转换为批量 public SQL。

该清单由 `tools/batch7_review_worksheet.py` 从聚合结果可重复生成，未发生业务确认。未经人工确认的历史资源继续 `pending_review`。
