import sys
sys.path.insert(0,'/home/aqyfzx/ai/knowhub-legacy-clean-target-20261016')
import support as s
q="SELECT json_agg(json_build_object('table',table_name,'column',column_name,'nullable',is_nullable)) FROM information_schema.columns WHERE table_schema='public' AND table_name IN ('tenants','mcp_oauth_clients','mcp_oauth_tokens','mcp_services','data_sources','models','storage_backends')"
rows=s.scalar(q)
print(json.dumps({'columns':rows,'candidate_schema':s.sql("SELECT version::text||'|'||dirty::text FROM schema_migrations"),'candidate_network':s.inspect(s.PG,'.HostConfig.NetworkMode')}))
