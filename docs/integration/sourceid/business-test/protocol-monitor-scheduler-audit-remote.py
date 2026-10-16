"""Read-only limited scheduler classification; no commands or env exported."""
matches=[]
patterns=re.compile(r'(?i)(sourceid|oidc|sid\.ruijie|/auth/oidc)')
p=subprocess.run(['crontab','-l'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=10)
if p.returncode==0:
 lines=[x for x in p.stdout.decode('utf-8','replace').splitlines() if x.strip() and not x.lstrip().startswith('#')]
 cron={'readable':True,'active_line_count':len(lines),'oidc_related_line_count':sum(bool(patterns.search(x)) for x in lines)}
elif b'no crontab' in p.stderr.lower():cron={'readable':True,'active_line_count':0,'oidc_related_line_count':0}
else:cron={'readable':False,'status':'UNKNOWN'}
system={}
for name,args in [('system_timers',['systemctl','list-timers','--all','--no-pager','--no-legend']),('user_timers',['systemctl','--user','list-timers','--all','--no-pager','--no-legend'])]:
 p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=10)
 if p.returncode==0:
  lines=p.stdout.decode('utf-8','replace').splitlines();system[name]={'readable':True,'oidc_marker_line_count':sum(bool(patterns.search(x)) for x in lines)}
 else:system[name]={'readable':False,'status':'UNKNOWN'}
print(json.dumps({'result':'READ_ONLY_COMPLETED','utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'current_user_crontab':cron,'timers':system,'scope_limit':'No privileged system crontab, other user schedules, client machines or browser extensions inspected. No marker does not prove no other callers.','new_login':False,'config_changed':False}))
