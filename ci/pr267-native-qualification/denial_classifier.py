"""Fixed-template permission classification; no free-form lines are exported."""
import hashlib,re

def classify(text):
 lines=[line for line in text.splitlines() if 'permission denied' in line.lower()]
 templates=('permission denied to access this knowledge base','permission denied to access this knowledge')
 expected=[line for line in lines if any(phrase in line.lower() for phrase in templates)]
 remaining=[line for line in lines if line not in expected]
 operating_system=[line for line in remaining if any(x in line.lower() for x in ('[errno 13]','eacces','open /','mkdir /','oserror:','permissionerror:'))]
 unknown=[line for line in remaining if line not in operating_system]
 return {'permission_total':len(lines),'api_expected_denial':len(expected),'operating_system_denial':len(operating_system),'unknown_denial':len(unknown),'public_go_callers':sorted(set(re.findall(r'[A-Za-z0-9_]+\.go:[0-9]+\[[A-Za-z0-9_./*()-]+\]', '\n'.join(lines))))[:8],'unknown_line_sha256':[hashlib.sha256(line.encode()).hexdigest() for line in unknown[:4]],'free_text_exported':False}

def self_test():
 api=classify('kb_access.go:75[CheckAccess] Permission denied to access this knowledge base')
 assert api['api_expected_denial']==1 and api['operating_system_denial']==api['unknown_denial']==0
 os_error=classify('PermissionError: [Errno 13] Permission denied: redacted')
 assert os_error['operating_system_denial']==1 and os_error['api_expected_denial']==os_error['unknown_denial']==0
 unknown=classify('component permission denied')
 assert unknown['unknown_denial']==1 and unknown['api_expected_denial']==unknown['operating_system_denial']==0

if __name__=='__main__':
 self_test();print('permission classifier: 3 synthetic cases PASS')
