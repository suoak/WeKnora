import os, tempfile, hashlib, ipaddress, time
os.umask(0o077)
baseline=set(run(['docker','ps','-q','--no-trunc']).decode().split())
image='sha256:63146a134fc3be75b663a1ac6ddba205be2b0a3c881891a3ba29a28d6a04f48a'
assert run(['docker','image','inspect','--format','{{.Id}}',image]).decode().strip()==image
prefix='oa-isolation-lab-'+str(os.getpid())
root=pathlib.Path(tempfile.mkdtemp(prefix=prefix+'-',dir='/home/aqyfzx/ai'))
containers=[];networks=[];checks={};port=None
front4='172.29.240.0/28';back4='172.29.241.0/28'
for nid in run(['docker','network','ls','-q']).decode().split():
    data=json.loads(run(['docker','network','inspect',nid]))[0]
    for x in data.get('IPAM',{}).get('Config',[]):
        if x.get('Subnet') and ':' not in x['Subnet']:
            assert not any(ipaddress.ip_network(x['Subnet']).overlaps(ipaddress.ip_network(s)) for s in (front4,back4))
try:
    for suffix,subnet,v6 in [('front',front4,'fd42:3027:aa01::/64'),('back',back4,'fd42:3027:aa02::/64')]:
        name=prefix+'-'+suffix
        run(['docker','network','create','--ipv6','--subnet',subnet,'--subnet',v6,'--label','knowhub.test=oa-isolation-lab']+(['--internal'] if suffix=='back' else [])+[name]);networks.append(name)
    app=root/'app.conf';front=root/'front.conf'
    app.write_text('events {} http { access_log off; error_log /dev/null; server { listen 8080; listen [::]:8080; location / { return 200 "SYNTHETIC_ONLY\n"; } } }')
    front.write_text('events {} http { access_log off; error_log /dev/null; server { listen 8080; listen [::]:8080; allow 172.29.240.10; allow fd42:3027:aa01::10; deny all; location / { proxy_pass http://172.29.241.2:8080; } } }')
    for suffix,network,ipv4,ipv6,conf,publish in [('app',networks[1],'172.29.241.2','fd42:3027:aa02::2',app,[]),('front',networks[0],'172.29.240.2','fd42:3027:aa01::2',front,['-p','127.0.0.1::8080'])]:
        cid=run(['docker','run','-d','--name',prefix+'-'+suffix,'--network',network,'--ip',ipv4,'--ip6',ipv6,'--label','knowhub.test=oa-isolation-lab','--read-only','--tmpfs','/var/cache/nginx','--tmpfs','/var/run','-v',str(conf)+':/etc/nginx/nginx.conf:ro']+publish+['--entrypoint','nginx',image,'-g','daemon off;']).decode().strip();containers.append(cid)
    run(['docker','network','connect','--ip','172.29.241.3','--ip6','fd42:3027:aa02::3',networks[1],containers[1]])
    time.sleep(1)
    def probe(label,ip,ip6,url,expected):
        q=subprocess.run(['docker','run','--rm','--network',networks[0],'--ip',ip,'--ip6',ip6,'--label','knowhub.test=oa-isolation-lab','--entrypoint','wget',image,'-T','3','-S','-O','-',url],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=15)
        status=re.findall(rb'HTTP/[0-9.]+ ([0-9]{3})',q.stderr)
        ok=(int(status[-1])==expected) if status else expected==0 and q.returncode!=0
        checks[label]={'status':int(status[-1]) if status else 0,'exit_code':q.returncode,'expected':expected,'pass':ok,'synthetic_body_only':q.stdout in (b'',b'SYNTHETIC_ONLY\n')}
        assert ok,label
    probe('allowed_ipv4','172.29.240.10','fd42:3027:aa01::10','http://172.29.240.2:8080',200)
    probe('denied_ipv4','172.29.240.11','fd42:3027:aa01::11','http://172.29.240.2:8080',403)
    probe('allowed_ipv6','172.29.240.10','fd42:3027:aa01::10','http://[fd42:3027:aa01::2]:8080',200)
    probe('denied_ipv6','172.29.240.11','fd42:3027:aa01::11','http://[fd42:3027:aa01::2]:8080',403)
    probe('direct_backend_denied','172.29.240.11','fd42:3027:aa01::11','http://172.29.241.2:8080',0)
    health=run(['docker','exec',containers[0],'wget','-q','-O','-','http://127.0.0.1:8080']).decode()
    checks['backend_health']={'pass':health=='SYNTHETIC_ONLY\n'}
    ports=inspect(containers[1],'.NetworkSettings.Ports')['8080/tcp'];assert len(ports)==1 and ports[0]['HostIp']=='127.0.0.1'
    port=ports[0]['HostPort']
    op=urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try: op.open('http://127.0.0.1:'+port,timeout=5);raise AssertionError('published_port_bypass')
    except urllib.error.HTTPError as e: checks['published_port_no_bypass']={'status':e.code,'pass':e.code==403};assert e.code==403
finally:
    for cid in reversed(containers):
        labels=inspect(cid,'.Config.Labels') or {};assert labels.get('knowhub.test')=='oa-isolation-lab'
        run(['docker','rm','-f',cid])
    for name in reversed(networks):run(['docker','network','rm',name])
    for f in root.iterdir():f.unlink()
    root.rmdir()
assert set(run(['docker','ps','-q','--no-trunc']).decode().split())==baseline
print(json.dumps({'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'scope':'new isolated synthetic Docker networks only','checks':checks,'published_port':port,'lab_removed':True,'existing_containers_unchanged':True,'running_service_rules_changed':False,'host_firewall_rules_modified':False,'real_environment_allow_deny_sources':'NOT_EXECUTED','result':'PASS'}))
