"""One approved existing account only. Dry-run exclusively; no --apply path."""
import argparse,json,os,pathlib,subprocess,datetime,re
BASE=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017')
IMAGE='sha256:742c7ce56ef38ad32548900c8e7f8b264f9f6cd6138043674b91b27557443413'
FIELDS={'issuer','employee_no','email','user_id','ownership_ref','source_ref','policy_ref','status','revision','verified_at','valid_until'}
def read_protected(path):
    path=pathlib.Path(path)
    if path.is_symlink() or not path.is_file() or path.stat().st_uid!=os.getuid() or path.stat().st_mode&0o077 or path.stat().st_size>65536:raise ValueError('protected_input_required')
    def unique(pairs):
        row={}
        for k,v in pairs:
            if k in row:raise ValueError('duplicate_input_key')
            row[k]=v
        return row
    return json.loads(path.read_text(encoding='utf-8'),object_pairs_hook=unique)
def validate(row,selection,now):
    if set(row)!=FIELDS or row['issuer']!='https://sid.ruijie.com.cn' or row['user_id']!=selection['existing_user_id'] or row['status']!='active':raise ValueError('approved_single_account_required')
    if type(row['revision']) is not int or row['revision']<1:raise ValueError('source_revision_required')
    for k in ('employee_no','email','ownership_ref','source_ref','policy_ref'):
        if not isinstance(row[k],str) or not row[k].strip() or any(ord(c)<32 for c in row[k]):raise ValueError('source_evidence_required')
    for k in ('ownership_ref','source_ref','policy_ref'):
        if any(marker in row[k].lower() for marker in ('synthetic','mock','placeholder','todo')):raise ValueError('real_evidence_required')
    def date(key):
        value=datetime.datetime.fromisoformat(row[key].replace('Z','+00:00'))
        if value.tzinfo is None:raise ValueError('timezone_required')
        return value
    if date('verified_at')>now or date('valid_until')<=now or date('valid_until')<=date('verified_at'):raise ValueError('current_source_validity_required')
def main(argv=None):
    p=argparse.ArgumentParser();p.add_argument('--protected-row',type=pathlib.Path);p.add_argument('--actor');a=p.parse_args(argv)
    if a.protected_row is None:
        print(json.dumps({'result':'NOT_EXECUTED','reason':'INDEPENDENT_RJGH_HISTORY_EVIDENCE_NOT_SUPPLIED','apply_available':False}));return 0
    if not a.actor or not re.fullmatch('[A-Za-z0-9_.:-]{1,80}',a.actor):raise ValueError('approved_operator_reference_required')
    selected=read_protected(BASE/'private/first-account-preparation/selected-account.json');row=read_protected(a.protected_row)
    validate(row,selected,datetime.datetime.now(datetime.timezone.utc))
    code="import os,subprocess,urllib.parse,sys;env=dict(os.environ);env['SOURCEID_OWNERSHIP_DSN']='postgres://clean_owner:'+urllib.parse.quote(env['DB_PASSWORD'],safe='')+'@127.0.0.1:5432/'+env['DB_NAME']+'?sslmode=disable';z=subprocess.run(['/app/sourceid-ownership','--actor',sys.argv[1]],input=sys.stdin.buffer.read(),env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE);sys.stdout.buffer.write(z.stdout);raise SystemExit(z.returncode)"
    z=subprocess.run(['docker','--host','unix:///var/run/docker.sock','run','--rm','-i','--network','container:knowhub-company-test-postgres-1','--log-driver','none','--env-file',str(BASE/'green/private/app.env'),'--entrypoint','python3',IMAGE,'-c',code,a.actor],input=(json.dumps(row)+'\n').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=45)
    if z.returncode:raise RuntimeError('controlled_dryrun_rejected')
    value=json.loads(z.stdout)
    if set(value)!={'rows','changed','unchanged','rejected','applied'} or value['rows']!=1 or value['applied'] is not False or any(type(value[k]) is not int for k in ('rows','changed','unchanged','rejected')):raise RuntimeError('dryrun_receipt_rejected')
    print(json.dumps({'result':'PASS' if value['rejected']==0 else 'FAIL','summary':value,'apply_available':False,'new_sid_requests':0}));return 0
if __name__=='__main__':
    try:raise SystemExit(main())
    except Exception:print(json.dumps({'result':'FAIL','reason':'CONTROLLED_DRYRUN_REJECTED','raw_error_saved':False}));raise SystemExit(2)
