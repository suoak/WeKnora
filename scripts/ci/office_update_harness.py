"""Update only our owned acceptance script, then execute it; no product changes."""
import stat
from office_test_remote import ROOT,REMOTE_ROOT,connect,execute
with connect() as client:
    with client.open_sftp() as sftp:
        path=REMOTE_ROOT+'/office_site_acceptance.py'
        row=sftp.lstat(path);assert stat.S_ISREG(row.st_mode) and row.st_uid==1000
        temporary=path+'.pending'
        with sftp.open(temporary,'wx') as f:f.write((ROOT/'scripts/ci/office_site_acceptance.py').read_bytes())
        sftp.chmod(temporary,0o600);sftp.posix_rename(temporary,path)
    raise SystemExit(execute(client,'import runpy;runpy.run_path('+repr(path)+',run_name="__main__")',600))
