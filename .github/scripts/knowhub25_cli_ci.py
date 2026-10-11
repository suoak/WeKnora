"""Bounded B-contract CI; raw output never published."""
import hashlib
import json
import os
import re
import subprocess
import sys
from pathlib import Path

PACKAGE_ID = os.environ.get('KNOWHUB25_CLI_PACKAGE', 'CLI_001')
PACKAGE = {'CLI_001':'internal/format','CLI_002':'internal/cmdutil','CLI_003':'cmd/auth'}[PACKAGE_ID]
BASELINE = 'a6403708915680ccad310b0fd64506597b879804'
CANARIES = json.loads((Path(__file__).with_name('knowhub25_cli_canaries.json')).read_text())
RULES = [(f'SYNTHETIC_{i + 1:03}', re.compile(re.escape(v))) for i, v in enumerate(CANARIES)]
RULES += [
    ('BEARER_PATTERN', re.compile(r'(?i)bearer[ \t]+(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}')),
    ('MCP_KEY_PATTERN', re.compile(r'(?<![A-Za-z0-9_])mcp_[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])')),
    ('CREDENTIAL_ASSIGNMENT', re.compile(r'''(?i)[\"']?(?:authorization|cookie|api_key|secret|token)[\"']?\s*[:=]\s*[\"']?(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}''')),
]


def parse(data, exit_code, required=(), allowed_skip=False):
    states = {}
    outputs = {}
    package_status = []
    for line in data.decode('utf-8', errors='strict').splitlines():
        event = json.loads(line)
        name, action = event.get('Test'), event.get('Action')
        if name and action == 'output':
            outputs[name] = outputs.get(name, '') + event.get('Output', '')
        if name and action in {'run', 'pass', 'fail', 'skip'}:
            states.setdefault(name, []).append(action)
        elif not name and action in {'pass', 'fail'}:
            package_status.append(action)
    valid = exit_code == 0 and package_status == ['pass'] and bool(states)
    counts = dict(run=0, passed=0, failed=0, skipped=0)
    for name, seq in states.items():
        permitted = allowed_skip and name == 'TestSaveWorksWhenTheFTS5ModuleIsNotCompiledIn' and 'fts5 is compiled in; the keyword index is checked by the other tests' in outputs.get(name, '') and name not in required
        valid &= len(seq) == 2 and seq[0] == 'run' and (seq[1] in {'pass', 'fail'} or (seq[1] == 'skip' and permitted))
        counts['run'] += seq.count('run')
        counts['passed'] += seq.count('pass')
        counts['failed'] += seq.count('fail')
        counts['skipped'] += seq.count('skip')
    valid &= counts['failed'] == 0 and all(states.get(name) == ['run', 'pass'] for name in required)
    return valid, counts


def self_test():
    good = b'{"Test":"fixed","Action":"run"}\n{"Test":"fixed","Action":"pass"}\n{"Action":"pass"}\n'
    assert parse(good, 0)[0]
    assert not parse(good, 1)[0]
    assert not parse(good.replace(b'"Test":"fixed","Action":"pass"', b'"Test":"fixed","Action":"fail"'), 0)[0]
    assert not parse(good.replace(b'"pass"', b'"skip"', 1), 0)[0]
    assert not parse(good.splitlines()[0], 0)[0]
    assert not parse(good + good, 0)[0]
    try:
        parse(good + b'broken', 0)
    except (ValueError, UnicodeError):
        pass
    else:
        raise AssertionError('Corrupt JSON accepted')
    assert any(p.search('token=SYNTHETIC_CREDENTIAL_VALUE') for _, p in RULES)
    name = 'TestSaveWorksWhenTheFTS5ModuleIsNotCompiledIn'
    skip = '\n'.join(json.dumps(event) for event in [
        {'Test': name, 'Action': 'run'},
        {'Test': name, 'Action': 'output', 'Output': 'fts5 is compiled in; the keyword index is checked by the other tests'},
        {'Test': name, 'Action': 'skip'}, {'Action': 'pass'}]).encode()
    assert parse(skip, 0, allowed_skip=True)[0]
    assert not parse(skip, 0)[0]
    assert not parse(skip, 0, [name], True)[0]
    assert not parse(skip.replace(b'fts5 is compiled in', b'wrong reason'), 0, allowed_skip=True)[0]


