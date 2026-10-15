ids=run(['docker','ps','--format','{{.ID}}']).decode().split()
rows=[]
for cid in ids:
    labels=inspect(cid,'.Config.Labels') or {}
    nets=inspect(cid,'.NetworkSettings.Networks') or {}
    ports=inspect(cid,'.NetworkSettings.Ports') or {}
    rows.append({'id':cid,'name':inspect(cid,'.Name'),'image_id':inspect(cid,'.Image'),'project':labels.get('com.docker.compose.project'),'service':labels.get('com.docker.compose.service'),'published_ports':ports,'networks':{n:{'ip':v.get('IPAddress'),'ipv6':v.get('GlobalIPv6Address')} for n,v in nets.items()}})
listeners=subprocess.run(['ss','-H','-ltn'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=10)
listen=[]
if listeners.returncode==0:
    for line in listeners.stdout.decode().splitlines():
        cols=line.split()
        if len(cols)>3 and re.fullmatch(r'[0-9a-fA-F:.%*\[\]-]+',cols[3]): listen.append(cols[3])
fw={}
for name,args in [('iptables',['sudo','-n','iptables','-S','DOCKER-USER']),('ip6tables',['sudo','-n','ip6tables','-S','DOCKER-USER']),('nft',['sudo','-n','nft','list','ruleset'])]:
    q=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=10)
    fw[name]={'readable':q.returncode==0,'docker_user_chain_seen':b'DOCKER-USER' in q.stdout,'raw_rules_exported':False}
info=json.loads(run(['docker','info','--format','{{json .SecurityOptions}}']))
print(json.dumps({'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'target':'172.30.232.42','read_only':True,'containers':rows,'tcp_listeners':listen,'firewall_inventory':fw,'docker_security_options':info,'network_rules_changed':False,'real_login_started':False}))
