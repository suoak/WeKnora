"""Bounded Linux comparison. No production credentials, deployments or migrations."""
import hashlib
import json
import os
import platform
import re
import shutil
import subprocess
import sys
import tempfile
import xml.etree.ElementTree as ET
from pathlib import Path

QA = Path(__file__).resolve().parents[1] / 'qa/a5-b2'
M = json.loads((QA / 'manifest.json').read_text())
BASE = M['baseline']
PACKAGES = ['internal/middleware', 'internal/mcpserver', 'internal/application/service',
            'internal/application/repository', 'internal/application/access', 'internal/types',
            'internal/handler', 'internal/router', 'internal/mcp', 'internal/agent/tools', 'internal/agent',
            'internal/datasource/connector/feishu/core', 'internal/datasource/connector/feishu/wiki',
            'internal/datasource/connector/feishu/drive', 'internal/datasource/connector/ima',
            'internal/datasource/connector/notion', 'internal/datasource/connector/yuque']
RACE = ['internal/mcp', 'internal/mcpserver', 'internal/middleware', 'internal/agent/tools',
        'internal/agent', 'internal/application/repository', 'internal/application/service', 'internal/handler',
        'internal/datasource/connector/feishu/core', 'internal/datasource/connector/feishu/wiki',
        'internal/datasource/connector/feishu/drive', 'internal/datasource/connector/ima',
        'internal/datasource/connector/notion', 'internal/datasource/connector/yuque']
PACKAGES += ['internal/agent/compaction', 'internal/models/api', 'internal/models/api/openaicompletions', 'internal/models/api/openairesponses', 'internal/models/api/anthropicmessages', 'internal/models/api/googlegenai']
RACE += ['internal/agent/compaction', 'internal/models/api', 'internal/models/api/openaicompletions', 'internal/models/api/openairesponses', 'internal/models/api/anthropicmessages', 'internal/models/api/googlegenai', 'internal/types']

CI = {'.github/scripts/knowhub25_a5_b2_ci.py', '.github/workflows/knowhub25-a5-b2-linux.yml',
      '.github/qa/a5-b2/manifest.json', '.github/qa/a5-b2/python-linux.lock',
      '.github/qa/a5-b2/personal_mcp_bridge_test.go', '.github/qa/a5-b2/test_personal_mcp_lifecycle_http.py',
      '.github/qa/a5-b2/notion_nested_http_test.go'}

def sha(b): return hashlib.sha256(b).hexdigest()
def git(root, *args): return subprocess.check_output(['git', '-C', str(root), *args])
def require(ok, reason):
    if not ok: raise RuntimeError(reason)
def save(p, value): p.write_text(json.dumps(value, indent=2))

def inventory_sha(rows):
    return sha(json.dumps(sorted(rows, key=lambda r:r['path']), sort_keys=True, separators=(',', ':')).encode())

def identity(root, approval, index_path=None):
    manifest_contract(M)
    # The precommit path inspects an actual isolated index AND its checkout.
    # Caller-provided clean flags, parent claims and tree claims are not accepted.
    root=Path(root).resolve()
    precommit=index_path is not None
    candidate_sha=None
    if precommit:
        index_path=Path(index_path).resolve()
        native=Path(git(root,'rev-parse','--git-path','index').decode().strip())
        if not native.is_absolute(): native=root/native
        require(index_path!=native.resolve() and index_path.is_file(), 'Isolated index required')
        env=dict(os.environ,GIT_INDEX_FILE=str(index_path))
        def indexed(*args):
            return subprocess.check_output(['git','-C',str(root),*args],env=env)
        parent=git(root,'rev-parse','HEAD').decode().strip()
        require(parent==BASE==approval['parent'],'Baseline mismatch')
        require(not indexed('ls-files','--unmerged','-z'),'Unmerged index')
        tree=indexed('write-tree').decode().strip()
        staged=set(indexed('diff','--cached','--name-only','-z',BASE).decode().rstrip('\0').split('\0'))
        require(staged=={r['path'] for r in M['source']}|CI,'Staged whitelist mismatch')
        require(not indexed('diff','--name-only','-z'),'Unstaged candidate pollution')
        # Include ignored untracked files; no unreviewed local material is admitted.
        require(not indexed('ls-files','--others','-z'),'Untracked candidate pollution')
    else:
        candidate_sha=git(root,'rev-parse','HEAD').decode().strip()
        parent=git(root,'rev-parse','HEAD^').decode().strip()
        tree=git(root,'rev-parse','HEAD^{tree}').decode().strip()
        require(not git(root,'status','--porcelain','--untracked-files=all'), 'Dirty source')
        require(candidate_sha==approval.get('candidateSHA'), 'SHA mismatch')
    require(parent==BASE==approval['parent'], 'Parent mismatch')
    require(tree==approval['candidateTree'], 'Tree mismatch')
    require(M['patch']==approval['productPatch'] and M['productTree']==approval['productTree'], 'External product binding mismatch')
    rows=approval['ciFiles']
    require(len(rows)==7 and {r['path'] for r in rows}==CI, 'External CI whitelist mismatch')
    require(inventory_sha(rows)==approval['ciInventorySHA256'], 'External inventory mismatch')
    manifest=next(r for r in rows if r['path']=='.github/qa/a5-b2/manifest.json')
    require(sha((QA/'manifest.json').read_bytes())==manifest['sha256'], 'External manifest binding mismatch')
    require([r for r in rows if r['path']!=manifest['path']]==M['ci_files'], 'Manifest CI inventory mismatch')
    files=sorted(r['path'] for r in M['source'])
    require(set(git(root,'diff','--name-only',BASE,tree).decode().splitlines())==set(files)|CI, 'Whitelist mismatch')
    for r in M['source']+rows:
        blob=git(root,'rev-parse',tree+':'+r['path']).decode().strip()
        require(blob==r['blob'], 'Source or CI blob mismatch')
        require(sha(git(root,'show',tree+':'+r['path']))==r['sha256'], 'Canonical byte mismatch')
        require(not (root/r['path']).is_symlink(),'Candidate symlink forbidden')
        require(sha((root/r['path']).read_bytes())==r['sha256'], 'Checkout byte mismatch')
    require(sha(git(root,'diff','--binary','--abbrev=9',BASE,tree,'--',*files))==approval['productPatch'], 'Patch mismatch')
    with tempfile.TemporaryDirectory(prefix='knowhub-product-tree-') as temp:
        env=dict(os.environ,GIT_INDEX_FILE=str(Path(temp)/'index'))
        subprocess.run(['git','-C',str(root),'read-tree',BASE],env=env,check=True)
        for r in M['source']:
            mode=git(root,'ls-tree',tree,'--',r['path']).decode().split()[0]
            subprocess.run(['git','-C',str(root),'update-index','--add','--cacheinfo',mode+','+r['blob']+','+r['path']],env=env,check=True)
        projected=subprocess.check_output(['git','-C',str(root),'write-tree'],env=env).decode().strip()
        require(projected==approval['productTree'], 'Product tree mismatch')
    for p,blob in M['protected'].items():
        require(git(root,'rev-parse',tree+':'+p).decode().strip()==blob, 'Protected blob mismatch')
    return dict(candidateSHA=candidate_sha,parent=parent,candidateTree=tree,productTree=projected,
                ciInventorySHA256=approval['ciInventorySHA256'],productPatch=approval['productPatch'],
                mode='UNCOMMITTED_INDEX_ONLY' if precommit else 'REAL_COMMIT_IDENTITY_ONLY',linuxTestPass=False)

def parse(log, compiled, exit_code, required, allowed_skips=None, stderr=''):
    errors=[]; runs=set(); terminal={}; outputs={}; package_terminal=[]
    allowed_skips=allowed_skips or {}; package_names=set()
    for number,line in enumerate(log.splitlines(),1):
        try:
            e=json.loads(line)
            require(isinstance(e,dict) and isinstance(e.get('Action'),str), 'Invalid event')
        except (ValueError,RuntimeError):
            errors.append('invalid_json_or_event_line:'+str(number)); continue
        action=e['Action']; name=e.get('Test'); package=e.get('Package','')
        if action not in {'start','run','pause','cont','output','pass','fail','skip','build-output','build-fail'}:
            errors.append('unknown_action'); continue
        if action=='build-fail': errors.append('build_failed')
        if name:
            key=(package,name); package_names.add(package)
            if action=='run':
                if key in runs: errors.append('duplicate_run:'+name)
                runs.add(key)
            elif action in {'pass','fail','skip'}:
                if key not in runs: errors.append('terminal_without_run:'+name)
                if key in terminal: errors.append('duplicate_or_conflicting_terminal:'+name)
                terminal[key]=action
            elif action=='output':
                if not isinstance(e.get('Output',''),str): errors.append('invalid_output')
                else: outputs.setdefault(key,[]).append(e.get('Output',''))
            elif action in {'pause','cont'} and key not in runs: errors.append('pause_without_run:'+name)
        elif action in {'pass','fail','skip'}:
            package_terminal.append((package,action))
    if log.splitlines():
        try:
            last=json.loads(log.splitlines()[-1])
            if last.get('Action')!='pass' or last.get('Test'): errors.append('stream_not_terminated_by_package_pass')
        except (ValueError,AttributeError): errors.append('truncated_stream')
    if len(package_names)>1: errors.append('multiple_test_packages')
    if len(package_terminal)!=1 or package_terminal[0][1]!='pass': errors.append('package_terminal_not_unique_pass')
    if package_terminal and package_names and package_terminal[0][0] not in package_names: errors.append('package_identity_mismatch')
    unfinished=sorted(name for package,name in runs if (package,name) not in terminal)
    if unfinished: errors.append('run_without_terminal')
    names={a:sorted(name for (package,name),status in terminal.items() if status==a) for a in ['pass','fail','skip']}
    missing=sorted(set(required)-set(names['pass']))
    for package,name in terminal:
        if terminal[(package,name)]!='skip': continue
        exception=allowed_skips.get(package+'::'+name)
        if any(name==r or name.startswith(r+'/') for r in required): errors.append('necessary_test_skipped:'+name)
        elif not exception: errors.append('unapproved_skip:'+name)
        elif exception['reasonText'] not in ''.join(outputs.get((package,name),[])):
            errors.append('skip_reason_mismatch:'+name)
    race='WARNING: DATA RACE' in log+stderr
    ok=compiled==0 and exit_code==0 and bool(runs) and not errors and not names['fail'] and not missing and not race
    return dict(gatePass=ok,compileExit=compiled,testExit=exit_code,run=len(runs),passed=len(names['pass']),
                fail=names['fail'],skip=names['skip'],missing=missing,unfinished=unfinished,
                validationErrors=errors,dataRace=race,requiredPassed=sorted(set(required)&set(names['pass'])))

