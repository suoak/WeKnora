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
        save(evidence/'go-parser-selftest.json',parser_selftest())
        save(evidence/'junit-parser-selftest.json', junit_selftest())
        save(evidence/'unittest-parser-selftest.json', unittest_selftest())
        approval=json.loads(Path(os.environ['KNOWHUB25_CI_APPROVAL_FILE']).read_text())
        require(approval.get('candidateSHA')==os.environ['GITHUB_SHA'],'External approval SHA mismatch')
        result['identity']=identity(candidate,approval)
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
                    required=list(M['required'].get(package,[]))
                    if label=='candidate':required+=M['candidate_required'].get(package,[])
                    record=parse_file(folder/'test.jsonl',cc,code,required) if code is not None else parse('',cc,code,required);record['compileSeparate']=mode=='normal'
                    record['candidateTestsNotPresent']=M['not_present_baseline'].get(package,[]) if label=='baseline' else []
                    group[mode+':'+package]=record;save(folder/'result.json',record);save(evidence/'comparison.json',result)
            if group['normal:internal/agent']['gatePass']:
                folder=evidence/label/'diagnostic_agent_shuffle';folder.mkdir(parents=True)
                code=execute(root,['go','test','-mod=readonly','-p','2','-count=1','-json','-shuffle=on','./internal/agent'],folder/'test.jsonl')
                required=M['required'].get('internal/agent',[])+(M['candidate_required'].get('internal/agent',[]) if label=='candidate' else [])
                record=parse_file(folder/'test.jsonl',0,code,required)
                record['diagnosticOnly']=True
                group['diagnostic:agent-shuffle']=record;save(folder/'result.json',record)
            else:
                group['diagnostic:agent-shuffle']={'gatePass':False,'status':'NOT_EXECUTED','reason':'normal Agent package did not pass'}
            if label == 'candidate':
                target = root/'internal/datasource/connector/notion/a4_ci_nested_http_test.go'
                require(not target.exists(), 'Notion overlay collision')
                shutil.copyfile(QA/'notion_nested_http_test.go', target)
                try:
                    for mode in ['normal', 'race']:
                        folder=evidence/label/('overlay_'+mode+'_notion');folder.mkdir(parents=True)
                        args=['go','test','-mod=readonly','-p','2','-count=1','-json']+(['-race'] if mode=='race' else [])+['./internal/datasource/connector/notion']
                        code=execute(root,args,folder/'test.jsonl')
                        required=M['candidate_required']['internal/datasource/connector/notion']+['TestA4CINestedHTTPFailure']
                        record=parse_file(folder/'test.jsonl',0,code,required)
                        record['compileSeparate']=False;record['testOnlyOverlay']=M['overlay'][-1];record['coverageScope']='CANDIDATE_ONLY_ADDITIONAL_GATE'
                        group['overlay-'+mode+':internal/datasource/connector/notion']=record;save(folder/'result.json',record)
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
            save(evidence/'comparison.json',result)
        result['moduleVerifyExit']=execute(candidate,['go','mod','verify'],evidence/'module-verify.log')
        result['pythonFreezeExit']=execute(candidate,[sys.executable,'-m','pip','freeze','--all'],evidence/'python-dependencies.log')
        ok=all(r['gatePass'] for group in result['groups'].values() for r in group.values()) and result['moduleVerifyExit']==0 and result['pythonFreezeExit']==0
        result['gate']='A5_LINUX_REVIEW_READY_PROPOSED' if ok else 'BLOCKED'
    except Exception as exc:
        result['errorClassification']=type(exc).__name__
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
            hits=sum(len(pattern.findall(text)) for pattern in patterns)
            status['files'].append(dict(path=p.relative_to(raw_root).as_posix(),rawSHA256=sha(raw),
                                        rawBytes=len(raw),scan='HIT' if hits else 'CLEAR',hits=hits))
        status['rawScanComplete']=True
        require(all(r['scan']=='CLEAR' for r in status['files']), 'Raw safety scan blocked')
        # A dependency/setup failure before Helper creates no comparison. Preserve only
        # safe hash/status metadata; installation logs remain on the controlled runner.
        comparison=json.loads((raw_root/'comparison.json').read_text())
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
    save(public_root/'safety-status.json',status)
    if status['approved']:
        copies={p.relative_to(public_root/'reviewed').as_posix():sha(p.read_bytes()) for p in (public_root/'reviewed').rglob('*') if p.is_file()}
        require(copies=={r['path']:r['uploadSHA256'] for r in status['files']},'Upload set mismatch')
    return status

if __name__=='__main__':
    if '--parser-selftest' in sys.argv: parser_selftest();print('parser selftest PASS')
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
