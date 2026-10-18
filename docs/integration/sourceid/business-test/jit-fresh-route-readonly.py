R=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017')
s=(R/'entry/nginx.conf').read_text()
print(json.dumps({'proxy_directives':re.findall(r'proxy_pass\s+([^;]+);',s),'upstream_values':re.findall(r'\bset\s+\$upstream\s+([^;]+);',s),'upstream_servers':re.findall(r'\bserver\s+([a-zA-Z0-9_.:-]+)\s*;',s),'stream_mode':'stream {' in s or 'stream\n' in s}))