def parse_file(path, compiled, exit_code, required):
    return parse(path.read_text(encoding='utf-8'),compiled,exit_code,required,M['approved_skips'],
                 path.with_name(path.name+'.stderr.log').read_text(encoding='utf-8'))

def parse_pytest(xml_path, exit_code):
    record = dict(exit=exit_code, run=0, passed=0, fail=[], error=[], skip=[], testcases=[], gatePass=False)
    try:
        root = ET.parse(xml_path).getroot()
        require(root.tag in {'testsuites', 'testsuite'}, 'Unexpected JUnit root')
        cases = list(root.iter('testcase'))
        require(bool(cases), 'Empty JUnit testcases')
        ids = set()
        for case in cases:
            ident = case.get('classname', '') + '::' + case.get('name', '')
            require(bool(case.get('name')) and ident not in ids, 'Missing or duplicate testcase ID')
            ids.add(ident)
            statuses = [status for status in ['failure', 'error', 'skipped'] if case.find(status) is not None]
            require(len(statuses) <= 1, 'Ambiguous JUnit testcase status')
            status = statuses[0] if statuses else 'pass'
            record['testcases'].append(dict(id=ident, status=status, time=case.get('time')))
            if status == 'pass': record['passed'] += 1
            else: record[{'failure': 'fail', 'error': 'error', 'skipped': 'skip'}[status]].append(ident)
        record['run'] = len(cases)
        suites = list(root.iter('testsuite'))
        require(bool(suites), 'Missing JUnit suite')
        for suite in suites:
            nested = list(suite.iter('testcase'))
            expected = dict(tests=len(nested), failures=sum(c.find('failure') is not None for c in nested),
                            errors=sum(c.find('error') is not None for c in nested), skipped=sum(c.find('skipped') is not None for c in nested))
            for field, count in expected.items():
                require(suite.get(field) is not None and int(suite.get(field)) == count, 'JUnit count mismatch: ' + field)
        record['gatePass'] = exit_code == 0 and not record['fail'] and not record['error'] and not record['skip']
        record['validJUnit'] = True
    except (OSError, ET.ParseError, ValueError, RuntimeError) as exc:
        record['validJUnit'] = False
        record['validationError'] = str(exc)
    return record

def junit_selftest():
    good = '<testsuites><testsuite tests="1" failures="0" errors="0" skipped="0"><testcase classname="synthetic" name="test_positive"/></testsuite></testsuites>'
    outcomes = []
    with tempfile.TemporaryDirectory(prefix='knowhub-junit-parser-') as temp:
        path = Path(temp) / 'result.xml'
        scenarios = [('valid', good, 0, True), ('nonzero-exit', good, 1, False),
                     ('missing', None, 0, False), ('malformed', '<invalid', 0, False),
                     ('empty', '<testsuites><testsuite tests="0" failures="0" errors="0" skipped="0"/></testsuites>', 0, False),
                     ('mismatch', good.replace('tests="1"', 'tests="2"'), 0, False)]
        for status, field in [('failure', 'failures'), ('error', 'errors'), ('skipped', 'skipped')]:
            scenarios.append((status, good.replace(field+'="0"',field+'="1"').replace('name="test_positive"/>','name="test_positive"><'+status+'/></testcase>'), 0, False))
        for name, xml, code, expected in scenarios:
            if xml is None:
                if path.exists(): path.unlink()
            else: path.write_text(xml, encoding='utf-8')
            result = parse_pytest(path, code)
            require(result['gatePass'] == expected, 'JUnit selftest accepted/rejected incorrectly: '+name)
            outcomes.append(dict(name=name, expectedGatePass=expected, result=result))
    return outcomes

def execute(root, args, log, env=None):
    with log.open('wb') as f, log.with_name(log.name+'.stderr.log').open('wb') as err:
        return subprocess.run(args, cwd=root, stdout=f, stderr=err, env=env).returncode

def read_output(log):
    return log.read_text(encoding='utf-8',errors='replace') + '\n' + log.with_name(log.name+'.stderr.log').read_text(encoding='utf-8',errors='replace')

def run_unittest(start, pattern, result_path):
    # Structured lifecycle is recorded by the runner, separately from test stdout.
    # Human "Ran N"/"OK" text is never used as evidence of test execution.
    import unittest
    class RecordedResult(unittest.TextTestResult):
        def __init__(self,*args,**kwargs):
            super().__init__(*args,**kwargs)
            self.started=[];self.records=[];self.states={};self.protocol_errors=[]
        def startTest(self,test):
            super().startTest(test)
            ident=test.id()
            if ident in self.states: self.protocol_errors.append('duplicate_test_id')
            self.started.append(ident);self.states[ident]=[]
        def mark(self,test,status):
            parent=getattr(test,'test_case',test);ident=parent.id()
            if ident not in self.states: self.protocol_errors.append('outcome_without_start')
            else: self.states[ident].append(status)
        def addSuccess(self,test): super().addSuccess(test);self.mark(test,'pass')
        def addFailure(self,test,err): super().addFailure(test,err);self.mark(test,'fail')
        def addError(self,test,err): super().addError(test,err);self.mark(test,'error')
        def addSkip(self,test,reason): super().addSkip(test,reason);self.mark(test,'skip')
        def addExpectedFailure(self,test,err): super().addExpectedFailure(test,err);self.mark(test,'expectedFailure')
        def addUnexpectedSuccess(self,test): super().addUnexpectedSuccess(test);self.mark(test,'unexpectedSuccess')
        def addSubTest(self,test,subtest,err):
            super().addSubTest(test,subtest,err)
            if err: self.mark(test,'fail' if issubclass(err[0],test.failureException) else 'error')
        def stopTest(self,test):
            states=self.states.get(test.id(),[])
            if not states: self.protocol_errors.append('missing_terminal')
            if 'pass' in states and len(states)>1: self.protocol_errors.append('conflicting_terminal')
            status=next((v for v in ['error','fail','unexpectedSuccess','expectedFailure','skip','pass'] if v in states),'unfinished')
            self.records.append(dict(id=test.id(),status=status))
            super().stopTest(test)
    suite=unittest.TestLoader().discover(start,pattern=pattern)
    discovered=suite.countTestCases()
    result=unittest.TextTestRunner(verbosity=2,resultclass=RecordedResult).run(suite)
    counts={v:sum(r['status']==v for r in result.records) for v in ['pass','fail','error','skip','expectedFailure','unexpectedSuccess']}
    save(result_path,dict(schema='unittest-lifecycle-v1',completed=True,discovered=discovered,run=result.testsRun,
                          started=result.started,testcases=result.records,counts=counts,
                          wasSuccessful=result.wasSuccessful(),protocolErrors=result.protocol_errors))
    return 0 if result.wasSuccessful() else 1

