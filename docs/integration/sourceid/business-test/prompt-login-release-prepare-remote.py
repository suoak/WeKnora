"""Prepare APP-only immutable pins, never invokes Compose or changes live env."""
base=pathlib.Path('/home/aqyfzx/ai/knowhub-prompt-login-479416c90')
receipt=json.loads((base/'status.json').read_text());assert receipt['result']=='PASS'
iid=receipt['image_id'];sha='479416c9086d91de4484dbbd2cc5f912cdcdb697'
assert re.fullmatch(r'sha256:[a-f0-9]{64}',iid) and receipt['revision']==sha
assert run(['docker','image','inspect','--format','{{index .Config.Labels "org.opencontainers.image.revision"}}',iid]).decode().strip()==sha
old='sha256:2d91336c3f5ba1ba330820801c7bf0573b1c9c45b9280994dd0862d016435286'
assert json.loads(run(['docker','image','inspect','--format','{{json .Id}}',old]))==old
ids=run(['docker','ps','--filter','label=com.docker.compose.project=knowhub-sourceid-business','--filter','label=com.docker.compose.service=app','--format','{{.ID}}']).decode().split();assert len(ids)==1
assert inspect(ids[0],'.Image')==old
candidate=base/'prompt-login-app-pin.yml';rollback=base/'prompt-login-app-rollback-pin.yml'
candidate.write_text('services:\n  app:\n    image: '+iid+'\n    environment:\n      SOURCEID_FORCE_REAUTH_PROMPT_LOGIN: "${SOURCEID_FORCE_REAUTH_PROMPT_LOGIN:-false}"\n')
rollback.write_text('services:\n  app:\n    image: '+old+'\n')
candidate.chmod(0o600);rollback.chmod(0o600)
print(json.dumps({'result':'PREPARED_NOT_DEPLOYED','candidate_image':iid,'revision':sha,'candidate_pin':str(candidate),'rollback_pin':str(rollback),'old_app_image':old,'live_app_unchanged':True,'new_flag_default':False,'database_migration_required':False}))
