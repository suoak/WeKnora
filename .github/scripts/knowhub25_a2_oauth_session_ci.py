"""Read-only Linux A2 whole-package comparison; no deployment/database credentials."""
import hashlib, json, os, platform, subprocess, sys, time
from pathlib import Path
BASELINE = '0ae8f41278e0e8866a840a695e6409beaeb602af'
SOURCE_BLOBS = {'internal/mcp/oauth_manager.go': 'dec553531769435e1d7a6a2e9dfc1bf192152522', 'internal/mcp/oauth_manager_test.go': '410cd52bd40bc3a17a2e7a85a2be78722390797f', 'internal/mcp/client.go': 'b4a619655836c6d6192455d5ff11a7a38475fcce', 'internal/mcp/client_session_test.go': '31b0d60d71f64fce2772407a5b698d920b5bba5e'}
PATCH_SHA256 = '847fad9a7012951d96a76760fbdd5653a7991582603406216d8afe5d97429887'
RECIPE = {".github/workflows/knowhub25-a2-oauth-session-linux.yml", ".github/scripts/knowhub25_a2_oauth_session_ci.py"}
PACKAGES = ['./internal/mcp', './internal/types', './internal/application/service', './internal/handler', './internal/application/repository', './internal/application/access']
REQUIRED = {'./internal/mcp': {'TestMCPManagerReconnectsWithoutReplayingToolCall', 'TestOAuthManagerKeepsTheRegisteredClientSecret', 'TestMCPClientDisconnectsWhenTheSessionIsGone', 'TestMCPStateless404DoesNotResetConnection', 'TestMCPHTTPAuthErrorsAreNotRetried', 'TestMCPSessionErrorClassification', 'TestOAuthManagerClientStorageFailureIsNotAuthorizationSuccess'}, './internal/types': set(), './internal/application/service': set(), './internal/handler': set(), './internal/application/repository': set(), './internal/application/access': set()}
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
    result={"status":"PASS" if passed else "BUILD_FAILED_TESTS_NOT_EXECUTED" if compile_code else "TEST_OR_EXECUTION_FAILURE","compileExit":compile_code,"testExit":test_code,"executedTopLevel":sum("/" not in n for n in names["run"]),"executedIncludingSubtests":len(names["run"]),"pass":len(names["pass"]),"fail":len(names["fail"]),"skip":len(names["skip"]),"skippedTests":sorted(names["skip"]),"requiredRegressionPass":sorted(REQUIRED[package]&names["pass"])}
    report["results"][label][package]=result;save(folder/"result.json",result);save(evidence/"comparison.json",report)
   race_folder=evidence/label/"race-mcp";race_folder.mkdir(parents=True)
   race_exit=run(["go","test","-mod=readonly","-p","2","-count=1","-race","-json","./internal/mcp"],root,race_folder/"test.jsonl",commands)
   race_events=[]
   for line in (race_folder/"test.jsonl").read_text().splitlines():
    try: race_events.append(json.loads(line))
    except ValueError: pass
   race_pass=race_exit==0 and any(e.get("Action")=="pass" and "Test" not in e for e in race_events)
   report.setdefault("race",{})[label]={"exit":race_exit,"pass":race_pass}
   save(evidence/"comparison.json",report)
   require(run(["go","list","-mod=readonly","-m","-json","all"],root,evidence/(label+"-modules.json"),commands)==0,"Module listing failed")
   require(not git(root,"status","--porcelain") and locked=={name:hashlib.sha256((root/name).read_bytes()).hexdigest() for name in locked},"Source/dependency mutation")
  require(identities[0]==identities[1],"Dependency drift")
  require(run(["go","mod","verify"],baseline,evidence/"module-cache-verification.txt",commands)==0,"Module cache integrity failed")
  report["gate"]="A2_LINUX_MODULES_PASS" if all(result["status"]=="PASS" for group in report["results"].values() for result in group.values()) and all(v["pass"] for v in report["race"].values()) else "BLOCKED"
 except Exception as error: report["error"]=str(error)
 save(evidence/"comparison.json",report);print(json.dumps(report,indent=2))
 with Path(os.environ["GITHUB_STEP_SUMMARY"]).open("a") as output: output.write("```json\n"+json.dumps(report,indent=2)+"\n```\n")
 return 0 if report["gate"]=="A2_LINUX_MODULES_PASS" else 1
if __name__=="__main__": sys.exit(main())
