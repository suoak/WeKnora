"""Render safe receipts only; no SSH, credential loading, or authentication."""
import json, pathlib, sys

root=pathlib.Path(__file__).resolve().parents[1]
r=json.loads(pathlib.Path(sys.argv[1]).read_text(encoding='utf-8'))
for name in ('build-status.json','upgrade-result.json','validation-result.json','rollback-result.json','network-result.json','capacity-result.json','final-result.json','image-receipt.json','jit-revoke-result.json'):
 assert r[name]['result']=='PASS',name
c=r['capacity-result.json'];assert len(c['capacity']['scenarios'])==10
assert all(s['functional_pass'] for s in c['capacity']['scenarios'])
assert c['capacity']['mock_counts']=={'authorize':4400,'token':4400,'profile':4300,'reused_code':0,'rate_limited':50,'failed':50,'expired_code':0}
assert c['zero_new_refresh_rows'] and c['rejected_identities_and_sessions_zero']
out=root/'evidence/jit-pilot-final-technical-acceptance-20261016.json'
out.write_text(json.dumps(r,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')

labels={'e300':'原账号首次关联300','e500':'原账号首次关联500','j300':'新员工JIT300','j500':'新员工JIT500','m300':'新旧混合300','m500':'新旧混合500','delay500':'SID延迟500','limited500':'SID限流500','failure500':'SID401失败500','expired500':'集中到期重登500'}
lines=['| 场景 | 成功 | 预期拒绝 | P50 ms | P95 ms | P99 ms | 最大发起延迟 ms |','|---|---:|---:|---:|---:|---:|---:|']
for s in c['capacity']['scenarios']:
 o=s['outcomes'];lines.append(f"| {labels[s['scenario']]} | {o.get('PASS',0)} | {o.get('EXPECTED_REJECT',0)} | {s['p50_ms']} | {s['p95_ms']} | {s['p99_ms']} | {s['max_start_lag_ms']} |")
lines.append('\n最终10场景全部满足成功/预期拒绝断言：4400笔新事务，4300笔成功、100笔预期安全拒绝；Token4400次、UserInfo4300次，重复/过期Code0。500个旧Access到期拒绝，500次重登user_id保持，随后500次退出与旧Access拒绝通过。')
lines.append('\n最终收据：'+c['utc']+'（UTC）；[完整脱敏执行证据](evidence/jit-pilot-final-technical-acceptance-20261016.json)。初次统计口径失败收据也包含在该文件，未删除或改写。')
p=root/'SOURCEID_JIT_CAPACITY_300_500_REPORT.md';s=p.read_text(encoding='utf-8');s=s.replace('<!-- CAPACITY_RESULTS -->','\n'.join(lines))
m=c['monitoring']
resources=f"""共{m['samples']}次采样；本测试库连接峰值{m['max_connections']}、active峰值{m['max_active']}（含监测查询），Lock等待采样峰值{m['max_lock_waiters']}、users锁等待{m['max_users_lock_waiters']}、最大采样锁等待{m['max_lock_wait_ms']}ms。只能说采样未观察到等待，不能证明不存在短暂锁竞争。

APP CPU最大采样{m['max_cpu_percent']}%，PostgreSQL {m['max_pg_cpu_percent']}%；APP内存抽样：{', '.join(m['memory_samples'])}。这些是Docker stats口径，CPU百分比按单核计，APP配额2核；Mock/PG资源不包含在APP限额中。

database/sql客户端池等待计数：**NOT_CAPTURED**。最终合成用户新Refresh为0，JIT成员/管理员权限为0，Token失败对应身份和Session为0；可丢弃库和容器已清理。"""
s=s.replace('<!-- RESOURCE_RESULTS -->',resources);p.write_text(s,encoding='utf-8')
p=root/'SOURCEID_JIT_PILOT_FINAL_TECHNICAL_ACCEPTANCE_REPORT.md';s=p.read_text(encoding='utf-8')
s=s.replace('本节将在本轮执行结束后填入最终脱敏收据及容量结果；在此之前不将进行中的测试标为PASS。', '''最终结论：**独立技术验收PASS；生产及真实业务放行NO-GO，等待独立授权与必需输入。**

- 构建/003029/健康/原数据完整性/旧09 APP兼容及实际回滚恢复：PASS。
- pilot与enterprise_all原账号Mock、JIT首次/再次开户及定向停用撤销：PASS。JIT用户没有合法成员时SwitchTenant为403；合法切换和不延长绝对期限由原账号用例验证，不能把JIT的拒绝当作成功切换。
- 300/500容量复测10场景：4300成功、100预期拒绝，无Code重复兑换，新增Refresh0；第一次全表Refresh断言失败已保留并解释。生产容量不作PASS声明。
- 最终DB3029 clean，原用户/角色/ACL/文件/2717条有效分块和向量引用保持；真实身份/企业Session/JIT记录0，业务门禁关闭。默认80/8080容器及DB3027、原库79保持。

[完整执行收据与首次失败记录](evidence/jit-pilot-final-technical-acceptance-20261016.json) · [300/500容量明细](SOURCEID_JIT_CAPACITY_300_500_REPORT.md) · [真实业务操作卡](SOURCEID_JIT_REAL_BUSINESS_ACCEPTANCE_OPERATION_CARD.md) · [GO/NO-GO清单](SOURCEID_ENTERPRISE_RELEASE_GO_NO_GO.md)

无凭据桌面/移动页面证据：[桌面](evidence/jit-release-login-desktop-20261016.png)、[移动](evidence/jit-release-login-mobile-20261016.png)、[四类请求路由](evidence/jit-release-browser-route-20261016.json)。实际有SSO的Chrome尚未验证，不要求用户提供Cookie、Token或HAR。''')
p.write_text(s,encoding='utf-8')
print(json.dumps({'result':'PASS','safe_receipts_saved':True,'scenarios':10,'real_sid_login':False,'production_release':'NO_GO'}))
