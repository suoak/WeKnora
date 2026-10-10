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

QA = Path(__file__).resolve().parents[1] / 'qa/a3-wiki'
M = json.loads((QA / 'manifest.json').read_text())
BASE = M['baseline']
PACKAGES = ['internal/middleware', 'internal/mcpserver', 'internal/application/service',
            'internal/application/repository', 'internal/application/access', 'internal/types',
            'internal/handler', 'internal/router', 'internal/mcp', 'internal/agent/tools', 'internal/agent']
RACE = ['internal/mcp', 'internal/mcpserver', 'internal/middleware', 'internal/agent/tools',
        'internal/agent', 'internal/application/repository', 'internal/application/service', 'internal/handler']
CI = {'.github/scripts/knowhub25_a3_wiki_ci.py', '.github/workflows/knowhub25-a3-wiki-linux.yml',
      '.github/qa/a3-wiki/manifest.json', '.github/qa/a3-wiki/python-linux.lock'}

def sha(b): return hashlib.sha256(b).hexdigest()
def git(root, *args): return subprocess.check_output(['git', '-C', str(root), *args])
def require(ok, reason):
    if not ok: raise RuntimeError(reason)
def save(p, value): p.write_text(json.dumps(value, indent=2))

def identity(root, expected, tree):
    require(git(root, 'rev-parse', 'HEAD').decode().strip() == expected, 'SHA mismatch')
    require(git(root, 'rev-parse', 'HEAD^').decode().strip() == BASE, 'Parent mismatch')
    require(git(root, 'rev-parse', 'HEAD^{tree}').decode().strip() == tree, 'Tree mismatch')
    require(not git(root, 'status', '--porcelain', '--untracked-files=all'), 'Dirty source')
    files = sorted(r['path'] for r in M['source'])
    require(set(git(root, 'diff', '--name-only', BASE, 'HEAD').decode().splitlines()) == set(files) | CI, 'Whitelist mismatch')
    for r in M['source']:
        require(sha((root/r['path']).read_bytes()) == r['sha256'], 'Source bytes mismatch')
        require(git(root, 'rev-parse', 'HEAD:' + r['path']).decode().strip() == r['blob'], 'Source blob mismatch')
    require(sha(git(root, 'diff', '--binary', '--abbrev=9', BASE, 'HEAD', '--', *files)) == M['patch'], 'Patch mismatch')
    for p, blob in M['protected'].items():
        require(git(root, 'rev-parse', 'HEAD:' + p).decode().strip() == blob, 'Protected blob mismatch')
    return {'sha': expected, 'parent': BASE, 'tree': tree, 'patch': M['patch'], 'files': files, 'ci': sorted(CI)}

def parse(log, compiled, exit_code, required):
    events = []
    for line in log.splitlines():
        try: events.append(json.loads(line))
        except ValueError: pass
    names = {a: {r['Test'] for r in events if r.get('Action') == a and r.get('Test')} for a in ['run', 'pass', 'fail', 'skip']}
    missing = set(required) - names['pass']
    package_pass = any(r.get('Action') == 'pass' and not r.get('Test') for r in events)
    ok = compiled == 0 and exit_code == 0 and package_pass and bool(names['run']) and not names['fail'] and not missing and 'WARNING: DATA RACE' not in log
    return dict(gatePass=ok, compileExit=compiled, testExit=exit_code, run=len(names['run']), passed=len(names['pass']),
                fail=sorted(names['fail']), skip=sorted(names['skip']), missing=sorted(missing), requiredPassed=sorted(set(required) & names['pass']))

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
    with log.open('wb') as f: return subprocess.run(args, cwd=root, stdout=f, stderr=subprocess.STDOUT, env=env).returncode

def parser_selftest():
    junit_selftest()
    valid='\n'.join(json.dumps(r) for r in [{'Action':'run','Test':'TestNecessary'},{'Action':'pass','Test':'TestNecessary'},{'Action':'pass'}])
    require(parse(valid,0,0,['TestNecessary'])['gatePass'], 'positive parser failed')
    for compiled, code, text, req in [(1,None,'',[]),(0,0,valid,['TestMissing']),(0,1,valid,[]),(0,0,valid+'\nWARNING: DATA RACE',[]),(0,0,'',[])]:
        require(not parse(text,compiled,code,req)['gatePass'], 'negative parser accepted')