SOURCE_BLOBS={'cli/internal/format/filter.go': 'fc0846f7fb8126e8d5cb1f47cbcb8b2950b8bb4e', 'cli/internal/format/jq_precision_test.go': '9009fe2db00a00318f4ae04dfe88731058e50917', 'cli/internal/cmdutil/errors.go': '1e10c03fe9f68db50d88a8ccbf1fc11b8a4a53ef', 'cli/internal/cmdutil/authrefresh_http_classification_test.go': 'a742dae6be46e1601d27ef635c0c6d8d2575eb20', 'cli/cmd/auth/status_jq_precision_test.go': '4f164805457df9e09316c539af7863134ff7f669'}

REQUIRED={'CLI_001': ['TestJQIntegerPrecisionAndInvalidTrailingData'], 'CLI_002': ['TestRejectedAutomaticRefreshPreservesHTTPClassification', 'TestRejectedAutomaticRefreshPreservesHTTPClassification/Unauthorized', 'TestRejectedAutomaticRefreshPreservesHTTPClassification/Forbidden', 'TestClassifyWrappedTypedHTTPStatusIgnoresBodyStatus'], 'CLI_003': ['TestStatusJQPreservesTenantID']}[PACKAGE_ID]

def review(value):
 if set(value)!={'schema','candidate','baseline','packageId','status','rows'} or value['schema']!=1 or value['packageId'] not in {'CLI_001','CLI_002','CLI_003'} or value['baseline']!=BASELINE or value['status'] not in {'PASS','BLOCKED'} or not re.fullmatch('[0-9a-f]{40}',value['candidate']):raise ValueError('SCHEMA_FAILED')
 for row in value['rows']:
  if set(row)!={'side','mode','compileExit','testExit','counts','parserValid','dataRace','notPresentCount','rawFiles'} or row['side'] not in {'BASELINE','CANDIDATE'} or row['mode'] not in {'NORMAL','RACE'}:raise ValueError('SCHEMA_FAILED')
  if any(row[k] is not None and type(row[k]) is not int for k in ['compileExit','testExit','notPresentCount']) or type(row['parserValid']) is not bool or type(row['dataRace']) is not bool:raise ValueError('SCHEMA_FAILED')
  if row['counts'] is not None and (set(row['counts'])!={'run','passed','failed','skipped'} or any(type(x) is not int or x<0 for x in row['counts'].values())):raise ValueError('SCHEMA_FAILED')
  for item in row['rawFiles']:
   if set(item)!={'role','sha256','findings'} or item['role'] not in {'GO_COMPILE_LOG','GO_TEST_JSONL','GO_TEST_STDERR'} or not re.fullmatch('[0-9a-f]{64}',item['sha256']):raise ValueError('SCHEMA_FAILED')
   for hit in item['findings']:
    if set(hit)!={'ruleId','count'} or hit['ruleId'] not in {n for n,_ in RULES} or type(hit['count']) is not int or hit['count']<=0:raise ValueError('SCHEMA_FAILED')
 if value['status']=='PASS' and (len(value['rows'])!=4 or {(r['side'],r['mode']) for r in value['rows']}!={(s,m) for s in ['BASELINE','CANDIDATE'] for m in ['NORMAL','RACE']} or any(not r['parserValid'] or r['compileExit']!=0 or r['testExit']!=0 or r['dataRace'] or not r['rawFiles'] or any(f['findings'] for f in r['rawFiles']) for r in value['rows'])):raise ValueError('SCHEMA_FAILED')
 return value

