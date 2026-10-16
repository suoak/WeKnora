"""Tighten only this rehearsal's protected artifacts; never touch original volumes."""
base=pathlib.Path('/home/aqyfzx/ai/knowhub-legacy79-rehearsal-20261016').resolve()
private=(base/'private').resolve();assert private.parent==base
private.chmod(0o700)
files=directories=0
for p in private.rglob('*'):
    assert not p.is_symlink();p.resolve().relative_to(private)
    if p.is_dir():p.chmod(0o700);directories+=1
    elif p.is_file():p.chmod(0o600);files+=1
assert not run(['docker','ps','-aq','--filter','name=^oa-legacy79-']).strip()
print(json.dumps({'private_directory_mode':'0700','private_file_mode':'0600','protected_files':files,'protected_subdirectories':directories,'temporary_rehearsal_containers_remaining':0,'result':'PASS'}))
