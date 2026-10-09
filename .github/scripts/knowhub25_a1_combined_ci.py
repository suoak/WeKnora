"""Read-only Linux A1 whole-package comparison; no deployment/database credentials."""
import hashlib, json, os, platform, subprocess, sys, time
from pathlib import Path
BASELINE = "1cbde4617aceab31b8928feb705599e2d65c7158"
SOURCE_BLOBS = {'internal/middleware/logger.go': 'a65ef0f98d55e9a3fb0fd6b316d7a85c1bbb6014', 'internal/middleware/logger_test.go': '79fe6d814e3c2190ee0f2b173c70cf5391cdb7aa', 'internal/utils/httputil.go': '8a8da886b93135059dc5bcb00988177dcf6b0249', 'internal/utils/httputil_test.go': 'f28b5bd91cc96e29938f8feaf5b61f49d0143bf9', 'internal/datasource/connector/gitlab/client.go': 'e9989525a7d4810d2c2dad2aec1e0257a9d2cbcc', 'internal/datasource/connector/gitlab/pagination_guard_test.go': '651e957114e0de33ff87d1ddabddfd1a34402c71', 'internal/datasource/connector/gitlab/pagination_recovery_test.go': 'e203a7d32149ad4c6fbfe57f721ef1d7bc701844', 'internal/application/service/datasource_gitlab_pagination_test.go': '33371771897618d509590884b044830c222b6d3b'}
PATCH_SHA256 = "e5f14908d5fb4839c71a51b245d916e2a72dd40997b0b511627f5e463e9e8774"
RECIPE = {".github/workflows/knowhub25-a1-combined-linux.yml", ".github/scripts/knowhub25_a1_combined_ci.py"}
PACKAGES = ["./internal/middleware", "./internal/utils", "./internal/datasource/connector/gitlab", "./internal/application/service"]
REQUIRED = {
 "./internal/middleware": {"TestReadRequestBodyKeepsShortBodyIntact", "TestReadRequestBodyStaysInsideItsLogBudget", "TestReadRequestBodyLeavesRouteBodyCapDeciding", "TestReadRequestBodySkipsNonTextBodies"},
 "./internal/utils": {"TestDownloadBytesSizeLimit", "TestDownloadBytesInterruptedStreamReturnsNoPartialData"},
 "./internal/datasource/connector/gitlab": {"TestProjectsStopsOnRepeatedNextPage", "TestTreeStopsOnRepeatedNextPage", "TestPaginationHopCapStopsWalk", "TestGitLabInvalidNextPageFails", "TestGitLabStreamPaginationFailureAndRetry"},
 "./internal/application/service": {"TestSyncServiceSQLite", "TestStreamHandler_CheckpointPersistsCursor", "TestProcessSyncGitLabPaginationFailurePreservesCursor"},
}
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
 report={"gate":"BLOCKED","scope":"A1 Linux comparison only; optional skipped PostgreSQL cases reported separately", "commands":[], "results":{}}
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
  report["identity"]={"baseline":BASELINE,"candidate":os.environ["GITHUB_SHA"],"patchSHA256":PATCH_SHA256,"sourceBlobs":SOURCE_BLOBS,"upstream":["6057a81017375a8d680cd32afca1c2ff03b0b17c","de2dfd3034a7b506fa269910401bbdcbd4efc71d","a2aeb97a01203b23b6093eba2369583b460aaa66"]}
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
   require(run(["go","list","-mod=readonly","-m","-json","all"],root,evidence/(label+"-modules.json"),commands)==0,"Module listing failed")
   require(not git(root,"status","--porcelain") and locked=={name:hashlib.sha256((root/name).read_bytes()).hexdigest() for name in locked},"Source/dependency mutation")
  require(identities[0]==identities[1],"Dependency drift")
  require(run(["go","mod","verify"],baseline,evidence/"module-cache-verification.txt",commands)==0,"Module cache integrity failed")
  report["gate"]="A1_LINUX_MODULES_PASS" if all(result["status"]=="PASS" for group in report["results"].values() for result in group.values()) else "BLOCKED"
 except Exception as error: report["error"]=str(error)
 save(evidence/"comparison.json",report);print(json.dumps(report,indent=2))
 with Path(os.environ["GITHUB_STEP_SUMMARY"]).open("a") as output: output.write("```json\n"+json.dumps(report,indent=2)+"\n```\n")
 return 0 if report["gate"]=="A1_LINUX_MODULES_PASS" else 1
if __name__=="__main__": sys.exit(main())
