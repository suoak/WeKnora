"""Run and collect the prepared real-site harness over the existing SSH channel."""
from office_test_remote import connect,execute,REMOTE_ROOT
CODE='import runpy;runpy.run_path('+repr(REMOTE_ROOT+'/office_site_acceptance.py')+',run_name="__main__")'
if __name__=='__main__':
    with connect() as client:raise SystemExit(execute(client,CODE,600))