def parse_unittest(path, exit_code):
    record=dict(exit=exit_code,run=0,passed=0,fail=[],error=[],skip=[],expectedFailure=[],unexpectedSuccess=[],gatePass=False)
    try:
        data=json.loads(path.read_text(encoding='utf-8'))
        require(isinstance(data,dict) and set(data)=={'schema','completed','discovered','run','started','testcases','counts','wasSuccessful','protocolErrors'},'Invalid lifecycle schema')
        require(data['schema']=='unittest-lifecycle-v1' and data['completed'] is True,'Missing completed lifecycle')
        require(isinstance(data['run'],int) and not isinstance(data['run'],bool) and data['run']>0,'Invalid run count')
        require(isinstance(data['discovered'],int) and not isinstance(data['discovered'],bool),'Invalid discovery count')
        cases=data['testcases'];started=data['started'];statuses=['pass','fail','error','skip','expectedFailure','unexpectedSuccess']
        require(isinstance(cases,list) and isinstance(started,list),'Invalid lifecycle arrays')
        require(all(isinstance(r,dict) and set(r)=={'id','status'} for r in cases),'Invalid testcase schema')
        require(isinstance(data['counts'],dict) and set(data['counts'])==set(statuses) and all(isinstance(v,int) and not isinstance(v,bool) and v>=0 for v in data['counts'].values()),'Invalid summary count format')
        ids=[r['id'] for r in cases]
        require(all(isinstance(i,str) and i for i in ids) and len(set(ids))==len(ids),'Missing/duplicate ID')
        require(started==ids and data['run']==data['discovered']==len(cases),'Execution count mismatch')
        require(all(r['status'] in statuses for r in cases),'Invalid terminal status')
        counts={v:sum(r['status']==v for r in cases) for v in statuses}
        require(data['counts']==counts,'Summary contradicts terminals')
        expected_success=not any(counts[v] for v in ['fail','error','unexpectedSuccess'])
        require(data['wasSuccessful'] is expected_success,'Runner outcome contradiction')
        require(data['protocolErrors']==[],'Invalid lifecycle protocol')
        record.update(run=data['run'],passed=counts['pass'],testcases=cases,validLifecycle=True)
        for v in statuses[1:]: record[v]=[r['id'] for r in cases if r['status']==v]
        record['gatePass']=exit_code==0 and counts['pass']==data['run'] and expected_success
    except (OSError,ValueError,KeyError,TypeError,RuntimeError) as exc:
        record['validLifecycle']=False;record['validationErrorClassification']=type(exc).__name__
    return record

def unittest_selftest():
    outcomes=[]
    with tempfile.TemporaryDirectory(prefix='knowhub-unittest-contract-') as temp:
        root=Path(temp)
        pass_fixture='import unittest\nclass Sample(unittest.TestCase):\n def test_a(self): self.assertTrue(True)\n def test_b(self): self.assertEqual(1,1)\n'
        fixtures=[('all-pass',pass_fixture,True),
                  ('one-skip',pass_fixture.replace('def test_a(self): self.assertTrue(True)','def test_a(self): self.skipTest("synthetic skip")'),False),
                  ('all-skip',pass_fixture.replace('self.assertTrue(True)','self.skipTest("synthetic skip")').replace('self.assertEqual(1,1)','self.skipTest("synthetic skip")'),False),
                  ('failure',pass_fixture.replace('self.assertTrue(True)','self.fail("synthetic failure")'),False),
                  ('error',pass_fixture.replace('self.assertTrue(True)','raise RuntimeError("synthetic error")'),False),
                  ('unexpected-success',pass_fixture.replace(' def test_a',' @unittest.expectedFailure\n def test_a'),False),
                  ('stdout-spoof',pass_fixture.replace('self.assertTrue(True)','print("Ran 999 tests\\nOK"); self.fail("synthetic failure")'),False)]
        for name,source,expected in fixtures:
            folder=root/name;folder.mkdir();(folder/'test_fixture.py').write_text(source,encoding='utf-8')
            result=folder/'result.json';log=folder/'stdout.log'
            code=execute(folder,[sys.executable,'-B',str(Path(__file__).resolve()),'--unittest-run',str(folder),'test_fixture.py',str(result)],log)
            parsed=parse_unittest(result,code)
            require(parsed['gatePass']==expected,'Unittest selftest mismatch: '+name)
            outcomes.append(dict(name=name,processExit=code,expectedGatePass=expected,result=parsed))
        valid=json.loads((root/'all-pass/result.json').read_text())
        for name,mutate in [('missing-completion',lambda d:d.pop('completed')),
                            ('count-mismatch',lambda d:d.update(run=3)),
                            ('contradictory-summary',lambda d:d['counts'].update(pass_=1)),
                            ('contradictory-success',lambda d:d.update(wasSuccessful=False)),
                            ('duplicate-terminal',lambda d:d['testcases'].append(d['testcases'][0]))]:
            data=json.loads(json.dumps(valid));mutate(data);p=root/(name+'.json');save(p,data)
            parsed=parse_unittest(p,0);require(not parsed['gatePass'],'Unittest negative accepted: '+name)
            outcomes.append(dict(name=name,expectedGatePass=False,result=parsed))
        for name,raw,code in [('nonzero-exit',json.dumps(valid),1),('truncated','{"schema":',0),('missing-result',None,0)]:
            p=root/(name+'.json')
            if raw is not None: p.write_text(raw,encoding='utf-8')
            parsed=parse_unittest(p,code);require(not parsed['gatePass'],'Unittest negative accepted: '+name)
            outcomes.append(dict(name=name,expectedGatePass=False,result=parsed))
    return outcomes

def parser_selftest():
    junit_selftest()
    valid='\n'.join(json.dumps(r) for r in [{'Action':'run','Test':'TestNecessary'},{'Action':'pass','Test':'TestNecessary'},{'Action':'pass'}])
    require(parse(valid,0,0,['TestNecessary'])['gatePass'], 'positive parser failed')
    for compiled, code, text, req in [(1,None,'',[]),(0,0,valid,['TestMissing']),(0,1,valid,[]),(0,0,valid+'\nWARNING: DATA RACE',[]),(0,0,'',[])]:
        require(not parse(text,compiled,code,req)['gatePass'], 'negative parser accepted')

    tests=[]
    def check(name,text,cc=0,code=0,required=None,allowed=None,expected=False,stderr=''):
        result=parse(text,cc,code,required or [],allowed,stderr)
        require(result['gatePass']==expected,'Parser selftest mismatch: '+name)
        tests.append(dict(name=name,expectedGatePass=expected,result=result))
    check('positive',valid,required=['TestNecessary'],expected=True)
    check('illegal-json',valid+'\nINVALID')
    check('truncated-jsonl',valid+'\n{"Action":')
    check('run-without-terminal',json.dumps({'Action':'run','Test':'TestNecessary'})+'\n'+json.dumps({'Action':'pass'}))
    check('duplicate-conflicting-terminal',valid+'\n'+json.dumps({'Action':'fail','Test':'TestNecessary'}))
    check('duplicate-terminal',valid+'\n'+json.dumps({'Action':'pass','Test':'TestNecessary'}))
    skipped=valid.replace('"Action": "pass", "Test"','"Action": "skip", "Test"')
    check('necessary-skip',skipped,required=['TestNecessary'])
    check('unapproved-extra-skip',skipped)
    exception={'::TestNecessary':dict(reasonText='approved synthetic reason')}
    allowed_text=skipped.replace('\n', '\n'+json.dumps({'Action':'output','Test':'TestNecessary','Output':'approved synthetic reason'})+'\n',1)
    check('precise-approved-skip',allowed_text,allowed=exception,expected=True)
    check('approved-name-wrong-reason',skipped,allowed=exception)
    check('compile-exit-nonzero',valid,cc=1)
    check('test-exit-nonzero',valid,code=1)
    check('stderr-data-race',valid,stderr='WARNING: DATA RACE')
    check('package-fail',valid.rsplit('\n',1)[0]+'\n'+json.dumps({'Action':'fail'}))
    check('missing-package-terminal',valid.rsplit('\n',1)[0])
    return tests

