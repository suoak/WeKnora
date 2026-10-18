"""Run recovery from the exact deployment payload, with no Docker/network."""
import ast
import json
import tempfile
import unittest
import sys
import types
import importlib.util
from unittest.mock import patch
from pathlib import Path


class AdapterTests(unittest.TestCase):
    def fixture(self, root, fail=None):
        tree=ast.parse(Path(__file__).with_name('jit-fresh-real-window-launch.py').read_text(encoding='utf-8'))
        code=next(ast.literal_eval(n.value) for n in tree.body if isinstance(n,ast.Assign) and any(isinstance(t,ast.Name) and t.id=='module' for t in n.targets))
        ns={}
        # Windows has no fcntl. Fake only the lock in this process-local test;
        # Linux deployment uses the actual kernel flock.
        modules={} if importlib.util.find_spec('fcntl') else {'fcntl':types.SimpleNamespace(flock=lambda *a:None,LOCK_EX=1)}
        with patch.dict(sys.modules,modules):exec(compile(code,'runtime-under-test','exec'),ns)
        r=Path(root);w=r/'real-window';w.mkdir();(r/'private').mkdir()
        config=r/'gateway.conf';config.write_text('candidate')
        (w/'original-gateway.conf').write_text('original');(w/'fresh-before.env').write_text('CLOSED=true\n')
        ns.update(R=r,W=w,CONFIG=config)
        calls=[];state={'connected':True}
        def cmd(args,**kwargs):
            calls.append(args)
            if fail=='restart' and args[:2]==['docker','restart']:raise RuntimeError('private detail must not escape')
            if args[:3]==['docker','network','disconnect']:state['connected']=False
            return b''
        def inspect(name,field):
            if field=='.NetworkSettings.Networks':return {'knowhub-jit-fresh_isolated':{}} if state['connected'] else {}
            if field=='.State.Paused':return False
            raise AssertionError('unexpected inspect')
        def compose():
            calls.append(['compose_fresh_only'])
            if fail=='close':raise RuntimeError('private close error')
        ns.update(cmd=cmd,inspect=inspect,compose_app=compose,
                  env=lambda n:{'OIDC_AUTH_ENABLE':'false','SOURCEID_JIT_ENABLE':'false','SOURCEID_PROTOCOL_ONLY':'true'},
                  fresh_meta=lambda:{'active_sessions':0,'active_access':0},
                  jq=lambda q,fresh=True:[] if 'json_agg' in q else {'documents':3,'chunks':2717,'vectors':2717},
                  sql=lambda q,*a,**kw:'3030|false' if 'schema_migrations' in q else '0',
                  health=lambda marker: calls.append(['health',marker]))
        return ns,calls,w,config

    def test_exact_adapter_restores_without_alias_collision(self):
        with tempfile.TemporaryDirectory() as d:
            ns,calls,w,config=self.fixture(d);ns['restore']('TEST')
            self.assertEqual(json.loads((w/'recovery.json').read_text())['result'],'RESTORED')
            self.assertEqual(config.read_text(),'original')
            disconnect=next(i for i,a in enumerate(calls) if a[:3]==['docker','network','disconnect'])
            restart=next(i for i,a in enumerate(calls) if a[:2]==['docker','restart'])
            health=next(i for i,a in enumerate(calls) if a[0]=='health')
            self.assertLess(disconnect,restart);self.assertLess(restart,health)
            before=len(calls);ns['restore']('WATCHDOG');self.assertEqual(len(calls),before)

    def test_close_failure_still_restores_gateway_and_alarms(self):
        with tempfile.TemporaryDirectory() as d:
            ns,calls,w,config=self.fixture(d,'close');ns['restore']('TEST')
            result=json.loads((w/'recovery.json').read_text())
            self.assertEqual(result['result'],'MANUAL_RECOVERY_REQUIRED')
            self.assertEqual(config.read_text(),'original')
            self.assertTrue(any(a[:2]==['docker','restart'] for a in calls))
            self.assertNotIn('private',json.dumps(result))

    def test_gateway_restart_failure_is_not_success(self):
        with tempfile.TemporaryDirectory() as d:
            ns,calls,w,_=self.fixture(d,'restart');ns['restore']('TEST')
            result=json.loads((w/'recovery.json').read_text())
            self.assertEqual(result['result'],'MANUAL_RECOVERY_REQUIRED')
            self.assertEqual(result['steps']['restore_gateway_and_drain_connections'],'FAIL')


if __name__=='__main__':unittest.main()