def main():
 self_test()
 if '--self-test' in sys.argv:
  try:review(dict(schema=1,packageId=PACKAGE_ID,candidate='a'*40,baseline=BASELINE,status='PASS',rows=[]))
  except ValueError:pass
  else:raise ValueError('SCHEMA_FAILED')
  print('CLIENT_PARSER_SCAN_AND_SCHEMA_SELF_TEST_PASS');return
 safe=Path(os.environ['RUNNER_TEMP'])/'cli-safe-summary';safe.mkdir(mode=0o700,exist_ok=True)
 if '--review' in sys.argv:review(json.loads((safe/'summary.json').read_text()));return
 head=subprocess.check_output(['git','rev-parse','HEAD']).decode().strip();assert head==os.environ['GITHUB_SHA']
 assert subprocess.check_output(['git','merge-base',BASELINE,'HEAD']).decode().strip()==BASELINE
 approved=set(SOURCE_BLOBS)|{'.github/scripts/knowhub25_cli_ci.py','.github/scripts/knowhub25_cli_canaries.json','.github/workflows/knowhub25-cli-contracts.yml'}
 assert set(subprocess.check_output(['git','diff','--name-only',BASELINE,'HEAD']).decode().splitlines())==approved
 for path,blob in SOURCE_BLOBS.items():assert subprocess.check_output(['git','rev-parse','HEAD:'+path]).decode().strip()==blob
 assert subprocess.check_output(['go','version']).decode().strip()=='go version go1.26.7 linux/amd64'
 raw=Path(os.environ['RUNNER_TEMP'])/'cli-private';raw.mkdir(mode=0o700)
 rows=[];okay=True
 for side,source in [('BASELINE',Path('../baseline').resolve()),('CANDIDATE',Path('.').resolve())]:
  assert subprocess.check_output(['git','-C',str(source),'rev-parse','HEAD']).decode().strip()==(BASELINE if side=='BASELINE' else head)
  for dep in ['cli/go.mod','cli/go.sum','client/go.mod','client/go.sum']:
   assert subprocess.check_output(['git','show',BASELINE+':'+dep])==subprocess.check_output(['git','-C',str(source),'show','HEAD:'+dep])
  definitions={name for path in (source/'cli'/PACKAGE).glob('*_test.go') for name in re.findall(r'^func (Test[A-Za-z0-9_]+)\(',path.read_text(),re.MULTILINE)}
  missing=sum(name.split('/')[0] not in definitions for name in REQUIRED)
  for mode in ['NORMAL','RACE']:
   files=[];cc=0
   if mode=='NORMAL':
    path=raw/(side+'-compile.log')
    with path.open('wb') as stream:cc=subprocess.run(['go','test','-mod=readonly','-p','2','-c','-o',str(raw/(side+'.test')),'./'+PACKAGE],cwd=source/'cli',stdout=stream,stderr=subprocess.STDOUT).returncode
    files.append((path,'GO_COMPILE_LOG'))
   path=raw/(side+'-'+mode+'.jsonl');errpath=raw/(side+'-'+mode+'.stderr')
   if cc==0:
    command=['go','test','-mod=readonly','-p','2','-count=1','-json']+(['-race'] if mode=='RACE' else [])+['./'+PACKAGE]
    with path.open('wb') as out,errpath.open('wb') as err:code=subprocess.run(command,cwd=source/'cli',stdout=out,stderr=err).returncode
    files.extend([(path,'GO_TEST_JSONL'),(errpath,'GO_TEST_STDERR')])
    valid,counts=parse(path.read_bytes(),code,REQUIRED if side=='CANDIDATE' else [])
    race=b'WARNING: DATA RACE' in path.read_bytes()+errpath.read_bytes()
   else:code=None;valid=False;counts=None;race=False
   metadata=[]
   for p,role in files:
    data=p.read_bytes();text=data.decode('utf-8','strict')
    metadata.append(dict(role=role,sha256=hashlib.sha256(data).hexdigest(),findings=[dict(ruleId=n,count=len(r.findall(text))) for n,r in RULES if r.search(text)]))
   okay &= valid and not race and not any(f['findings'] for f in metadata)
   rows.append(dict(side=side,mode=mode,compileExit=cc,testExit=code,counts=counts,parserValid=valid,dataRace=race,notPresentCount=missing,rawFiles=metadata))
 value=review(dict(schema=1,packageId=PACKAGE_ID,candidate=head,baseline=BASELINE,status='PASS' if okay else 'BLOCKED',rows=rows))
 (safe/'summary.json').write_text(json.dumps(value,indent=2)+'\n');sys.exit(0 if okay else 1)

if __name__=='__main__':
 os.umask(0o077)
 try:main()
 except Exception:
  safe=Path(os.environ.get('RUNNER_TEMP','.'))/'cli-safe-summary';safe.mkdir(mode=0o700,exist_ok=True)
  value=review(dict(schema=1,packageId=PACKAGE_ID,candidate=os.environ.get('GITHUB_SHA','0'*40),baseline=BASELINE,status='BLOCKED',rows=[]))
  (safe/'summary.json').write_text(json.dumps(value)+'\n');sys.exit(1)