def main():
    workspace=Path(os.environ['GITHUB_WORKSPACE']);evidence=workspace/'evidence';evidence.mkdir(exist_ok=True)
    candidate=workspace/'candidate';baseline=workspace/'baseline';result={'gate':'BLOCKED','groups':{},'notProductionGate':True}
    try:
        manifest_contract(M)
        result['lastCompletedPhase']='MANIFEST'
        save(evidence/'comparison.json',result)
        save(evidence/'go-parser-selftest.json',parser_selftest())
        save(evidence/'junit-parser-selftest.json', junit_selftest())
        save(evidence/'unittest-parser-selftest.json', unittest_selftest())
        approval=json.loads(Path(os.environ['KNOWHUB25_CI_APPROVAL_FILE']).read_text())
        require(approval.get('candidateSHA')==os.environ['GITHUB_SHA'],'External approval SHA mismatch')
        result['identity']=identity(candidate,approval)
        validate_shuffle_source(candidate,'candidate')
        validate_shuffle_source(baseline,'baseline')
        result['lastCompletedPhase']='IDENTITY'
        save(evidence/'comparison.json',result)
        require(git(baseline,'rev-parse','HEAD').decode().strip()==BASE,'Baseline mismatch')
        require(platform.system()=='Linux' and platform.machine()=='x86_64','Runner architecture')
        require(subprocess.check_output(['go','env','GOVERSION']).decode().strip()=='go1.26.7','Go version')
        require(os.getenv('CGO_ENABLED')=='1' and os.getenv('GOTOOLCHAIN')=='local','CGO/toolchain')
        execute(candidate,['go','env','-json'],evidence/'go-env.json')
        execute(candidate,['gcc','--version'],evidence/'gcc.log')
        execute(candidate,[sys.executable,'--version'],evidence/'python-version.log')
        require(sys.version_info[:2]==(3,12),'Python version')
        for label, root in [('baseline',baseline),('candidate',candidate)]:
            require(not git(root,'status','--porcelain'),'Source dirty before modules')
            result['groups'][label]={};group=result['groups'][label]
            for mode, packages in [('normal',PACKAGES),('race',RACE)]:
                for package in packages:
                    folder=evidence/label/(mode+'_'+package.replace('/','_'));folder.mkdir(parents=True)
                    if mode=='normal':
                        cc=execute(root,['go','test','-mod=readonly','-p','2','-c','-x','-o',str(folder/'package.test'),'./'+package],folder/'compile.log')
                    else: cc=0
                    code=None
                    if cc==0:
                        args=['go','test','-mod=readonly','-p','2','-count=1','-json']+(['-race'] if mode=='race' else [])+['./'+package]
                        code=execute(root,args,folder/'test.jsonl')
                    text=read_output(folder/'test.jsonl') if code is not None else ''
                    required=necessary_tests(label,package)
                    record=parse_file(folder/'test.jsonl',cc,code,required) if code is not None else parse('',cc,code,required);record['compileSeparate']=mode=='normal'
                    record['candidateTestsNotPresent']=M['not_present_baseline'].get(package,[]) if label=='baseline' else []
                    group[mode+':'+package]=record;save(folder/'result.json',record);save(evidence/'comparison.json',result)
                result['lastCompletedPhase']=label.upper()+'_'+mode.upper()
                save(evidence/'comparison.json',result)
            if group['normal:internal/agent']['gatePass']:
                folder=evidence/label/'diagnostic_agent_shuffle';folder.mkdir(parents=True)
                code=execute(root,['go','test','-mod=readonly','-p','2','-count=1','-json','-shuffle=on','./internal/agent'],folder/'test.jsonl')
                required=necessary_tests(label,'internal/agent')
                record=parse_file(folder/'test.jsonl',0,code,required)
                record['diagnosticOnly']=True
                record['shuffleDiagnostics']=shuffle_diagnostics(folder/'test.jsonl',label,record)
                group['diagnostic:agent-shuffle']=record;save(folder/'result.json',record)
            else:
                group['diagnostic:agent-shuffle']={'gatePass':False,'status':'NOT_EXECUTED','reason':'normal Agent package did not pass'}
            result['lastCompletedPhase']=label.upper()+'_SHUFFLE'
            save(evidence/'comparison.json',result)
            if label == 'candidate':
                target = root/'internal/datasource/connector/notion/a4_ci_nested_http_test.go'
                require(not target.exists(), 'Notion overlay collision')
                shutil.copyfile(QA/'notion_nested_http_test.go', target)
                try:
                    for mode in ['normal', 'race']:
                        folder=evidence/label/('overlay_'+mode+'_notion');folder.mkdir(parents=True)
                        args=['go','test','-mod=readonly','-p','2','-count=1','-json']+(['-race'] if mode=='race' else [])+['./internal/datasource/connector/notion']
                        code=execute(root,args,folder/'test.jsonl')
                        required=necessary_tests(label,'internal/datasource/connector/notion',overlay=True)
                        record=parse_file(folder/'test.jsonl',0,code,required)
                        record['compileSeparate']=False;record['testOnlyOverlay']=M['overlay'][-1];record['coverageScope']='CANDIDATE_ONLY_ADDITIONAL_GATE'
                        group['overlay-'+mode+':internal/datasource/connector/notion']=record;result['lastCompletedPhase']='CANDIDATE_OVERLAY';save(folder/'result.json',record);save(evidence/'comparison.json',result)
                finally: target.unlink()
            require(not git(root,'status','--porcelain'),'Source mutated after module tests')
            pyenv=dict(os.environ,PYTHONPATH=str(root/'mcp-server'),PYTHONDONTWRITEBYTECODE='1')
            for name,args in [('unittest',[sys.executable,'-B',str(Path(__file__).resolve()),'--unittest-run',str(root/'mcp-server'),'test_*.py',str(evidence/label/'unittest-result.json')]),
                              ('pytest',[sys.executable,'-m','pytest','-q','mcp-server/tests','--junitxml='+str(evidence/label/'pytest.xml')])]:
                log=evidence/label/(name+'.log');code=execute(root,args,log,pyenv);text=read_output(log)
                if name == 'pytest':
                    group['python:'+name] = parse_pytest(evidence/label/'pytest.xml', code)
                    save(evidence/label/'pytest-testcases.json', group['python:'+name])
                else:
                    group['python:'+name]=parse_unittest(evidence/label/'unittest-result.json',code)
            result['lastCompletedPhase']=label.upper()+'_PYTHON'
            save(evidence/'comparison.json',result)
            # Overlay is explicit test-only provenance, never a F1 product source.
            target=root/'internal/middleware/user_mcp_lifecycle_http_test.go'
            require(not target.exists(),'Overlay would overwrite existing file')
            shutil.copyfile(QA/'personal_mcp_bridge_test.go',target)
            try:
                binary=evidence/label/'personal-app.test'
                cc=execute(root,['go','test','-mod=readonly','-p','2','-c','-o',str(binary),'./internal/middleware'],evidence/label/'gateway-fixture-compile.log')
                record={'compileExit':cc,'gatePass':False,'overlay':M['overlay'],'sourceSHA':git(root,'rev-parse','HEAD').decode().strip()}
                if cc==0:
                    gatewayenv=dict(pyenv,PERSONAL_MCP_APP_TEST_BINARY=str(binary))
                    log=evidence/label/'gateway.log'
                    code=execute(root,[sys.executable,'-B',str(Path(__file__).resolve()),'--unittest-run',str(QA),'test_personal_mcp_lifecycle_http.py',str(evidence/label/'gateway-result.json')],log,gatewayenv)
                    record.update(parse_unittest(evidence/label/'gateway-result.json',code))
                    record['gatePass']=record['gatePass'] and record['run']==4 and record['passed']==4
                group['python:real-gateway']=record
            finally: target.unlink()
            require(not git(root,'status','--porcelain'),'Source mutated after overlay removal')
            result['lastCompletedPhase']=label.upper()+'_GATEWAY'
            save(evidence/'comparison.json',result)
        verify_matrix(result,complete=True)
        result['moduleVerifyExit']=execute(candidate,['go','mod','verify'],evidence/'module-verify.log')
        result['pythonFreezeExit']=execute(candidate,[sys.executable,'-m','pip','freeze','--all'],evidence/'python-dependencies.log')
        ok=all(r['gatePass'] for group in result['groups'].values() for r in group.values()) and result['moduleVerifyExit']==0 and result['pythonFreezeExit']==0
        result['gate']='A5_LINUX_REVIEW_READY_PROPOSED' if ok else 'BLOCKED'
        result['lastCompletedPhase']='FINAL_METADATA'
    except Exception as exc:
        result['errorClassification']='HELPER_FAILED'
    finally:
        save(evidence/'comparison.json',result)
    return 0 if result['gate']=='A5_LINUX_REVIEW_READY_PROPOSED' else 1

def artifact_audit(raw_root, public_root):
    # Never edit raw evidence. No raw logs are published when scanning is incomplete,
    # an unknown credential is detected, or any original canary is present.
    require(not public_root.exists(), 'Public output collision')
    public_root.mkdir(parents=True)
    status=dict(approved=False,rawScanComplete=False,files=[],scanBasis='ORIGINAL_BYTES',
                policy='ZERO_HITS_REQUIRED_NO_RAW_PUBLICATION',linuxTestPass=False)
    patterns=[re.compile(re.escape(x)) for x in M['canaries']]
    patterns += [re.compile(r'(?i)bearer[ \t]+(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}'),
                 re.compile(r'(?<![A-Za-z0-9_])mcp_[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])'),
                 re.compile(r'(?i)[\"\']?(?:authorization|cookie|api_key|secret|token)[\"\']?\s*[:=]\s*[\"\']?(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}')]
    suffixes={'.json','.jsonl','.log','.txt','.xml'}
    try:
        require(raw_root.is_dir(), 'Missing raw evidence')
        paths=list(raw_root.rglob('*'))
        require(not any(p.is_symlink() for p in paths), 'Symlink evidence forbidden')
        selected=sorted(p for p in paths if p.is_file() and p.suffix.lower() in suffixes)
        require(bool(selected), 'Empty evidence')
        for p in selected:
            raw=p.read_bytes();text=raw.decode('utf-8',errors='strict')
            findings=scan_findings(text)
            hits=sum(r['count'] for r in findings)
            logical=evidence_role(p.relative_to(raw_root).as_posix())
            status['files'].append(dict(path=p.relative_to(raw_root).as_posix(),rawSHA256=sha(raw),
                                        **logical,
                                        rawBytes=len(raw),scan='HIT' if hits else 'CLEAR',hits=hits,ruleHits=findings))
        status['rawScanComplete']=True
        require(all(r['role']!='UNKNOWN_EVIDENCE' for r in status['files']), 'Unknown evidence role')
        require(all(r['scan']=='CLEAR' for r in status['files']), 'Raw safety scan blocked')
        # A dependency/setup failure before Helper creates no comparison. Preserve only
        # safe hash/status metadata; installation logs remain on the controlled runner.
        comparison=json.loads((raw_root/'comparison.json').read_text())
        verify_matrix(comparison,complete=True)
        require(comparison.get('identity') and set(comparison.get('groups',{}))=={'baseline','candidate'},'Incomplete execution evidence')
        expected={m+':'+p for m,ps in [('normal',PACKAGES),('race',RACE)] for p in ps}
        expected|={'diagnostic:agent-shuffle','python:unittest','python:pytest','python:real-gateway'}
        for label, group in comparison['groups'].items():
            needed=expected|({'overlay-normal:internal/datasource/connector/notion','overlay-race:internal/datasource/connector/notion'} if label=='candidate' else set())
            require(set(group)==needed,'Incomplete matrix evidence')
        require('moduleVerifyExit' in comparison and 'pythonFreezeExit' in comparison,'Missing final metadata')
        for row in status['files']:
            source=raw_root/row['path']; raw=source.read_bytes()
            require(sha(raw)==row['rawSHA256'],'Raw evidence changed after audit')
            dest=public_root/'reviewed'/row['path'];dest.parent.mkdir(parents=True,exist_ok=True);dest.write_bytes(raw)
            row['uploadSHA256']=sha(dest.read_bytes())
            require(row['uploadSHA256']==row['rawSHA256'],'Upload copy identity mismatch')
        status['approved']=True
    except (OSError,ValueError,RuntimeError,UnicodeError) as exc:
        status['blockedClassification']=type(exc).__name__
        # A failed copy is never allowed to produce a publication permit.
    # Detailed paths are internal audit evidence, never an upload target.
    save(public_root/'internal-safety-status.json',status)
    save(public_root/'safety-status.json',public_audit_status(status))
    if status['approved']:
        copies={p.relative_to(public_root/'reviewed').as_posix():sha(p.read_bytes()) for p in (public_root/'reviewed').rglob('*') if p.is_file()}
        require(copies=={r['path']:r['uploadSHA256'] for r in status['files']},'Upload set mismatch')
    return status


