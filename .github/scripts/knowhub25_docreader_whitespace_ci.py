"""Bounded HTML/MHTML regression; raw output never leaves the private runner."""
import ast
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
QA = ROOT / '.github/qa/continuous'
spec = importlib.util.spec_from_file_location('contracts', QA / 'contracts.py')
C = importlib.util.module_from_spec(spec)
spec.loader.exec_module(C)
M = json.loads((ROOT / '.github/qa/docreader-whitespace.json').read_text())
PHASES = {'IDENTITY', 'INSTALL_UV', 'DEPENDENCIES', 'HTML', 'MHTML', 'COMPLETE'}
FAILURES = {'NONE', 'IDENTITY_FAILED', 'DEPENDENCY_FAILED', 'TEST_FAILED', 'PARSER_FAILED', 'RAW_SCAN_HIT', 'HELPER_FAILED'}
RULE_IDS = {'SYNTHETIC_%03d' % (i+1) for i in range(len(C.M['canaries']))} | {'BEARER_PATTERN', 'MCP_KEY_PATTERN', 'CREDENTIAL_ASSIGNMENT'}

def scan(raw):
    counts = dict.fromkeys(RULE_IDS, 0)
    for item in C.scan_findings(raw.decode('utf-8', errors='replace')):
        counts[item['ruleId']] = item['count']
    return counts

def sha(data):
    return hashlib.sha256(data).hexdigest()

def validate(summary):
    assert set(summary) == {'schema', 'sourceSHA', 'status', 'phase', 'failureClass', 'rows', 'rawFiles', 'rawUploadAllowed'}
    assert summary['schema'] == 1 and summary['phase'] in PHASES and summary['failureClass'] in FAILURES
    assert summary['status'] in {'PASS', 'BLOCKED'} and summary['rawUploadAllowed'] is False
    assert len(summary['sourceSHA']) == 40 and all(c in '0123456789abcdef' for c in summary['sourceSHA'])
    for row in summary['rows']:
        assert set(row) == {'suite', 'exit', 'run', 'pass', 'fail', 'error', 'skip', 'requiredPass', 'parserValid'}
        assert row['suite'] in {'HTML', 'MHTML'} and type(row['exit']) is int
        assert all(type(row[k]) is int and row[k] >= 0 for k in ['run', 'pass', 'fail', 'error', 'skip'])
        assert type(row['requiredPass']) is bool and type(row['parserValid']) is bool
    for f in summary['rawFiles']:
        assert set(f) == {'role', 'phase', 'sha256', 'rules'}
        assert f['role'] in {'PYTHON_OUTPUT', 'PYTHON_RESULT'} and f['phase'] in PHASES
        assert len(f['sha256']) == 64 and all(c in '0123456789abcdef' for c in f['sha256'])
        assert set(f['rules']) == RULE_IDS
        assert all(type(v) is int and v >= 0 for v in f['rules'].values())
    if summary['status'] == 'PASS':
        assert summary['phase'] == 'COMPLETE' and summary['failureClass'] == 'NONE'
        assert len(summary['rows']) == 2
        assert {r['suite'] for r in summary['rows']} == {'HTML','MHTML'}
        assert all(r['exit'] == 0 and r['parserValid'] and r['requiredPass'] and r['fail'] == r['error'] == r['skip'] == 0 and r['pass'] == r['run'] > 0 for r in summary['rows'])
        assert all(not any(f['rules'].values()) for f in summary['rawFiles'])
        assert len(summary['rawFiles']) == 6 and {(f['phase'],f['role']) for f in summary['rawFiles']} == {
            ('INSTALL_UV','PYTHON_OUTPUT'),('DEPENDENCIES','PYTHON_OUTPUT'),
            ('HTML','PYTHON_OUTPUT'),('HTML','PYTHON_RESULT'),
            ('MHTML','PYTHON_OUTPUT'),('MHTML','PYTHON_RESULT')}

def selftest():
    assert len(C.unittest_selftest()) == 15
    valid = {'schema': 1, 'sourceSHA': '0'*40, 'status': 'BLOCKED', 'phase': 'IDENTITY', 'failureClass': 'HELPER_FAILED', 'rows': [], 'rawFiles': [], 'rawUploadAllowed': False}
    validate(valid)
    for field,value in [('rawUploadAllowed',True), ('phase','raw exception'), ('failureClass','token=synthetic_fixture_value')]:
        bad = dict(valid); bad[field] = value
        try: validate(bad)
        except AssertionError: pass
        else: raise AssertionError('unsafe summary accepted')
    assert C.scan_findings('token=synthetic_fixture_value')
    assert C.scan_findings('Bearer synthetic_fixture_value')
    complete = dict(valid,status='PASS',phase='COMPLETE',failureClass='NONE',rows=[
        dict(suite=s,exit=0,run=1,**{'pass':1,'fail':0,'error':0,'skip':0},requiredPass=True,parserValid=True) for s in ['HTML','MHTML']],rawFiles=[
        dict(role=role,phase=phase,sha256='0'*64,rules=dict.fromkeys(RULE_IDS,0)) for phase,role in [
            ('INSTALL_UV','PYTHON_OUTPUT'),('DEPENDENCIES','PYTHON_OUTPUT'),('HTML','PYTHON_OUTPUT'),('HTML','PYTHON_RESULT'),('MHTML','PYTHON_OUTPUT'),('MHTML','PYTHON_RESULT')]])
    validate(complete)
    for name in ['missing-evidence','raw-hit','required-missing','skip','unexpected-field']:
        bad=json.loads(json.dumps(complete))
        if name=='missing-evidence': bad['rawFiles'].pop()
        elif name=='raw-hit': bad['rawFiles'][0]['rules']['CREDENTIAL_ASSIGNMENT']=1
        elif name=='required-missing': bad['rows'][0]['requiredPass']=False
        elif name=='skip': bad['rows'][0]['skip']=1
        else: bad['exception']='raw cause'
        try: validate(bad)
        except AssertionError: pass
        else: raise AssertionError('incomplete or unsafe gate accepted')
    print('UNITTEST_15_SCHEMA_8_SCAN_2_PASS')

