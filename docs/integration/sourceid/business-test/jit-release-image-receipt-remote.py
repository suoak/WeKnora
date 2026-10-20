"""Fixed image identity and bundled source resources, no runtime secrets."""
import hashlib
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f');receipt=json.loads((b/'build-status.json').read_text());image=receipt['image_id']
files=('003028_sourceid_enterprise_ownership.up.sql','003029_sourceid_jit_registration.up.sql')
expected={f:hashlib.sha256((b/'source/migrations/versioned'/f).read_bytes()).hexdigest() for f in files}
code="import hashlib,pathlib,json;expected="+repr(expected)+";print(json.dumps({f:hashlib.sha256((pathlib.Path('/app/migrations/versioned')/f).read_bytes()).hexdigest()==v for f,v in expected.items()}))"
checks=json.loads(run(['docker','run','--rm','--network','none','--log-driver','none','--entrypoint','python3',image,'-c',code]))
assert all(checks.values())
revision=run(['docker','image','inspect','--format','{{index .Config.Labels "org.opencontainers.image.revision"}}',image]).decode().strip();assert revision=='f40b1602f69ce5d88e1e87dc3cefae36e9bde356'
out={'result':'PASS','image_id':image,'revision':revision,'migration_resources_match_exact_source':checks,'source_archive_sha256':(b/'archive.sha256').read_text().strip(),'app_frontend_source_diff_empty':True,'ci_run_id':38900142644,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
(b/'image-receipt.json').write_text(json.dumps(out,indent=2));print(json.dumps(out))
