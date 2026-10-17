B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
state=json.loads((B/'deployment-state.json').read_text());D=pathlib.Path(state['backup_directory'])
names=('company-3029.dump','company-files.tar','protected-data-baseline.json','protected-file-baseline.json','protected-firewall-before.json')
print(json.dumps({'artifacts':{n:((D/n).stat().st_size if (D/n).exists() else None) for n in names},'app_running':inspect('knowhub-company-test-app-1','.State.Running'),'app_health':inspect('knowhub-company-test-app-1','.State').get('Health',{}).get('Status'),'backup_stage':json.loads((B/'deployment-backup-result.json').read_text())['stage']}))
