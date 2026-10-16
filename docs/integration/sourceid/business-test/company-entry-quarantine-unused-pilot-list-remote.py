"""Quarantine copied pilot-list files only in green; no identity parsing."""
import shutil,os
B=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017');folder=B/'green/private/ownership';archive=B/'private/unused-pilot-files';archive.mkdir(mode=0o700)
assert folder.is_dir() and not folder.is_symlink()
count=0
for f in list(folder.iterdir()):
 assert f.is_file() and not f.is_symlink();shutil.move(str(f),str(archive/f.name));count+=1
assert not list(folder.iterdir())
env=environment('knowhub-company-test-app-1');assert env['SOURCEID_ACCOUNT_SCOPE']=='enterprise_all' and env['OIDC_AUTH_ENABLE']=='false'
out={'result':'PASS','copied_pilot_list_files_quarantined':count,'new_ownership_mount_empty':True,'original_pilot_files_changed':False,'contents_read':False};(B/'pilot-list-quarantine-result.json').write_text(json.dumps(out));print(json.dumps(out))
