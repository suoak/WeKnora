"""Actual shared APP denies draft/archived/unproved/ABA/mixed Wiki pages."""
from pathlib import Path
import hashlib,importlib.util,json,sys
base=Path(sys.argv[1]).resolve();h=json.loads((base/'formal-runtime-current.json').read_text());p=Path(h['path']);assert hashlib.sha256(p.read_bytes()).hexdigest()==h['sha256'];s=importlib.util.spec_from_file_location('r',p);m=importlib.util.module_from_spec(s);s.loader.exec_module(m);r=m.Runtime(base)
out=base/'results/wiki-negative';out.mkdir(exist_ok=False);checks={};key=None
try:
 A=r.login('A');key=r.create_key(A,'Formal Wiki negatives');token=key['token']
 for state in ['draft','archived']:
  r.sql("UPDATE wiki_pages SET status='"+state+"'WHERE id='fm-wiki-private-home'")
  assert r.denied(r.call(token,'wiki_read_page',{'slug':'concept/verified','knowledge_base_id':'fm-private-home'}));checks[state+'_denied']='PASS'
 r.sql("UPDATE wiki_pages SET status='published',content='FORMAL_RAW_WRITER_CANARY'WHERE id='fm-wiki-private-home'")
 assert r.denied(r.call(token,'wiki_read_page',{'slug':'concept/verified','knowledge_base_id':'fm-private-home'}));checks['raw_writer_unproved_body_denied']='PASS'
 r.sql("UPDATE wiki_pages SET content='verified private-home Wiki ordinary content'WHERE id='fm-wiki-private-home'")
 assert r.denied(r.call(token,'wiki_read_page',{'slug':'concept/verified','knowledge_base_id':'fm-private-home'}));checks['ABA_does_not_restore_proof']='PASS'
 assert r.denied(r.call(token,'wiki_read_page',{'slug':'concept/unknown','knowledge_base_id':'fm-public-two'}))
 r.sql("UPDATE wiki_pages SET source_refs='[]'WHERE id='fm-wiki-untrusted'")
 assert r.denied(r.call(token,'wiki_read_page',{'slug':'concept/unknown','knowledge_base_id':'fm-public-two'}));checks['mixed_and_missing_sources_denied']='PASS'
 good=r.value(r.call(token,'wiki_read_page',{'slug':'concept/verified','knowledge_base_id':'fm-public-two'}));assert 'ordinary content'in json.dumps(good);checks['healthy_Wiki_still_readable']='PASS'
 result={'exit_code':0,'candidate_sha256':r.sha,'checks':checks,'synthetic_only':True,'modified_only_fm_fixture':True}
except Exception as e:result={'exit_code':1,'error_class':type(e).__name__,'message':str(e),'checks':checks}
finally:
 if key:r.request('DELETE','/api/v1/mcp-api-keys/'+str(key['id']),token=A['token'])
 (out/'http-actions.json').write_text(json.dumps(r.actions,indent=2));(out/'result.json').write_text(json.dumps(result,indent=2))
print(json.dumps(result));sys.exit(result['exit_code'])
