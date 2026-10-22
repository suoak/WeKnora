"""Build a deidentified, unapproved review worksheet from read-only aggregates."""
import json
from pathlib import Path

ROOT=Path(__file__).resolve().parents[4]
OUT=ROOT/'docs/integration/knowhub-mcp-portal/evidence/batch7'
source=json.loads((OUT/'production-inventory-readonly.json').read_text(encoding='utf8'))
assert source['read_only'] and not source['production_changes'] and source['ssh_exit_code']==0
rows=[]
for item in source['first_confirmation_inventory']:
    rows.append(dict(item,portal_published=True,first_confirmation='PENDING_REVIEW',public_mcp_eligible=False,
                     source_acl='NOT_VERIFIED',confirmed_sensitive='NOT_ASSESSED',
                     large_kb_review_required=item['documents']>256,
                     owner_confirmation='NEEDS_BUSINESS_OWNER',publication_reviewer='NEEDS_AUTHORITY_MAPPING',
                     technical_blockers=['PRODUCTION_3033_WITHOUT_POLICY_TABLES','FORMAL_EMPLOYEE_PROVIDER_UNBOUND']+
                        (['EMPTY_KB'] if item['documents']==0 else [])+
                        (['DOCUMENTS_NOT_ALL_READY'] if item['ready_documents']<item['documents'] else [])+
                        (['PRODUCTION_LARGE_KB_PERFORMANCE_NOT_CERTIFIED'] if item['documents']>256 else [])))
assert len(rows)==48 and len({r['space_ref'] for r in rows})==17 and sum(r['documents']for r in rows)==2718
result={'source_observed_utc':source['observed_utc'],'read_only':True,'approval_performed':False,'counts':{
    'portal_published_spaces':17,'portal_published_kbs':48,'completed_first_confirmation_kbs':0,'public_mcp_eligible_kbs':0,
    'unverified_source_acl_kbs':48,'external_or_unknown_channel_signal_kbs':sum(any(k not in ('web','api')for k in r['source_channels'])for r in rows),
    'confirmed_sensitive_or_mixed_kbs':'NOT_ASSESSED','large_kb_production_validation_pending':4,
    'empty_kbs':sum(r['documents']==0 for r in rows),'other_production_blocked_kbs':48},
    'categories_overlap':True,'review_items':rows}
(OUT/'first-confirmation-worksheet.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf8')
header='| 空间脱敏标识 | KB 脱敏标识 | Knowledge | 已就绪 | 来源渠道统计 | 大 KB 复核 | 首次确认 |\n| --- | --- | ---: | ---: | --- | --- | --- |\n'
table=''.join('| '+r['space_ref']+' | '+r['kb_ref']+' | '+str(r['documents'])+' | '+str(r['ready_documents'])+' | '+json.dumps(r['source_channels'],ensure_ascii=False)+' | '+('是'if r['large_kb_review_required']else '否')+' | 待负责人及正式审核人确认 |\n'for r in rows)
(OUT/'first-confirmation-worksheet.md').write_text('# 存量首次确认待审清单（未批准）\n\n仅含脱敏标识与统计，不含标题、正文、来源路径、用户或凭据。不得按本清单自动执行公开写入。\n\n'+header+table,encoding='utf8')
print(json.dumps(result['counts'],ensure_ascii=False))
