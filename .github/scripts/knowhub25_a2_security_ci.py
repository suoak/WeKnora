"""Read-only Linux A2 whole-package comparison; no deployment/database credentials."""
import hashlib, json, os, platform, subprocess, sys, time
from pathlib import Path
BASELINE = '0ae8f41278e0e8866a840a695e6409beaeb602af'
SOURCE_BLOBS = {'internal/agent/tools/mcp_oauth.go': '1febb826e68803311a2a51204408e3eeb6849b85', 'internal/application/repository/mcp_oauth.go': '21ccbcdd3ff6a9047545a408388dac126f3914df', 'internal/handler/mcp_oauth.go': '4950f35431774127b186639db26360f14c920139', 'internal/mcp/client.go': '82f8c713f82588ce784d50667c9090cdc3b37890', 'internal/mcp/manager.go': '31f32fe256eb353a48367a8b0cf0fbb1053a6b70', 'internal/mcp/oauth_lifecycle.go': '14d09449d90521d9e75d34d15d82380872b199c7', 'internal/mcp/oauth_manager.go': '7efef9124b17ca64089137cec849e553557d717d', 'internal/mcp/oauth_principal_test.go': 'cd3fe1b8e85654e8f52cfc6938e769775247d22d', 'internal/mcp/oauth_tokenstore.go': '3a997aac3b9621f8c6bb7e728673b2a8e8adb5fe', 'internal/middleware/logger.go': '461df951add05e3e24050caefa6210e01b40755b', 'internal/types/mcp_oauth.go': 'c91d0d660e2ffce26751044e7bc6ca1b7e96c7eb', 'internal/mcp/oauth_manager_test.go': 'e44f62ce760ecc4768c107081f05246409e31f9e', 'internal/mcp/client_session_test.go': '31b0d60d71f64fce2772407a5b698d920b5bba5e', 'internal/mcp/oauth_security_test.go': 'd55cfb1b7d6b84aa2521ac0a62f7c224655e954d', 'internal/mcp/manager_security_test.go': '74e6e394c43b2bea33c1307953cb438bc186b127', 'internal/mcp/oauth_tenant_wire_test.go': 'd594933c9d5c650f6b4ba1e92c088785cabd2359', 'internal/mcp/oauth_errors.go': 'f062d7c8a1c682f0d92d19e2f3215c166350c836', 'internal/handler/mcp_oauth_security_test.go': 'd5394128ee52f990f21f053b2a9a71f5ef3a5133', 'internal/application/repository/mcp_oauth_security_test.go': 'b0835df8595799a656a6f2a26a780765809eded0', 'internal/middleware/oauth_query_security_test.go': '1ffe3ab90b955055c46bd21994d5c73275944913', 'internal/mcp/oauth_discovery.go': '44ec419e5919db7bfd0377729c5e48aa15c17e65', 'internal/mcp/oauth_discovery_test.go': '1b4204a291c9c566ef984611c8816a62e607b29a', 'internal/mcp/oauth_tokenstore_security_test.go': '8b3891be994ce641104de9f9d5ef77e4a3d9bbc8', 'internal/handler/mcp_oauth_revoke_scope_test.go': 'aad0a48d636ab05cba3794592d7238b7a6997763'}
PATCH_SHA256 = '37db75239d4a08fcbcd48ad7a2354a1fa8430699ecd958db1aa3a2bf8c023fec'
RECIPE = {".github/workflows/knowhub25-a2-security-linux.yml", ".github/scripts/knowhub25_a2_security_ci.py"}
PACKAGES = ['./internal/mcp', './internal/types', './internal/application/service', './internal/handler', './internal/application/repository', './internal/application/access', './internal/middleware', './internal/agent/tools']
REQUIRED = {'./internal/mcp': {'TestMCPManagerReconnectsWithoutReplayingToolCall', 'TestOAuthManagerKeepsTheRegisteredClientSecret', 'TestMCPClientDisconnectsWhenTheSessionIsGone', 'TestMCPStateless404DoesNotResetConnection', 'TestMCPHTTPAuthErrorsAreNotRetried', 'TestMCPSessionErrorClassification', 'TestOAuthManagerClientStorageFailureIsNotAuthorizationSuccess'}, './internal/types': set(), './internal/application/service': set(), './internal/handler': {'TestOAuthAPIAndAccessLogsDoNotLeakProviderOrSQL'}, './internal/application/repository': {'TestMCPOAuthClientFailClosedSQL', 'TestMCPOAuthRepositorySQLLogsAreRedacted'}, './internal/application/access': set(), './internal/middleware': {'TestOAuthProviderErrorQueryRedaction'}, './internal/agent/tools': set()}
REQUIRED['./internal/mcp'].update({'TestOAuthAuthorizationFailClosedStateAndRecovery', 'TestOAuthMaliciousProviderErrorsAndLogsAreRedacted', 'TestOAuthRefreshRedactionPreservesClassification', 'TestBuiltinOAuthTenantConnectionIsolation', 'TestManagerRealWaiterDeadlineDoesNotCancelInitialization', 'TestBuiltinOAuthWireScope', 'TestOAuthCallbackInvalidatesOnlyAuthenticatedStateScope'})
for parent,children,package in [
 ('TestOAuthAuthorizationFailClosedStateAndRecovery',['missing-key','invalid-key','sql-error','public'],'./internal/mcp'),
 ('TestOAuthMaliciousProviderErrorsAndLogsAreRedacted',['registration','exchange','discovery'],'./internal/mcp'),
 ('TestMCPOAuthClientFailClosedSQL',['valid','missing','invalid','entropy','public','forged-prefix'],'./internal/application/repository'),
 ('TestOAuthAPIAndAccessLogsDoNotLeakProviderOrSQL',['provider','sql','callback','status','revoke'],'./internal/handler')]:
 REQUIRED[package].update(parent+'/'+child for child in children)