# Public diagnostics are a separate, closed projection. They never grant raw upload.
SAFE_FAILURES = {'GO_COMPILE_FAILED','GO_TEST_FAILED','RACE_TEST_FAILED',
 'SHUFFLE_FAILED','SHUFFLE_NOT_EXECUTED','PYTHON_UNITTEST_FAILED','PYTHON_PYTEST_FAILED','GATEWAY_FAILED',
 'RAW_SCAN_HIT','RAW_SCAN_NOT_VERIFIED','EVIDENCE_INCOMPLETE','UNKNOWN_FAILURE',
 'IDENTITY_NOT_VERIFIED','DEPENDENCY_FAILED','HELPER_FAILED','AUDIT_FAILED'}
SAFE_OUTCOMES = {'success','failure','cancelled','skipped','unknown'}

def legacy_fixed_blocked_summary():
    return dict(schemaVersion=1,comparisonAvailable=False,matrixComplete=False,
      rawScanStatus='NOT_VERIFIED',gatePass=False,rootCause='UNKNOWN',
      failures=['EVIDENCE_INCOMPLETE','RAW_SCAN_NOT_VERIFIED','UNKNOWN_FAILURE'],
      matrixRows=None,rawUploadPermitted=False)

def legacy_validate_safe_summary(s):
    require(type(s) is dict and set(s)==set(legacy_fixed_blocked_summary()),'Summary keys')
    require(type(s['schemaVersion']) is int and s['schemaVersion']==1,'Schema version')
    for k in ['comparisonAvailable','matrixComplete','gatePass','rawUploadPermitted']:
        require(type(s[k]) is bool,'Summary boolean')
    require(s['rootCause']=='UNKNOWN','Unproven root cause')
    require(s['rawScanStatus'] in {'NOT_VERIFIED','CLEAR','CANARY_DETECTED'},'Scan status')
    f=s['failures'];require(type(f) is list and all(type(x) is str and x in SAFE_FAILURES for x in f),'Failure enum')
    require(f==sorted(set(f)),'Failure uniqueness')
    rows=s['matrixRows'];require(rows is None or type(rows) is int and 0<=rows<=98,'Matrix rows')
    require(s['comparisonAvailable'] == (rows is not None),'Availability/count mismatch')
    require(not s['matrixComplete'] or (s['comparisonAvailable'] and rows==98),'Incomplete matrix')
    require(not s['gatePass'] or (s['matrixComplete'] and s['rawScanStatus']=='CLEAR' and not f),'Gate prerequisites')
    require(s['gatePass'] or bool(f),'Missing blocked reason')
    require(not s['rawUploadPermitted'],'Diagnostics cannot permit raw upload')
    require((s['rawScanStatus']=='CANARY_DETECTED') == ('RAW_SCAN_HIT' in f),'Hit fact mismatch')
    require((s['rawScanStatus']=='NOT_VERIFIED') == ('RAW_SCAN_NOT_VERIFIED' in f),'Incomplete scan fact')
    return s

def strict_diagnostic_json(path):
    def pairs(items):
        d={}
        for k,v in items:
            require(k not in d,'Duplicate JSON key');d[k]=v
        return d
    return json.loads(Path(path).read_text(encoding='utf-8'),object_pairs_hook=pairs,
                      parse_constant=lambda x: (_ for _ in ()).throw(ValueError('JSON constant')))

def legacy_safe_summary(raw_root, public_root, outcomes):
    require(set(outcomes)=={'identity','dependencies','comparison','audit'} and
            all(v in SAFE_OUTCOMES for v in outcomes.values()),'Step outcome enum')
    s=legacy_fixed_blocked_summary();facts=set();all_pass=False
    if outcomes['identity']!='success':facts.add('IDENTITY_NOT_VERIFIED')
    if outcomes['dependencies']!='success':facts.add('DEPENDENCY_FAILED')
    if outcomes['audit']!='success':facts.add('AUDIT_FAILED')
    try:
        c=strict_diagnostic_json(Path(raw_root)/'comparison.json')
        require(type(c) is dict and type(c.get('groups')) is dict,'Comparison shape')
        if c.get('errorClassification')=='HELPER_FAILED':facts.add('HELPER_FAILED')
        require(set(c['groups'])=={'baseline','candidate'},'Comparison sides')
        expected={mode+':'+p for mode,ps in [('normal',PACKAGES),('race',RACE)] for p in ps}
        expected|={'diagnostic:agent-shuffle','python:unittest','python:pytest','python:real-gateway'}
        n=0;complete=True;all_pass=True
        for label,g in c['groups'].items():
            keys=expected|({'overlay-normal:internal/datasource/connector/notion','overlay-race:internal/datasource/connector/notion'} if label=='candidate' else set())
            require(type(g) is dict and set(g)<=keys,'Comparison keys')
            complete=complete and set(g)==keys
            for key,r in g.items():
                require(type(r) is dict and type(r.get('gatePass')) is bool,'Result shape')
                n+=1;all_pass=all_pass and r['gatePass']
                if not r['gatePass']:
                    if type(r.get('compileExit')) is int and r['compileExit']!=0: facts.add('GO_COMPILE_FAILED')
                    elif key.startswith('python:'):facts.add({'python:unittest':'PYTHON_UNITTEST_FAILED','python:pytest':'PYTHON_PYTEST_FAILED','python:real-gateway':'GATEWAY_FAILED'}[key])
                    elif key=='diagnostic:agent-shuffle':facts.add('SHUFFLE_NOT_EXECUTED' if r.get('status')=='NOT_EXECUTED' else 'SHUFFLE_FAILED')
                    elif r.get('fail'):facts.add('RACE_TEST_FAILED' if key.startswith(('race:','overlay-race:')) else 'GO_TEST_FAILED')
                    else:facts.add('EVIDENCE_INCOMPLETE')
        s.update(comparisonAvailable=True,matrixRows=n,matrixComplete=complete)
        all_pass=all_pass and complete and bool(c.get('identity')) and c.get('moduleVerifyExit')==0 and c.get('pythonFreezeExit')==0 and c.get('gate')=='A5_LINUX_REVIEW_READY_PROPOSED'
        if outcomes['comparison']!='success' and not facts:facts.add('UNKNOWN_FAILURE')
        if not complete:facts.add('EVIDENCE_INCOMPLETE')
    except (OSError,ValueError,RuntimeError,TypeError,KeyError):
        s=legacy_fixed_blocked_summary();facts.add('EVIDENCE_INCOMPLETE');all_pass=False
    try:
        a=strict_diagnostic_json(Path(public_root)/'internal-safety-status.json')
        require(type(a) is dict and a.get('rawScanComplete') is True and a.get('scanBasis')=='ORIGINAL_BYTES','Incomplete audit')
        files=a.get('files');require(type(files) is list and bool(files),'Missing scan records')
        for r in files:
            require(type(r) is dict and type(r.get('hits')) is int and r['hits']>=0 and
                    r.get('scan') in {'CLEAR','HIT'} and (r['scan']=='HIT')==(r['hits']>0) and
                    re.fullmatch('[0-9a-f]{64}',r.get('rawSHA256','')) is not None,'Scan record')
            p=Path(raw_root)/r['path'];require(p.resolve().is_relative_to(Path(raw_root).resolve()) and not p.is_symlink(),'Scan path')
            require(sha(p.read_bytes())==r['rawSHA256'],'Audit evidence changed')
        actual={p.relative_to(raw_root).as_posix() for p in Path(raw_root).rglob('*') if p.is_file() and p.suffix.lower() in {'.json','.jsonl','.log','.txt','.xml'}}
        require(actual=={r['path'] for r in files} and len(files)==len(actual),'Scan coverage')
        s['rawScanStatus']='CANARY_DETECTED' if any(r['hits'] for r in files) else 'CLEAR'
        if s['rawScanStatus']=='CANARY_DETECTED':facts.add('RAW_SCAN_HIT')
    except (OSError,ValueError,RuntimeError,TypeError,KeyError):
        s['rawScanStatus']='NOT_VERIFIED';facts.add('RAW_SCAN_NOT_VERIFIED')
    s['gatePass']=all_pass and s['rawScanStatus']=='CLEAR' and all(v=='success' for v in outcomes.values()) and not facts
    if not s['gatePass'] and not facts:facts.add('UNKNOWN_FAILURE')
    s['failures']=sorted(facts);return legacy_validate_safe_summary(s)

