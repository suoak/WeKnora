fields=['SOURCEID_ENABLE','SOURCEID_CLAIMS_SOURCE','SOURCEID_EMPLOYEE_CLAIM','SOURCEID_LOGOUT_ENABLE','SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED']
v=json.loads(run(['docker','exec','knowhub-company-test-app-1','python3','-c',"import os,json,sys;print(json.dumps({k:os.environ.get(k) for k in json.loads(sys.argv[1])}))",json.dumps(fields)]))
print(json.dumps({'result':'READ_ONLY','claim_path_configuration':v,'business_gate_changed':False}))