# Every new safety path must RUN/PASS in the whole suite AND whole MCP race.
REQUIRED['./internal/mcp'].update({'TestOAuthDiscoveryRejectsInvalidMetadataBeforeSideEffects', 'TestOAuthDiscoveryUsesConsumerCacheAndGuardsRebuiltHandlers', 'TestOAuthRefreshBadDiscoveryPreservesToken', 'TestOAuthDiscoveryKeepsRedirectPolicyAndExistingPublicClient', 'TestOAuthTokenStoreErrorsAreSafeAndScoped', 'TestOAuthRuntimeRefreshesExpiredToken', 'TestOAuthRuntimeDeletesPermanentlyInvalidRefreshToken', 'TestOAuthRuntimePreservesTokenOnTemporaryRefreshFailure', 'TestOAuthRuntimeSerializesRotatingRefreshToken', 'TestOAuthCallRefreshesAndRetriesResource401Once', 'TestOAuthCallDoesNotRetryMoreThanOnce'})
REQUIRED['./internal/handler'].add('TestOAuthRevokeSuccessClearsOnlyAuthenticatedScope')
for parent,children in [
 ('TestOAuthDiscoveryRejectsInvalidMetadataBeforeSideEffects',['http-400','http-401','http-403','http-500','empty','null','malformed','empty-object','missing-authorization','missing-token','missing-registration','unsafe-endpoint','interrupted','deadline','cancel']),
 ('TestOAuthDiscoveryKeepsRedirectPolicyAndExistingPublicClient',['same-origin','cross-origin']),
 ('TestOAuthTokenStoreErrorsAreSafeAndScoped',['read','managed-read','write','cancel-read','cancel-write']),
 ('TestOAuthManagerKeepsTheRegisteredClientSecret',['confidential','public'])]:
 REQUIRED['./internal/mcp'].update(parent+'/'+child for child in children)
CANARIES=['synthetic-client-secret','synthetic-access-token','synthetic-refresh-token','synthetic-password','synthetic-secret','synthetic-access','synthetic-refresh','registered-secret','other-secret','tenant-a-token','tenant-b-token','other-principal-token','synthetic-revoke-a','synthetic-revoke-b','synthetic-revoke-other']

def require(condition, message):
 if not condition: raise RuntimeError(message)
def git(root, *args): return subprocess.check_output(["git", *args], cwd=root)
def save(path, data): path.write_text(json.dumps(data, indent=2)+"\n", encoding="utf-8")
def run(command, root, path, commands):
 started=time.monotonic()
 with path.open("wb") as output: result=subprocess.run(command, cwd=root, stdout=output, stderr=subprocess.STDOUT)
 commands.append({"command":command,"cwd":str(root),"log":str(path),"exit":result.returncode,"seconds":time.monotonic()-started})
 return result.returncode