# P1-A closed infrastructure contract. The digest is taken from the approved
# original manifest, not derived from a possibly weakened runtime manifest.
NECESSARY_CONTRACT_SHA256='c12e8822bab3231a8c94f3d00e2574a3f907e9475acea01258fc9c5e5d034b64'
PHASES={'NOT_AVAILABLE','MANIFEST','IDENTITY','BASELINE_NORMAL','BASELINE_RACE',
 'BASELINE_SHUFFLE','BASELINE_PYTHON','BASELINE_GATEWAY','CANDIDATE_NORMAL',
 'CANDIDATE_RACE','CANDIDATE_SHUFFLE','CANDIDATE_OVERLAY','CANDIDATE_PYTHON',
 'CANDIDATE_GATEWAY','FINAL_METADATA'}
MATRIX_STATES={'PASS','FAIL','NOT_EXECUTED','NOT_AVAILABLE'}
SCAN_RULES={'SYNTHETIC_%03d'%(i+1) for i in range(len(M['canaries']))}|{
 'BEARER_PATTERN','MCP_KEY_PATTERN','CREDENTIAL_ASSIGNMENT'}

def manifest_contract(manifest):
    require(type(manifest) is dict,'Manifest shape')
    fields=['required','candidate_required','not_present_baseline','approved_skips','overlay']
    require(all(k in manifest for k in fields),'Manifest required key missing')
    actual=sha(json.dumps({k:manifest[k] for k in fields},sort_keys=True,separators=(',',':')).encode())
    require(actual==NECESSARY_CONTRACT_SHA256,'Manifest necessary assertions differ')
    c=manifest.get('p1a_contract',{})
    require(c.get('necessarySHA256')==NECESSARY_CONTRACT_SHA256 and c.get('expectedMatrixRows')==98,
            'Manifest matrix/necessary binding')
    require(c.get('overlayRequired')==['TestA4CINestedHTTPFailure'],'Overlay assertion contract')
    for name in ['required','candidate_required']:
        require(type(manifest[name]) is dict,'Necessary map')
        for package,tests in manifest[name].items():
            require(package in PACKAGES and type(tests) is list and
                    all(type(t) is str and t.startswith('Test') for t in tests) and
                    len(tests)==len(set(tests)),'Necessary tests invalid')
    require(len(matrix_slots())==98,'Matrix cardinality')
    diagnostic_contract(manifest)
    return {'gatePass':True,'matrixRows':98,'necessarySHA256':actual}

def necessary_tests(side,package,overlay=False):
    manifest_contract(M)
    require(side in {'baseline','candidate'} and package in PACKAGES,'Necessary selection')
    values=list(M['required'].get(package,[]))
    if side=='candidate':values+=M['candidate_required'].get(package,[])
    if overlay:
        require(side=='candidate' and package=='internal/datasource/connector/notion','Overlay scope')
        values+=M['p1a_contract']['overlayRequired']
    return sorted(set(values))

def matrix_slots():
    slots=[]
    for side in ['baseline','candidate']:
        for mode,packages in [('normal',PACKAGES),('race',RACE)]:
            slots.extend((side,mode+':'+p) for p in packages)
        slots.extend((side,k) for k in ['diagnostic:agent-shuffle','python:unittest','python:pytest','python:real-gateway'])
        if side=='candidate':
            slots.extend((side,'overlay-'+mode+':internal/datasource/connector/notion') for mode in ['normal','race'])
    return slots

def verify_matrix(comparison,complete=False):
    require(type(comparison) is dict and type(comparison.get('groups')) is dict,'Matrix groups')
    groups=comparison['groups'];require(set(groups)<={'baseline','candidate'},'Matrix side')
    expected=set(matrix_slots());actual=set()
    for side,group in groups.items():
        require(type(group) is dict,'Matrix group shape')
        for key,row in group.items():
            require((side,key) in expected and type(row) is dict and type(row.get('gatePass')) is bool,'Matrix row shape')
            for field in ['compileExit','testExit','exit']:
                value=row.get(field);require(value is None or type(value) is int,'Matrix exit shape')
            if row['gatePass']:
                require(row.get('status')!='NOT_EXECUTED','Not executed PASS forbidden')
                require(row.get('compileExit',0)==0 and row.get('testExit',row.get('exit',0))==0,
                        'Nonzero exit PASS forbidden')
                if not key.startswith('python:'):
                    require(type(row.get('run')) is int and row['run']>0 and
                            type(row.get('passed')) is int and type(row.get('skip')) is list and
                            row.get('fail')==[] and row['passed']+len(row['skip'])==row['run'] and
                            not row.get('unfinished') and not row.get('validationErrors') and
                            row.get('dataRace') is not True,'PASS terminal counts invalid')
                    package='internal/agent' if key=='diagnostic:agent-shuffle' else key.split(':',1)[1]
                    necessary=necessary_tests(side,package,overlay=key.startswith('overlay-'))
                    require(set(necessary)<=set(row.get('requiredPassed',[])),'Necessary PASS terminal missing')
            actual.add((side,key))
    require(not complete or actual==expected,'Matrix incomplete')
    return {'expected':98,'available':len(actual),'complete':actual==expected}

def scan_findings(text):
    # Exact original rules, now with fixed safe identifiers. No snippets captured.
    rules=[('SYNTHETIC_%03d'%(i+1),re.compile(re.escape(x))) for i,x in enumerate(M['canaries'])]
    rules += [('BEARER_PATTERN',re.compile(r'(?i)bearer[ \t]+(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}')),
      ('MCP_KEY_PATTERN',re.compile(r'(?<![A-Za-z0-9_])mcp_[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])')),
      ('CREDENTIAL_ASSIGNMENT',re.compile(r'(?i)[\"\']?(?:authorization|cookie|api_key|secret|token)[\"\']?\s*[:=]\s*[\"\']?(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}'))]
    return [dict(ruleId=name,count=len(pattern.findall(text))) for name,pattern in rules if pattern.search(text)]

EVIDENCE_ROLES={'GO_TEST_JSONL','GO_COMPILE_LOG','GO_RESULT_METADATA','SHUFFLE_JSONL',
 'SHUFFLE_METADATA','PY_JUNIT_XML','PY_JUNIT_METADATA','PYTHON_OUTPUT','GATEWAY_OUTPUT',
 'GATEWAY_METADATA','COMPARISON_METADATA','PARSER_METADATA','ENVIRONMENT_METADATA','UNKNOWN_EVIDENCE'}
SIDES={'BASELINE','CANDIDATE','GLOBAL'}
MODES={'NORMAL','RACE','SHUFFLE','OVERLAY_NORMAL','OVERLAY_RACE','PYTHON','METADATA'}
PACKAGE_IDS={'PKG_%03d'%(i+1) for i in range(len(PACKAGES))}|{'NOT_APPLICABLE'}

def diagnostic_contract(manifest):
    d=manifest.get('p2r2_diagnostics')
    require(type(d) is dict and set(d)=={'revision','roles','tests','sourceBlobs','testInventorySHA256'},'Diagnostic contract fields')
    require(d['revision']==1 and d['roles']==sorted(EVIDENCE_ROLES),'Diagnostic contract enums')
    require(type(d['tests']) is dict and set(d['tests'])=={'baseline','candidate'},'Test inventory sides')
    require(type(d['sourceBlobs']) is dict and set(d['sourceBlobs'])=={'baseline','candidate'},'Test source sides')
    require(sha(json.dumps(d['tests'],sort_keys=True,separators=(',',':')).encode())==d['testInventorySHA256'], 'Test inventory digest')
    for side,rows in d['tests'].items():
        require(type(rows) is list and bool(rows),'Empty test inventory')
        names=set();ids=set()
        for r in rows:
            require(type(r) is dict and set(r)=={'internalId','testId','parentId'},'Test inventory fields')
            require(type(r['internalId']) is str and r['internalId'].split('/')[0]==r['parentId'],'Test parent identity')
            require(re.fullmatch(r'(?:Test|Fuzz|Example)[A-Za-z0-9_]+',r['parentId']) is not None,'Source parent name')
            require(type(r['testId']) is str and re.fullmatch(r'[A-Za-z0-9_/=.-]{1,256}',r['testId']) is not None,'Safe test ID')
            require(r['internalId'] not in names and r['testId'] not in ids,'Duplicate test ID')
            names.add(r['internalId']);ids.add(r['testId'])

