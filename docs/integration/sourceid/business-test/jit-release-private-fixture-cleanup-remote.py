"""Remove only generated disposable fixture copies; preserve all backups/data."""
import shutil
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f');private=b/'private'
assert json.loads((b/'final-result.json').read_text())['result']=='PASS'
assert json.loads((b/'jit-revoke-result.json').read_text())['result']=='PASS'
removed=[]
for name in ('mock-fixture-pilot','mock-fixture-enterprise_all','mock-fixture-jit','capacity-fixture'):
 target=private/name
 if target.exists():
  assert not target.is_symlink();target.resolve().relative_to(private.resolve())
  assert target.is_dir();shutil.rmtree(target);removed.append(name)
for name in ('mock-runtime.env','validation-runtime.env','test.env','pg-scale.env'):
 target=private/name
 if target.exists():
  assert not target.is_symlink();target.resolve().relative_to(private.resolve())
  assert target.is_file();target.unlink();removed.append(name)
out={'result':'PASS','removed_generated_fixture_names':removed,'protected_backup_preserved':(private/'before-upgrade/database3028.dump').is_file() and (private/'before-upgrade/files.tar').is_file(),'live_environment_untouched':True,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
(b/'private-fixture-cleanup.json').write_text(json.dumps(out,indent=2));print(json.dumps(out))