def main():
 workspace=Path(os.environ["GITHUB_WORKSPACE"]); evidence=workspace/"evidence"; evidence.mkdir(exist_ok=True)
 report={"gate":"BLOCKED","scope":"A2 Linux module comparison only; skips and production dependency gaps reported separately", "commands":[], "results":{}}
 commands=report["commands"]
 try:
  require(platform.system()=="Linux" and platform.machine()=="x86_64", "Linux amd64 required")
  candidate=workspace/"candidate"; baseline=workspace/"baseline"
  require(git(baseline,"rev-parse","HEAD").decode().strip()==BASELINE, "Baseline mismatch")
  require(git(candidate,"rev-parse","HEAD").decode().strip()==os.environ["GITHUB_SHA"], "Candidate mismatch")
  require(set(git(candidate,"diff","--name-only",BASELINE,"HEAD").decode().splitlines())==set(SOURCE_BLOBS)|RECIPE, "Unreviewed paths")
  patch=git(candidate,"diff","--no-ext-diff","--no-textconv","--no-renames","--full-index","--binary","--src-prefix=a/","--dst-prefix=b/",BASELINE,"HEAD","--",*SOURCE_BLOBS)
  require(hashlib.sha256(patch).hexdigest()==PATCH_SHA256,"Product patch mismatch")
  (evidence/"product.patch").write_bytes(patch)
  for name,blob in SOURCE_BLOBS.items(): require(git(candidate,"rev-parse","HEAD:"+name).decode().strip()==blob,"Source blob mismatch")
  report["identity"]={"baseline":BASELINE,"candidate":os.environ["GITHUB_SHA"],"patchSHA256":PATCH_SHA256,"sourceBlobs":SOURCE_BLOBS,"upstream":["f559473498719ff0f5c94fc9c6918199986aaf3e","28734725a3567d5d12a0b190b0e5bccbb91b252c"]}
  for name,cmd in [("go-version",["go","version"]),("gcc-version",["gcc","--version"]),("gxx-version",["g++","--version"]),("uname",["uname","-a"])]: require(run(cmd,baseline,evidence/(name+".txt"),commands)==0,"Environment probe failed")
  fields=["GOVERSION","GOOS","GOARCH","CGO_ENABLED","GOTOOLCHAIN","GOFLAGS","CC","CXX","CGO_CFLAGS","CGO_CXXFLAGS","CGO_LDFLAGS","GOMODCACHE","GOCACHE","GOPROXY","GOSUMDB"]
  require(run(["go","env","-json",*fields],baseline,evidence/"go-environment.json",commands)==0,"go env failed")
  environment=json.loads((evidence/"go-environment.json").read_text());report["environment"]=environment
  for key,value in {"GOVERSION":"go1.26.7","GOOS":"linux","GOARCH":"amd64","CGO_ENABLED":"1","GOTOOLCHAIN":"local","GOFLAGS":""}.items(): require(environment[key]==value,"Unexpected "+key)
  identities=[]
  for label,root in [("baseline",baseline),("candidate",candidate)]:
   require(not git(root,"status","--porcelain"),"Dirty initial checkout")
   locked={name:hashlib.sha256((root/name).read_bytes()).hexdigest() for name in ["go.mod","go.sum"]};identities.append(locked)
   report["results"][label]={}
   for package in PACKAGES:
    folder=evidence/label/package.removeprefix("./");folder.mkdir(parents=True)
    compile_code=run(["go","test","-mod=readonly","-p","2","-c","-x","-o",str(folder/"package.test"),package],root,folder/"compile.log",commands)
    test_code=None;events=[]
    if compile_code==0:
     run(["file",str(folder/"package.test")],root,folder/"elf.txt",commands);run(["ldd",str(folder/"package.test")],root,folder/"linkage.txt",commands)
     test_code=run(["go","test","-mod=readonly","-p","2","-count=1","-json",package],root,folder/"test.jsonl",commands)
     for line in (folder/"test.jsonl").read_text().splitlines():
      try: events.append(json.loads(line))
      except ValueError: pass
    full_package="github.com/Tencent/WeKnora/"+package.removeprefix("./")
    names={action:{e["Test"] for e in events if e.get("Package")==full_package and e.get("Action")==action and "Test" in e} for action in ["run","pass","fail","skip"]}
    package_pass=any(e.get("Package")==full_package and e.get("Action")=="pass" and "Test" not in e for e in events)
    passed=compile_code==0 and test_code==0 and bool(names["run"]) and bool(names["pass"]) and package_pass and not names["fail"] and names["run"]==names["pass"]|names["skip"] and (label!="candidate" or REQUIRED[package]<=names["pass"])
    result={"status":"PASS" if passed else "BUILD_FAILED_TESTS_NOT_EXECUTED" if compile_code else "TEST_OR_EXECUTION_FAILURE","compileExit":compile_code,"testExit":test_code,"executedTopLevel":sum("/" not in n for n in names["run"]),"executedIncludingSubtests":len(names["run"]),"pass":len(names["pass"]),"fail":len(names["fail"]),"skip":len(names["skip"]),"skippedTests":sorted(names["skip"]),"requiredRegressionPass":sorted(REQUIRED[package]&names["pass"]),"requiredRegressionMissing":sorted(REQUIRED[package]-names["pass"])}
    report["results"][label][package]=result;save(folder/"result.json",result);save(evidence/"comparison.json",report)
   race_folder=evidence/label/"race-mcp";race_folder.mkdir(parents=True)
   race_exit=run(["go","test","-mod=readonly","-p","2","-count=1","-race","-json","./internal/mcp"],root,race_folder/"test.jsonl",commands)
   race_events=[]
   for line in (race_folder/"test.jsonl").read_text().splitlines():
    try: race_events.append(json.loads(line))
    except ValueError: pass
   race_names={action:{e["Test"] for e in race_events if e.get("Action")==action and "Test" in e} for action in ["run","pass","fail","skip"]}
   race_pass=race_exit==0 and bool(race_names["run"]) and race_names["run"]==race_names["pass"]|race_names["skip"] and not race_names["fail"] and any(e.get("Action")=="pass" and "Test" not in e for e in race_events) and (label!="candidate" or REQUIRED['./internal/mcp']<=race_names["pass"])
   report.setdefault("race",{})[label]={"exit":race_exit,"pass":race_pass,"executed":len(race_names["run"]),"testPass":len(race_names["pass"]),"testFail":len(race_names["fail"]),"skippedTests":sorted(race_names["skip"])}
   save(evidence/"comparison.json",report)
   require(run(["go","list","-mod=readonly","-m","-json","all"],root,evidence/(label+"-modules.json"),commands)==0,"Module listing failed")
   require(not git(root,"status","--porcelain") and locked=={name:hashlib.sha256((root/name).read_bytes()).hexdigest() for name in locked},"Source/dependency mutation")
  require(identities[0]==identities[1],"Dependency drift")
  require(run(["go","mod","verify"],baseline,evidence/"module-cache-verification.txt",commands)==0,"Module cache integrity failed")
  # Supplemental reproduction only: it does not replace either full suite.
  # The preserved old candidate must compile, then fail this wire assertion.
  old=workspace/"old-candidate"
  require(git(old,"rev-parse","HEAD").decode().strip()=="457e124d76a2b84f4b7e1f47cc66ae55bc8f5444", "Old candidate identity mismatch")
  require(not git(old,"status","--porcelain"),"Old checkout initially dirty")
  wire="internal/mcp/oauth_tenant_wire_test.go"
  (old/wire).write_bytes((candidate/wire).read_bytes())
  reproduction=evidence/"old-tenant-collision";reproduction.mkdir()
  old_compile=run(["go","test","-mod=readonly","-p","2","-c","-x","-o",str(reproduction/"package.test"),"./internal/mcp"],old,reproduction/"compile.log",commands)
  old_test=None;old_events=[]
  if old_compile==0:
   old_test=run(["go","test","-mod=readonly","-p","2","-count=1","-json","-run","^TestBuiltinOAuthWireScope$","./internal/mcp"],old,reproduction/"test.jsonl",commands)
   for line in (reproduction/"test.jsonl").read_text().splitlines():
    try: old_events.append(json.loads(line))
    except ValueError: pass
  reproduced=old_compile==0 and old_test==1 and any(e.get("Test")=="TestBuiltinOAuthWireScope" and e.get("Action")=="fail" for e in old_events)
  reproduced=reproduced and any("different tenant credentials require independent transport initialization" in e.get("Output","") for e in old_events)
  report["oldTenantCollision"]={"sha":"457e124d76a2b84f4b7e1f47cc66ae55bc8f5444","compileExit":old_compile,"testExit":old_test,"expectedAssertionFailureReproduced":reproduced,"fixtureSHA256":hashlib.sha256((old/wire).read_bytes()).hexdigest()}
  save(evidence/"comparison.json",report)
  require(reproduced,"Old tenant collision was not demonstrated; manual investigation required")
  # Scan actual runtime Output only: fixture literals in product.patch are
  # intentional synthetic inputs, not evidence of runtime credential leakage.
  findings=[]
  for path in (evidence/"candidate").rglob("test.jsonl"):
   for number,line in enumerate(path.read_text().splitlines(),1):
    try: event=json.loads(line)
    except ValueError: continue
    for index,value in enumerate(CANARIES):
     if value in event.get("Output",""):
      findings.append({"log":str(path.relative_to(evidence)),"line":number,"canaryIndex":index,"test":event.get("Test")})
  report["runtimeCanaryScan"]={"findings":findings,"scope":"candidate whole packages and MCP race Output; fixture patch excluded"}
  save(evidence/"comparison.json",report)
  require(not findings,"Runtime credential canary detected; inspect captured evidence")
  report["gate"]="A2_SECURITY_LINUX_CI_PASS" if all(result["status"]=="PASS" for group in report["results"].values() for result in group.values()) and all(v["pass"] for v in report["race"].values()) else "BLOCKED"
 except Exception as error: report["error"]=str(error)
 save(evidence/"comparison.json",report);print(json.dumps(report,indent=2))
 with Path(os.environ["GITHUB_STEP_SUMMARY"]).open("a") as output: output.write("```json\n"+json.dumps(report,indent=2)+"\n```\n")
 return 0 if report["gate"]=="A2_SECURITY_LINUX_CI_PASS" else 1
if __name__=="__main__": sys.exit(main())