def validate_shuffle_source(root,side):
    sources=M['p2r2_diagnostics']['sourceBlobs'][side];roots=set()
    require(type(sources) is dict and bool(sources),'Missing test source binding')
    for path,blob in sources.items():
        require(re.fullmatch(r'internal/agent/[A-Za-z0-9_]+_test.go',path) is not None,'Test source path')
        require(re.fullmatch('[0-9a-f]{40}',blob) is not None,'Test source blob')
        data=(Path(root)/path).read_bytes()
        actual=subprocess.check_output(['git','-C',str(root),'hash-object','--path='+path,'--stdin'],input=data).decode().strip()
        require(actual==blob,'Test source identity')
        roots.update(re.findall(r'^func ((?:Test|Fuzz|Example)\w+)\(',data.decode('utf-8'),re.M))
    require(all(r['parentId'] in roots for r in M['p2r2_diagnostics']['tests'][side]),'Test whitelist source mismatch')

def validate_role(row):
    require(row['role'] in EVIDENCE_ROLES and row['side'] in SIDES and row['mode'] in MODES and row['packageId'] in PACKAGE_IDS,'Evidence role enum')

def public_audit_status(status):
    files=[]
    for row in status['files']:
        logical={k:row[k] for k in ['role','side','mode','packageId']};validate_role(logical)
        require(re.fullmatch('[0-9a-f]{64}',row['rawSHA256']) is not None,'Audit hash')
        for hit in row['ruleHits'] or [dict(ruleId='NO_HIT',count=0)]:
            require(hit['ruleId'] in SCAN_RULES|{'NO_HIT'} and type(hit['count']) is int and hit['count']>=0,'Audit rule/count')
            files.append(dict(logical,**hit,rawSHA256=row['rawSHA256']))
    return dict(approved=status['approved'],rawScanComplete=status['rawScanComplete'],scanBasis='ORIGINAL_BYTES',files=files)

def evidence_role(relative):
    # Only exact scheduler-owned locations map to a role. Never expose the path.
    base=relative.removesuffix('.stderr.log')
    value=dict(role='UNKNOWN_EVIDENCE',side='GLOBAL',mode='METADATA',packageId='NOT_APPLICABLE')
    global_roles={'comparison.json':'COMPARISON_METADATA','go-env.json':'ENVIRONMENT_METADATA',
      'gcc.log':'ENVIRONMENT_METADATA','python-version.log':'ENVIRONMENT_METADATA',
      'module-verify.log':'ENVIRONMENT_METADATA','python-dependencies.log':'ENVIRONMENT_METADATA',
      'go-parser-selftest.json':'PARSER_METADATA','junit-parser-selftest.json':'PARSER_METADATA',
      'unittest-parser-selftest.json':'PARSER_METADATA'}
    if base in global_roles:value['role']=global_roles[base];return value
    for side in ['baseline','candidate']:
        prefix=side+'/'
        if not base.startswith(prefix):continue
        part=base[len(prefix):];value['side']=side.upper()
        simple={'unittest.log':'PYTHON_OUTPUT','unittest-result.json':'PY_JUNIT_METADATA',
          'pytest.log':'PYTHON_OUTPUT','pytest.xml':'PY_JUNIT_XML','pytest-testcases.json':'PY_JUNIT_METADATA',
          'gateway.log':'GATEWAY_OUTPUT','gateway-result.json':'GATEWAY_METADATA','gateway-fixture-compile.log':'GO_COMPILE_LOG'}
        if part in simple:value.update(role=simple[part],mode='PYTHON');return value
        for mode,packages in [('normal',PACKAGES),('race',RACE),('overlay_normal',['internal/datasource/connector/notion']),('overlay_race',['internal/datasource/connector/notion'])]:
            for package in packages:
                folder=(mode+'_'+package.replace('/','_')) if not mode.startswith('overlay') else mode+'_notion'
                roles={'test.jsonl':'GO_TEST_JSONL','compile.log':'GO_COMPILE_LOG','result.json':'GO_RESULT_METADATA'}
                for leaf,role in roles.items():
                    if part==folder+'/'+leaf:
                        value.update(role=role,mode=mode.upper(),packageId='PKG_%03d'%(PACKAGES.index(package)+1));return value
        roles={'test.jsonl':'SHUFFLE_JSONL','result.json':'SHUFFLE_METADATA'}
        for leaf,role in roles.items():
            if part=='diagnostic_agent_shuffle/'+leaf:
                value.update(role=role,mode='SHUFFLE',packageId='PKG_%03d'%(PACKAGES.index('internal/agent')+1));return value
    return value

def unavailable_shuffle(side,state='NOT_AVAILABLE',failure='EVIDENCE_INCOMPLETE'):
    return dict(side=side.upper(),state=state,seed=None,failureClass=failure,failedTests=[])

def validate_shuffle(row):
    require(type(row) is dict and set(row)=={'side','state','seed','failureClass','failedTests'},'Shuffle fields')
    require(row['side'] in {'BASELINE','CANDIDATE'} and row['state'] in MATRIX_STATES,'Shuffle enums')
    require(row['failureClass'] in {'NONE','TEST_FAILED','PRECONDITION_FAILED','EVIDENCE_INCOMPLETE','DATA_RACE'},'Shuffle failure enum')
    require(row['seed'] is None or type(row['seed']) is int and 0<=row['seed']<2**63,'Shuffle seed')
    if row['state'] in {'NOT_EXECUTED','NOT_AVAILABLE'}:require(row['seed'] is None and not row['failedTests'],'Unexecuted shuffle evidence')
    else:require(row['seed'] is not None,'Shuffle seed missing')
    if row['state']=='PASS':require(row['failureClass']=='NONE' and not row['failedTests'],'Shuffle PASS invalid')
    require(type(row['failedTests']) is list,'Shuffle test list')
    allowed={r['testId']:r for r in M['p2r2_diagnostics']['tests'][row['side'].lower()]};seen=set()
    for t in row['failedTests']:
        require(type(t) is dict and set(t)=={'testId','parentId','scope','state','failureClass'},'Shuffle test fields')
        require(t['testId'] in allowed and t['testId'] not in seen,'Unapproved test ID');seen.add(t['testId'])
        a=allowed[t['testId']]
        require(t['parentId']==a['parentId'] and t['scope']==('CHILD' if '/' in a['internalId'] else 'PARENT'),'Shuffle test parent')
        require(t['state']=='FAIL' and t['failureClass']=='TEST_FAILED','Shuffle test enum')
    return row

def shuffle_diagnostics(path,side,record):
    # Do not classify assertion text or echo arbitrary names. Manifest IDs are fixed.
    allowed={r['internalId']:r for r in M['p2r2_diagnostics']['tests'][side]}
    seeds=[];failed=[]
    for line in path.read_text(encoding='utf-8').splitlines():
        event=json.loads(line);require(type(event) is dict,'Shuffle JSON event')
        output=event.get('Output','');require(type(output) is str,'Shuffle output type')
        match=re.fullmatch(r'-test.shuffle ([0-9]+)\n?',output)
        if match:seeds.append(int(match[1]))
        if event.get('Test') and event.get('Action')=='fail':
            require(event['Test'] in allowed,'Unapproved shuffle failure ID')
            a=allowed[event['Test']]
            failed.append(dict(testId=a['testId'],parentId=a['parentId'],scope='CHILD' if '/' in a['internalId'] else 'PARENT',state='FAIL',failureClass='TEST_FAILED'))
    require(len(seeds)==1 and 0<=seeds[0]<2**63,'Missing/conflicting shuffle seed')
    require(len(failed)==len(record.get('fail',[])),'Shuffle failure evidence mismatch')
    reason='NONE' if record['gatePass'] else ('DATA_RACE' if record.get('dataRace') else ('TEST_FAILED' if failed else 'EVIDENCE_INCOMPLETE'))
    return validate_shuffle(dict(side=side.upper(),state='PASS' if record['gatePass'] else 'FAIL',seed=seeds[0],failureClass=reason,failedTests=failed))

def fixed_blocked_summary():
    s=legacy_fixed_blocked_summary();s['schemaVersion']=2
    s.update(lastCompletedPhase='NOT_AVAILABLE',matrix=[],scanFindings=[],shuffleDiagnostics=[])
    return s

