"""Full frontend unit/type regression; publish reviewed counts, never raw logs."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import xml.etree.ElementTree as ET

RULES = {
    'BEARER_PATTERN': re.compile(r'(?i)bearer[ \t]+(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}'),
    'MCP_KEY_PATTERN': re.compile(r'(?<![A-Za-z0-9_])mcp_[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])'),
    'CREDENTIAL_ASSIGNMENT': re.compile(r'''(?i)["']?(?:authorization|cookie|api_key|secret|token)["']?\s*[:=]\s*["']?(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}'''),
}
POLICY = json.loads((Path(__file__).resolve().parents[1]/'qa/markdown-safety.json').read_bytes())
RULES.update({'SYNTHETIC_%03d' % (i+1): re.compile(re.escape(value)) for i,value in enumerate(POLICY['canaries'])})


def parse_junit(path, exit_code, required=()):
    out = {'run':None,'passed':None,'failed':None,'skipped':None,'state':'BLOCKED','exit':exit_code}
    try:
        tree=ET.parse(path)
        cases=tree.findall('.//testcase')
        if not cases or tree.getroot().tag not in ('testsuite','testsuites'):
            return out
        bad=sum(any(c.find(n) is not None for n in ('failure','error')) for c in cases)
        skip=sum(c.find('skipped') is not None for c in cases)
        names=[c.get('name','') for c in cases]
        if not all(names):
            return out
        out.update(run=len(cases),passed=len(cases)-bad-skip,failed=bad,skipped=skip)
        diagnostics=[]
        browser_name='human handoff expands, stays reachable on narrow screens, focuses the browser, and collapses after completion'
        for case in cases:
            node=case.find('failure')
            if node is None: node=case.find('error')
            if node is None: continue
            if case.get('name') != browser_name:
                diagnostics.append({'testId':'OTHER_FIXED_TEST','category':'UNKNOWN','sourceLines':[]})
                continue
            text=' '.join(node.itertext())
            category='UNKNOWN'
            for marker,kind in [('browserType.launch','BROWSER_LAUNCH_FAILED'),('ERR_MODULE','MODULE_FAILED'),('TimeoutError','BROWSER_TIMEOUT'),('AssertionError','ASSERTION_FAILED')]:
                if marker in text: category=kind; break
            lines=sorted(set(int(v) for v in re.findall(r'BrowserTaskPreview\.browser\.test\.mjs:(\d+)',text)))
            diagnostics.append({'testId':'BROWSER_PREVIEW_HANDOFF','category':category,'sourceLines':lines})
        out['diagnostics']=diagnostics
        if exit_code==0 and bad==skip==0 and all(name in names for name in required):
            out['state']='PASS'
    except (ET.ParseError, OSError, ValueError):
        pass
    return out


def run():
    os.umask(0o077)
    base=Path(os.environ['RUNNER_TEMP'])/'knowhub25-markdown-private'
    base.mkdir(mode=0o700,exist_ok=False)
    public=Path(os.environ['RUNNER_TEMP'])/'knowhub25-markdown-reviewed'
    public.mkdir(mode=0o700,exist_ok=False)
    summary={'schemaVersion':1,'sourceSHA':os.environ['GITHUB_SHA'],'rows':[],
             'rawUploadPermitted':False,'gatePass':False,'failureClass':'UNKNOWN'}
    try:
        assert subprocess.check_output(['node','--version']).strip()==b'v24.10.0'
        for side in ('BASELINE','CANDIDATE'):
            cwd=Path(side.lower())/'frontend'
            before=(cwd/'package-lock.json').read_bytes()
            folder=base/side
            folder.mkdir(mode=0o700)
            def execute(command, stage):
                out,err=folder/(stage+'.log'),folder/(stage+'.stderr.log')
                with out.open('wb') as o, err.open('wb') as e:
                    result=subprocess.run(command,cwd=cwd,stdout=o,stderr=e,timeout=900)
                return result.returncode
            install=execute(['npm','ci','--no-audit','--no-fund'],'INSTALL')
            row={'side':side,'installExit':install,'browserInstallExit':None,'typeExit':None,'unit':None,'files':[],'state':'BLOCKED'}
            if install==0:
                row['typeExit']=execute(['npm','run','type-check'],'TYPE_CHECK')
                row['browserInstallExit']=execute(['node','node_modules/playwright/cli.js','install','chromium'],'BROWSER_INSTALL')
                browser_module=(cwd/'node_modules/playwright/index.mjs').resolve()
                with (folder/'BROWSER_IDENTITY.log').open('wb') as output, (folder/'BROWSER_IDENTITY.stderr.log').open('wb') as errors:
                    located=subprocess.run(['node','--input-type=module','-e',"import { chromium } from 'playwright'; process.stdout.write(chromium.executablePath());"],cwd=cwd,stdout=output,stderr=errors,timeout=30)
                browser_path=(folder/'BROWSER_IDENTITY.log').read_text(encoding='utf-8')
                if row['browserInstallExit']!=0 or located.returncode!=0 or not browser_module.is_file() or not Path(browser_path).is_file():
                    raise ValueError
                os.environ['BROWSERSKILL_TEST_PLAYWRIGHT']=str(browser_module)
                os.environ['BROWSERSKILL_TEST_CHROMIUM']=browser_path
                xml=(folder/'unit.xml').resolve()
                code=execute(['npm','test','--','--test-concurrency=1','--test-reporter=junit','--test-reporter-destination='+str(xml)],'UNIT')
                required = POLICY['requiredTests'] if side=='CANDIDATE' else ()
                row['unit']=parse_junit(xml,code,required)
                row['newNecessaryTests']='VERIFIED' if side=='CANDIDATE' and row['unit']['state']=='PASS' else 'NOT_PRESENT' if side=='BASELINE' else 'NOT_VERIFIED'
                if row['browserInstallExit']==0 and row['typeExit']==0 and row['unit']['state']=='PASS':
                    row['state']='PASS'
            assert (cwd/'package-lock.json').read_bytes()==before
            for n,path in enumerate(sorted(folder.iterdir())):
                if not path.is_file():
                    raise ValueError
                data=path.read_bytes()
                text=data.decode('utf-8',errors='strict')
                findings=[{'ruleId':name,'count':len(pattern.findall(text))} for name,pattern in RULES.items() if pattern.search(text)]
                row['files'].append({'fileId':'FILE_%03d'%(n+1),'role':'FRONTEND_JUNIT_XML' if path.suffix=='.xml' else 'FRONTEND_OUTPUT',
                                     'sha256':hashlib.sha256(data).hexdigest(),'findings':findings})
                if findings:
                    row['state']='BLOCKED'
            summary['rows'].append(row)
        summary['gatePass']=len(summary['rows'])==2 and all(row['state']=='PASS' for row in summary['rows'])
        summary['failureClass']='NONE' if summary['gatePass'] else 'TEST_OR_RAW_SCAN_BLOCKED'
    except Exception:
        summary['failureClass']='HELPER_FAILED'
    assert re.fullmatch('[0-9a-f]{40}',summary['sourceSHA'])
    (public/'summary.json').write_text(json.dumps(summary,indent=2)+'\n',encoding='utf-8')
    return 0 if summary['gatePass'] else 1


def self_test():
    import tempfile
    with tempfile.TemporaryDirectory() as tmp:
        p=Path(tmp)/'unit.xml'
        p.write_text('<testsuites><testsuite><testcase name="controlled"/></testsuite></testsuites>')
        assert parse_junit(p,0)['state']=='PASS'
        assert parse_junit(p,1)['state']=='BLOCKED'
        assert parse_junit(p,0,['missing necessary test'])['state']=='BLOCKED'
        for xml in ('<testsuites/>','<testsuites><testcase name="controlled"><skipped/></testcase></testsuites>',
                    '<testsuites><testcase name="controlled"><failure/></testcase></testsuites>','<testsuites>'):
            p.write_text(xml)
            assert parse_junit(p,0)['state']=='BLOCKED'
    assert RULES['CREDENTIAL_ASSIGNMENT'].search('token=synthetic-fixture-value')
    assert RULES['CREDENTIAL_ASSIGNMENT'].search('packagefile go/token=/tmp/archive.a')
    print('MARKDOWN_INFRA_SELF_TEST_PASS')


if __name__=='__main__':
    if sys.argv[1:]==['--self-test']:
        self_test()
    else:
        sys.exit(run())