def run():
    os.umask(0o077)
    temp = Path(os.environ['RUNNER_TEMP'])
    private = temp / 'docreader-whitespace-private'; private.mkdir(mode=0o700)
    public = temp / 'docreader-whitespace-reviewed'; public.mkdir(mode=0o700)
    head = subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT).decode().strip()
    summary = dict(schema=1,sourceSHA=head,status='BLOCKED',phase='IDENTITY',failureClass='HELPER_FAILED',rows=[],rawFiles=[],rawUploadAllowed=False)
    def execute(phase, command, role='PYTHON_OUTPUT'):
        summary['phase'] = phase
        path = private / (phase+'.log')
        with path.open('wb') as log:
            code = subprocess.run(command,cwd=ROOT,stdout=log,stderr=subprocess.STDOUT,timeout=1200).returncode
        raw = path.read_bytes(); findings = scan(raw)
        summary['rawFiles'].append(dict(role=role,phase=phase,sha256=sha(raw),rules=findings))
        if any(findings.values()): summary['failureClass']='RAW_SCAN_HIT'; raise RuntimeError()
        return code
    try:
        assert os.environ['GITHUB_REPOSITORY'] == 'suoak/WeKnora'
        assert os.environ['GITHUB_REF'] == 'refs/heads/integration/knowhub25-docreader-whitespace-20261012'
        assert head == os.environ['GITHUB_SHA']
        assert not subprocess.check_output(['git','status','--porcelain'],cwd=ROOT)
        for path,ident in M['files'].items():
            assert sha((ROOT/path).read_bytes()) == ident['sha256']
            assert subprocess.check_output(['git','rev-parse','HEAD:'+path],cwd=ROOT).decode().strip() == ident['blob']
        summary['failureClass']='DEPENDENCY_FAILED'
        assert execute('INSTALL_UV',[sys.executable,'-m','pip','install','--disable-pip-version-check','uv==0.12.15']) == 0
        envdir = private / 'venv'
        os.environ['UV_PROJECT_ENVIRONMENT'] = str(envdir)
        os.environ['UV_CACHE_DIR'] = str(private/'uv-cache')
        assert execute('DEPENDENCIES',[sys.executable,'-m','uv','sync','--project','docreader','--frozen','--no-dev','--python',sys.executable]) == 0
        for phase,pattern in [('HTML','test_html_parser.py'),('MHTML','test_mhtml_parser.py')]:
            result = private/(phase+'.json')
            summary['failureClass']='TEST_FAILED'
            code = execute(phase,[str(envdir/'bin/python'),'-B',str(QA/'contracts.py'),'--unittest-run','docreader/tests',pattern,str(result)])
            raw = result.read_bytes(); findings = scan(raw)
            summary['rawFiles'].append(dict(role='PYTHON_RESULT',phase=phase,sha256=sha(raw),rules=findings))
            if any(findings.values()): summary['failureClass']='RAW_SCAN_HIT'; raise RuntimeError()
            parsed = C.parse_unittest(result,code)
            lifecycle = json.loads(raw)
            ids = {r['id'] for r in lifecycle['testcases'] if r['status']=='pass'}
            required = set(M['required'][phase]); counts=lifecycle['counts']
            summary['rows'].append(dict(suite=phase,exit=code,run=lifecycle['run'],**{'pass':counts['pass'],'fail':counts['fail'],'error':counts['error'],'skip':counts['skip']},requiredPass=required <= ids,parserValid=parsed['gatePass']))
            assert parsed['gatePass'] and required <= ids
        summary.update(status='PASS',phase='COMPLETE',failureClass='NONE')
    except Exception:
        pass  # Never publish exception strings or raw provider output.
    validate(summary)
    (public/'summary.json').write_text(json.dumps(summary,indent=2)+'\n',encoding='utf-8')
    print(json.dumps({'status':summary['status'],'phase':summary['phase'],'failureClass':summary['failureClass']}))
    return 0 if summary['status']=='PASS' else 1

if __name__ == '__main__':
    if sys.argv[1:] == ['--self-test']: selftest()
    else: sys.exit(run())