def validate_safe_summary(s):
    require(type(s) is dict and set(s)==set(fixed_blocked_summary()),'Summary keys')
    require(type(s['schemaVersion']) is int and s['schemaVersion']==2,'Schema version')
    core={k:v for k,v in s.items() if k not in {'lastCompletedPhase','matrix','scanFindings','shuffleDiagnostics'}}
    core['schemaVersion']=1
    require(s['rawScanStatus'] in {'NOT_VERIFIED','CLEAR','SYNTHETIC_CANARY_DETECTED','SUSPECTED_CREDENTIAL_DETECTED','MIXED_HIT_DETECTED'},'Public scan enum')
    if s['rawScanStatus'] not in {'NOT_VERIFIED','CLEAR'}:core['rawScanStatus']='CANARY_DETECTED'
    legacy_validate_safe_summary(core)
    require(s['lastCompletedPhase'] in PHASES,'Phase enum')
    require(type(s['matrix']) is list and len(s['matrix']) in {0,98},'Matrix projection shape')
    expected={'ROW_%03d'%(i+1) for i in range(98)};seen=set()
    for row in s['matrix']:
        require(type(row) is dict and set(row)=={'logicalId','state','compileExit','testExit','compileMode','run','passed','fail','skip'},'Public row fields')
        require(row['logicalId'] in expected and row['logicalId'] not in seen,'Public row ID')
        seen.add(row['logicalId']);require(row['state'] in MATRIX_STATES,'Public state enum')
        for field in ['compileExit','testExit']:
            require(row[field] is None or type(row[field]) is int,'Public exit type')
        require(row['compileMode'] in {'SEPARATE','IN_TEST','NOT_APPLICABLE','NOT_AVAILABLE'},'Compile mode')
        for field in ['run','passed','fail','skip']:
            require(row[field] is None or type(row[field]) is int and row[field]>=0,'Public count type')
        if row['state'] in {'NOT_AVAILABLE','NOT_EXECUTED'}:
            require(row['testExit'] is None,'Unexecuted test exit forbidden')
        if row['state']=='PASS':require(row['compileExit'] in {None,0} and row['testExit']==0,'Public PASS exit')
    if s['matrix']:
        require(sum(r['state']!='NOT_AVAILABLE' for r in s['matrix'])==s['matrixRows'],'Public availability mismatch')
    require(type(s['scanFindings']) is list,'Findings shape')
    seen=set()
    for row in s['scanFindings']:
        require(type(row) is dict and set(row)=={'ruleId','count','role','side','mode','packageId','rawSHA256'},'Finding fields')
        require(row['ruleId'] in SCAN_RULES and type(row['count']) is int and row['count']>0,'Finding rule/count')
        validate_role(row)
        require(type(row['rawSHA256']) is str and re.fullmatch('[0-9a-f]{64}',row['rawSHA256']),'Finding hash')
        key=tuple(row[k] for k in ['ruleId','rawSHA256','role','side','mode','packageId']);require(key not in seen,'Duplicate finding');seen.add(key)
    hits=s['rawScanStatus'] not in {'NOT_VERIFIED','CLEAR'}
    require(bool(s['scanFindings'])==hits,'Missing/unverified rule evidence')
    if hits:
        synthetic=any(r['ruleId'].startswith('SYNTHETIC_') for r in s['scanFindings'])
        suspected=any(not r['ruleId'].startswith('SYNTHETIC_') for r in s['scanFindings'])
        expected='MIXED_HIT_DETECTED' if synthetic and suspected else ('SYNTHETIC_CANARY_DETECTED' if synthetic else 'SUSPECTED_CREDENTIAL_DETECTED')
        require(s['rawScanStatus']==expected,'Hit class mismatch')
    require(type(s['shuffleDiagnostics']) is list and len(s['shuffleDiagnostics']) in {0,2},'Shuffle projection shape')
    sides=set()
    for row in s['shuffleDiagnostics']:
        validate_shuffle(row)
        require(row['side'] not in sides,'Duplicate shuffle side');sides.add(row['side'])
    if s['shuffleDiagnostics']:require(sides=={'BASELINE','CANDIDATE'},'Shuffle sides')
    return s

def safe_summary(raw_root,public_root,outcomes):
    core=legacy_safe_summary(raw_root,public_root,outcomes)
    s=dict(core,schemaVersion=2,lastCompletedPhase='NOT_AVAILABLE',matrix=[],scanFindings=[],shuffleDiagnostics=[])
    try:
        c=strict_diagnostic_json(Path(raw_root)/'comparison.json');v=verify_matrix(c)
        phase=c.get('lastCompletedPhase','NOT_AVAILABLE');require(phase in PHASES,'Phase invalid')
        s['lastCompletedPhase']=phase
        for side in ['baseline','candidate']:
            row=c.get('groups',{}).get(side,{}).get('diagnostic:agent-shuffle')
            if row is None:detail=unavailable_shuffle(side)
            elif row.get('status')=='NOT_EXECUTED':detail=unavailable_shuffle(side,'NOT_EXECUTED','PRECONDITION_FAILED')
            else:
                detail=shuffle_diagnostics(Path(raw_root)/side/'diagnostic_agent_shuffle/test.jsonl',side,row)
                require(detail==row.get('shuffleDiagnostics'),'Shuffle source mismatch')
            s['shuffleDiagnostics'].append(detail)
        for i,(side,key) in enumerate(matrix_slots()):
            r=c.get('groups',{}).get(side,{}).get(key)
            state='NOT_AVAILABLE' if r is None else ('NOT_EXECUTED' if r.get('status')=='NOT_EXECUTED' or
              (r.get('compileExit') not in {None,0} and r.get('testExit',r.get('exit')) is None)
              else ('PASS' if r['gatePass'] else 'FAIL'))
            def count(field):
                value=r.get(field) if r and state not in {'NOT_AVAILABLE','NOT_EXECUTED'} else None
                return len(value) if type(value) is list else (value if type(value) is int else None)
            s['matrix'].append(dict(logicalId='ROW_%03d'%(i+1),state=state,
              compileExit=r.get('compileExit') if r else None,
              testExit=None if state in {'NOT_AVAILABLE','NOT_EXECUTED'} else r.get('testExit',r.get('exit',0 if r['gatePass'] else None)),
              compileMode='NOT_AVAILABLE' if r is None else ('NOT_APPLICABLE' if key.startswith('python:') else ('SEPARATE' if r.get('compileSeparate') else 'IN_TEST')),
              run=count('run'),passed=count('passed'),fail=count('fail'),skip=count('skip')))
        require(core['comparisonAvailable'] and core['matrixRows']==v['available'],'Matrix core mismatch')
    except (OSError,ValueError,RuntimeError,TypeError,KeyError):
        s.update(comparisonAvailable=False,matrixComplete=False,matrixRows=None,matrix=[],shuffleDiagnostics=[],lastCompletedPhase='NOT_AVAILABLE',gatePass=False)
        s['failures']=sorted(set(s['failures'])|{'EVIDENCE_INCOMPLETE'})
    if s['rawScanStatus']=='CANARY_DETECTED':
        try:
            a=strict_diagnostic_json(Path(public_root)/'internal-safety-status.json')
            for row in a['files']:
                p=Path(raw_root)/row['path'];actual=scan_findings(p.read_bytes().decode('utf-8',errors='strict'))
                require(actual==row.get('ruleHits') and sum(x['count'] for x in actual)==row['hits'],'Rule evidence mismatch')
                logical=evidence_role(row['path'])
                require(all(row.get(k)==v for k,v in logical.items()),'Role evidence mismatch')
                for hit in actual:
                    s['scanFindings'].append(dict(hit,**logical,rawSHA256=row['rawSHA256']))
            # Same-content files share a hash; aggregate the stable public identity.
            merged={}
            for row in s['scanFindings']:
                key=tuple(row[k] for k in ['ruleId','rawSHA256','role','side','mode','packageId'])
                if key in merged:merged[key]['count']+=row['count']
                else:merged[key]=row
            s['scanFindings']=list(merged.values())
            require(bool(s['scanFindings']),'Missing rule evidence')
            synthetic=any(r['ruleId'].startswith('SYNTHETIC_') for r in s['scanFindings'])
            suspected=any(not r['ruleId'].startswith('SYNTHETIC_') for r in s['scanFindings'])
            s['rawScanStatus']='MIXED_HIT_DETECTED' if synthetic and suspected else ('SYNTHETIC_CANARY_DETECTED' if synthetic else 'SUSPECTED_CREDENTIAL_DETECTED')
        except (OSError,ValueError,RuntimeError,TypeError,KeyError,UnicodeError):
            s.update(rawScanStatus='NOT_VERIFIED',scanFindings=[],gatePass=False)
            s['failures']=sorted((set(s['failures'])-{'RAW_SCAN_HIT'})|{'RAW_SCAN_NOT_VERIFIED'})
    return validate_safe_summary(s)

if __name__=='__main__':
    if '--safe-summary' in sys.argv:
        outcomes=dict(zip(['identity','dependencies','comparison','audit'],sys.argv[5:9]))
        save(Path(sys.argv[4]),safe_summary(Path(sys.argv[2]),Path(sys.argv[3]),outcomes))
    elif '--validate-safe-summary' in sys.argv:
        validate_safe_summary(strict_diagnostic_json(Path(sys.argv[2])))
    elif '--parser-selftest' in sys.argv: parser_selftest();print('parser selftest PASS')
    elif '--unittest-selftest' in sys.argv:
        print(json.dumps(unittest_selftest(),indent=2))
    elif '--unittest-run' in sys.argv:
        sys.exit(run_unittest(sys.argv[2],sys.argv[3],Path(sys.argv[4])))
    elif '--identity-only' in sys.argv:
        print(json.dumps(identity(Path(sys.argv[2]),json.loads(Path(sys.argv[3]).read_text())),indent=2))
    elif '--index-identity' in sys.argv:
        approval=json.loads(Path(sys.argv[3]).read_text())
        print(json.dumps(identity(Path(sys.argv[2]),approval,index_path=Path(sys.argv[4])),indent=2))
    elif '--artifact-audit' in sys.argv:
        status=artifact_audit(Path(sys.argv[2]),Path(sys.argv[3]))
        if os.getenv('GITHUB_OUTPUT'):
            with open(os.environ['GITHUB_OUTPUT'],'a') as f: f.write('approved='+str(status['approved']).lower()+'\n')
        sys.exit(0 if status['approved'] else 1)
    else: sys.exit(main())
