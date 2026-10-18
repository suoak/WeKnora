"""Classify container inventory differences without exporting raw metadata."""
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51');S=json.loads((B/'retry-state.json').read_text());D=pathlib.Path(S['backup_directory'])
before=json.loads((D/'containers-before.json').read_text());names=list(before)
fmt='{"id":{{json .Id}},"image_id":{{json .Image}},"mounts":{{json .Mounts}},"ports":{{json .NetworkSettings.Ports}}}'
now=dict(zip(names,[json.loads(v) for v in run(['docker','inspect','--format',fmt]+names).decode().splitlines()]))
out=[]
for name in names:
 if name=='knowhub-company-test-app-1':continue
 old=before[name];new=now[name]
 if old!=new:
  keys=[k for k in set(old)|set(new) if old.get(k)!=new.get(k)]
  out.append({'container':name,'differing_keys':keys,'mounts_equal_ignoring_order':sorted(old['mounts'],key=lambda x:json.dumps(x,sort_keys=True))==sorted(new['mounts'],key=lambda x:json.dumps(x,sort_keys=True)),'id_unchanged':old['id']==new['id'],'image_unchanged':old['image_id']==new['image_id'],'ports_unchanged':old['ports']==new['ports']})
print(json.dumps({'result':'READ_ONLY','differences':out,'app_image':inspect('knowhub-company-test-app-1','.Image'),'app_health':inspect('knowhub-company-test-app-1','.State.Health.Status'),'real_sid_requests':0}))
