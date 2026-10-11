"""Exact parser/scanner functions extracted from fixed P2-R3 helper; no product code."""
import hashlib,json,os,re,subprocess,sys,tempfile
import xml.etree.ElementTree as ET
from pathlib import Path
M=json.loads((Path(__file__).resolve().parent/"manifest.json").read_text())
def require(ok, reason):
 if not ok: raise RuntimeError(reason)
def save(p, value):
 p.write_text(json.dumps(value,indent=2),encoding="utf-8")
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

def scan_findings(text):
    # Exact original rules, now with fixed safe identifiers. No snippets captured.
    rules=[('SYNTHETIC_%03d'%(i+1),re.compile(re.escape(x))) for i,x in enumerate(M['canaries'])]
    rules += [('BEARER_PATTERN',re.compile(r'(?i)bearer[ \t]+(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}')),
      ('MCP_KEY_PATTERN',re.compile(r'(?<![A-Za-z0-9_])mcp_[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])')),
      ('CREDENTIAL_ASSIGNMENT',re.compile(r'(?i)[\"\']?(?:authorization|cookie|api_key|secret|token)[\"\']?\s*[:=]\s*[\"\']?(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}'))]
    return [dict(ruleId=name,count=len(pattern.findall(text))) for name,pattern in rules if pattern.search(text)]

if __name__ == "__main__":
 try:
  if sys.argv[1] != "--unittest-run": raise RuntimeError("ENTRY_FAILED")
  sys.exit(run_unittest(sys.argv[2],sys.argv[3],Path(sys.argv[4])))
 except Exception:
  sys.exit(1)