def main():
    workspace=Path(os.environ['GITHUB_WORKSPACE']);evidence=workspace/'evidence';evidence.mkdir(exist_ok=True)
    candidate=workspace/'candidate';baseline=workspace/'baseline';result={'gate':'BLOCKED','groups':{},'notProductionGate':True}
    try:
        parser_selftest()
        save(evidence/'junit-parser-selftest.json', junit_selftest())
        expected=os.environ['GITHUB_SHA'];tree=git(candidate,'rev-parse',expected+'^{tree}').decode().strip()
        result['identity']=identity(candidate,expected,tree)
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
                    text=(folder/'test.jsonl').read_text(errors='replace') if code is not None else ''
                    required=list(M['required'].get(package,[]))
                    if label=='candidate':required+=M['candidate_required'].get(package,[])
                    record=parse(text,cc,code,required);record['compileSeparate']=mode=='normal'
                    group[mode+':'+package]=record;save(folder/'result.json',record);save(evidence/'comparison.json',result)
            require(not git(root,'status','--porcelain'),'Source mutated after module tests')
            pyenv=dict(os.environ,PYTHONPATH=str(root/'mcp-server'),PYTHONDONTWRITEBYTECODE='1')
            for name,args in [('unittest',[sys.executable,'-m','unittest','discover','-v','-s','mcp-server','-p','test_*.py']),
                              ('pytest',[sys.executable,'-m','pytest','-q','mcp-server/tests','--junitxml='+str(evidence/label/'pytest.xml')])]:
                log=evidence/label/(name+'.log');code=execute(root,args,log,pyenv);text=log.read_text(errors='replace')
                if name == 'pytest':
                    group['python:'+name] = parse_pytest(evidence/label/'pytest.xml', code)
                    save(evidence/label/'pytest-testcases.json', group['python:'+name])
                else:
                    count=re.search(r'Ran (\d+) tests?',text)
                    group['python:'+name]={'exit':code,'runOrPassed':int(count.group(1)) if count else 0,'gatePass':code==0 and bool(count) and int(count.group(1))>0}
            save(evidence/'comparison.json',result)
        result['moduleVerifyExit']=execute(candidate,['go','mod','verify'],evidence/'module-verify.log')
        result['pythonFreezeExit']=execute(candidate,[sys.executable,'-m','pip','freeze','--all'],evidence/'python-dependencies.log')
        ok=all(r['gatePass'] for group in result['groups'].values() for r in group.values()) and result['moduleVerifyExit']==0
        result['gate']='A3_WIKI_LINUX_GATE_PASS_PROPOSED' if ok else 'BLOCKED'
    except Exception as exc:
        result['error']=str(exc)
    finally:
        findings=[]
        pattern=(r'(?<![A-Za-z0-9_])mcp_[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])|'
                 r'(?i:authorization[\"\s:=]+bearer[ \t]+)(?!\[REDACTED)[A-Za-z0-9_.~-]{12,}|'
                 + '|'.join(re.escape(x) for x in M['canaries']))
        for p in evidence.rglob('*'):
            if p.suffix in {'.log','.jsonl','.txt','.xml'}:
                text=p.read_text(errors='replace')
                if re.search(pattern,text):
                    findings.append({'path':str(p.relative_to(evidence)),'rawSHA256':sha(p.read_bytes()),'matches':len(re.findall(pattern,text))})
                    p.write_text(re.sub(pattern,'[REDACTED_SYNTHETIC_CREDENTIAL]',text));result['gate']='BLOCKED'
        result['credentialFindings']=findings
        save(evidence/'comparison.json',result)
        save(evidence/'artifact-sha256.json',{str(p.relative_to(evidence)):sha(p.read_bytes()) for p in evidence.rglob('*') if p.is_file() and p.suffix in {'.log','.jsonl','.txt','.json','.xml'} and p.name!='artifact-sha256.json'})
    return 0 if result['gate']=='A3_WIKI_LINUX_GATE_PASS_PROPOSED' else 1

if __name__=='__main__':
    if '--parser-selftest' in sys.argv: parser_selftest();print('parser selftest PASS')
    elif '--identity-only' in sys.argv:
        print(json.dumps(identity(Path(sys.argv[2]),sys.argv[3],sys.argv[4]),indent=2))
    else: sys.exit(main())
