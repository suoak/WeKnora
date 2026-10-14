import copy,importlib.util,json,pathlib
p=pathlib.Path(__file__).with_name('probe_report_filter.py')
s=importlib.util.spec_from_file_location('report_filter',p);m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
report={'endpoint':dict(m.ENDPOINTS),'validation':{'completed':False,'state':True,'token_exchange':False},'http':[{'stage':'token','status':401,'duration_ms':208,'diagnostics':{'request':{'method':'POST','host':'sid.ruijie.com.cn','path':'/oidc/accessToken','host_matches_endpoint':True,'content_type':'application/x-www-form-urlencoded','authorization_type':'Basic','authorization_present':True,'basic_format_valid':True,'configured_client_id':'knowhub','configured_redirect_uri':m.ENDPOINTS['callback'],'form_fields':['client_id','code','grant_type','redirect_uri'],'field_presence':{'client_id':True,'code':True,'grant_type':True,'redirect_uri':True,'client_secret':False,'code_verifier':False},'field_checks':{'grant_type_authorization_code':True,'client_id_matches_config':True,'redirect_uri_matches_config':True,'code_single_nonempty':True},'unknown_field_count':0,'form_inspected':True,'query_present':False,'dispatch_utc':'2026-10-13T14:11:54.123456789Z','sent_utc':'2026-10-13T14:11:54.243456789Z'},'response':{'content_type':'application/json','declared_length':26,'bytes_read':26,'body_eof':True,'prefix_truncated':False,'read_error':False,'body_elapsed_ms':208,'body_kind':'json_object','unknown_field_count':0,'error_description_present':True,'error_description_summary':'withheld_untrusted_description','vendor_code_suppressed':False,'www_authenticate_present':True,'www_schemes':['basic'],'www_oauth_error':'invalid_client','www_realm_present':True,'www_description_present':False,'correlation':{'X-Request-ID':{'present':True,'format_valid':True,'sha256':'a'*64}},'gateway_suspected':False,'error_fields':{'error':'string','error_description':'string'},'oauth_error':'invalid_client'}}}]}
assert m.safe(report)
mutations=[('request','authorization_value','synthetic-private'),('response','raw_body','synthetic-private'),('response','error_description_summary','synthetic-private'),('response','vendor_error_code','EMPLOYEE_123456'),('response','www_schemes',['synthetic-private'])]
for side,key,value in mutations:
    x=copy.deepcopy(report);x['http'][0]['diagnostics'][side][key]=value;assert not m.safe(x)
x=copy.deepcopy(report);x['http'][0]['diagnostics']['response']['vendor_error_code']='SID_INVALID_CLIENT';assert not m.safe(x);assert m.safe(x,['SID_INVALID_CLIENT'])
print(json.dumps({'report_filter':'PASS','unknown_raw_fields_rejected':True,'unapproved_vendor_codes_rejected':True,'nanosecond_timestamp':'PASS','real_secret_read':False}))

transport = {
 'trace':'12345678-1234-1234-1234-123456789abc', 'utc':'2026-10-14T09:09:00Z', 'beijing':'2026-10-14T17:09:00+08:00',
 'scheme':'https','accept_class':'application/json','user_agent_header_present':False,'user_agent_class':'transport_default_not_observed',
 'basic_strategy':'oauth_form_escape_then_set_basic_auth_base64_once','client_secret_loaded':True,'oauth_form_encoding_executed':True,'set_basic_auth_used':True,
 'authorization_header_value_count':1,'duplicate_authorization':False,'body_value_count':4,'known_body_field_counts':{'code':1,'client_id':1,'grant_type':1,'redirect_uri':1},'duplicate_body_fields':False,
 'pkce_disabled':True,'state_validation':'PASS','nonce_validation_at_token_dispatch':'NOT_YET_EXECUTED','proxy_policy':'no_proxy_in_configured_transport',
 'remote_address':'192.0.2.1:443','tls_version':'TLS1.3','negotiated_protocol':'h2','response_http_version':'HTTP/2.0','connection_reused':False,
 'network_events':[{'kind':'tls_done','utc':'2026-10-14T09:09:00Z','beijing':'2026-10-14T17:09:00+08:00','result':'ok','duration_ms':12}],
 'events_dropped':0,'observed_write_attempts':1,'observed_completed_writes':1,'retry_observed':False,'redirect_response':False,
 'response_date_utc':'2026-10-14T09:09:00Z','server_class':'nginx','via_class':'present_value_withheld','www_realm_class':'present_value_withheld',
 'proxy_authenticate_present':False,'network_failure_class':'none','rejecting_layer':'UNKNOWN','outcome':'empty_401_basic_challenge',
}
assert m.transport(transport)
x=copy.deepcopy(report);x['http'][0]['transport']=transport;assert m.safe(x)
for key,value in [('secret','synthetic-private'),('server_class','nginx/synthetic-private'),('remote_address','synthetic-private:443'),('trace','synthetic-private'),('body_value_count',True),('beijing','synthetic-private')]:
 y=copy.deepcopy(transport);y[key]=value;assert not m.transport(y)
y=copy.deepcopy(transport);y['network_events'][0]['raw_headers']='synthetic-private';assert not m.transport(y)
y=copy.deepcopy(transport);y['network_events']*=65;assert not m.transport(y)
print(json.dumps({'business_transport_filter':'PASS','raw_headers_and_unknown_fields_rejected':True,'bounded_events':True}))
