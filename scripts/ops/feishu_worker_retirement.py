"""Offline-signed retirement of one proven-dead Feishu worker; never opens a gate.

Run plan/execute through the authorized SSH/Docker operator account. Approval
stays on the separate signing host. No application deployment or web identity
is invented by this tool. RecoveryManager.Plan must be generated again afterward.
"""
import argparse
import base64
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import socket
import stat
import subprocess
import time
import uuid


DOMAIN = "knowhub_expired_worker_retirement_v2"


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode()


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def restricted(path):
    p = Path(path)
    info = p.lstat()
    if not stat.S_ISREG(info.st_mode) or (os.name != "nt" and info.st_mode & 0o077):
        raise ValueError("restricted regular file required")
    if info.st_size > 1024 * 1024:
        raise ValueError("bounded artifact required")
    return p.read_bytes()


def write_new(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as stream:
        json.dump(value, stream, indent=2)


REDIS_CLIENT = r'''
import os,socket,ssl,json,base64
addr=os.environ['REDIS_ADDR'];host,port=addr.rsplit(':',1)
s=socket.create_connection((host.strip('[]'),int(port)),timeout=5)
if os.environ.get('REDIS_USE_TLS','').lower()=='true':
 c=ssl.create_default_context()
 if os.environ.get('REDIS_TLS_INSECURE_SKIP_VERIFY','').lower()=='true':c.check_hostname=False;c.verify_mode=ssl.CERT_NONE
 s=c.wrap_socket(s,server_hostname=os.environ.get('REDIS_TLS_SERVER_NAME') or host.strip('[]'))
f=s.makefile('rb')
def reply():
 line=f.readline();kind=line[:1];v=line[1:-2]
 if kind==b'+':return v
 if kind==b'-':raise RuntimeError('redis_precondition_rejected')
 if kind==b':':return int(v)
 if kind==b'$':
  n=int(v)
  if n<0:return None
  out=f.read(n);assert len(out)==n and f.read(2)==b'\r\n';return out
 if kind==b'*':
  n=int(v);return None if n<0 else [reply() for _ in range(n)]
 raise RuntimeError('redis_protocol_rejected')
def cmd(*args):
 v=[x if isinstance(x,bytes) else str(x).encode() for x in args]
 s.sendall(b'*'+str(len(v)).encode()+b'\r\n'+b''.join(b'$'+str(len(x)).encode()+b'\r\n'+x+b'\r\n' for x in v));return reply()
if os.environ.get('REDIS_PASSWORD'):
 if os.environ.get('REDIS_USERNAME'):cmd('AUTH',os.environ['REDIS_USERNAME'],os.environ['REDIS_PASSWORD'])
 else:cmd('AUTH',os.environ['REDIS_PASSWORD'])
cmd('SELECT',int(os.environ.get('REDIS_DB','0')))
def enc(v):
 if isinstance(v,bytes):return {'b64':base64.b64encode(v).decode()}
 if isinstance(v,list):return [enc(x) for x in v]
 return v
'''


SNAPSHOT_LUA = r'''
local p=ARGV[1];local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000)
local ids=redis.call('LRANGE','asynq:{sync}:pending',0,-1)
if #ids>64 then return redis.error_reply('QUEUE_BOUND') end
local tasks={};for _,id in ipairs(ids) do
 table.insert(tasks,{id,redis.call('HGETALL','asynq:{sync}:t:'..id)}) end
local servers={};local keys=redis.call('ZRANGE','asynq:servers',0,-1)
if #keys>128 then return redis.error_reply('SERVER_BOUND') end
table.sort(keys);for _,key in ipairs(keys) do local v=redis.call('GET',key)
 if v then table.insert(servers,{key,v}) end end
return {tostring(now),redis.call('HGETALL',p..'workers'),redis.call('ZRANGE',p..'leases',0,-1,'WITHSCORES'),
 redis.call('HGETALL',p..'admitted'),redis.call('ZRANGE',p..'joining',0,-1,'WITHSCORES'),
 redis.call('ZRANGE',p..'draining',0,-1,'WITHSCORES'),redis.call('GET',p..'membership_revision') or '',
 redis.call('GET',p..'trip') or '',redis.call('EXISTS',p..'permit'),
 redis.call('GET','asynq:{sync}:paused') or '',ids,tasks,servers,
 redis.call('GET',p..'recovery') or '',redis.call('XREVRANGE',p..'audit','+','-','COUNT',1),
 redis.call('LLEN','asynq:{sync}:active'),redis.call('ZCARD','asynq:{sync}:retry'),
 redis.call('ZCARD','asynq:{sync}:scheduled'),redis.call('ZCARD','asynq:{sync}:archived'),
 redis.call('HGETALL',p..'inflight')}
'''


RETIRE_LUA = r'''
local p=ARGV[1];local plan=cjson.decode(ARGV[2]);local old=plan.old_worker;local cur=plan.current_worker
local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000)
if now>plan.expires_ms or now<plan.created_ms-5000 then return redis.error_reply('EXPIRED') end
local snap=plan.snapshot
local members=0
for id,expected in pairs(snap.workers) do
 members=members+1
 if redis.call('HGET',p..'workers',id)~=expected then return redis.error_reply('MEMBERSHIP_CHANGED') end
 local lease=redis.call('ZSCORE',p..'leases',id)
 if id~=cur and (lease~=snap.leases[id] or tonumber(lease or '0')>now-10000) then
  return redis.error_reply('MEMBERSHIP_CHANGED') end
end
if members<2 or members>64 then return redis.error_reply('MEMBERSHIP_BOUND') end
if redis.call('GET',p..'trip')~=plan.trip or redis.call('EXISTS',p..'permit')~=0 or
 redis.call('GET',p..'membership_revision')~=snap.revision or
 redis.call('HLEN',p..'workers')~=members or redis.call('ZCARD',p..'leases')~=members or
 redis.call('HGET',p..'workers',old)~=snap.workers[old] or
 redis.call('HGET',p..'workers',cur)~=snap.workers[cur] or
 redis.call('ZSCORE',p..'leases',old)~=snap.leases[old] or
 tonumber(snap.leases[old])>now-10000 or
 tonumber(redis.call('ZSCORE',p..'leases',cur) or '0')<=now or
 (redis.call('GET',p..'recovery') or '')~=snap.recovery then
 return redis.error_reply('MEMBERSHIP_CHANGED') end
for _,name in ipairs({'admitted','joining','draining'}) do
 local expected=snap[name][old] or ''
 local actual
 if name=='admitted' then actual=redis.call('HGET',p..name,old) or ''
 else actual=redis.call('ZSCORE',p..name,old) or '' end
 if actual~=expected or (name~='admitted' and actual~='' and tonumber(actual)>now) then
  return redis.error_reply('OLD_LIFECYCLE_CHANGED') end
 local currentExpected=snap[name][cur] or '';local currentActual
 if name=='admitted' then currentActual=redis.call('HGET',p..name,cur) or ''
 else currentActual=redis.call('ZSCORE',p..name,cur) or '' end
 if currentActual~=currentExpected then return redis.error_reply('CURRENT_LIFECYCLE_CHANGED') end
end
local audit=redis.call('XREVRANGE',p..'audit','+','-','COUNT',1)
if #audit~=1 or audit[1][1]~=plan.incident then return redis.error_reply('INCIDENT_CHANGED') end
if redis.call('GET','asynq:{sync}:paused')~=snap.pause or
 redis.call('LLEN','asynq:{sync}:active')~=0 or redis.call('ZCARD','asynq:{sync}:retry')~=0 or
 redis.call('ZCARD','asynq:{sync}:scheduled')~=0 or redis.call('ZCARD','asynq:{sync}:archived')~=0 or
 redis.call('HLEN',p..'inflight')~=0 then return redis.error_reply('EXECUTION_CHANGED') end
local ids=redis.call('LRANGE','asynq:{sync}:pending',0,-1)
if #ids~=#snap.ids then return redis.error_reply('QUEUE_CHANGED') end
local at=4
for i,id in ipairs(ids) do
 if id~=snap.ids[i] then return redis.error_reply('QUEUE_CHANGED') end
 local key='asynq:{sync}:t:'..id;local n=tonumber(ARGV[at]);at=at+1
 if not n or redis.call('HLEN',key)~=n then return redis.error_reply('TASK_CHANGED') end
 for j=1,n do
  if redis.call('HGET',key,ARGV[at])~=ARGV[at+1] then return redis.error_reply('TASK_CHANGED') end
  at=at+2
 end
end
local serverkeys=redis.call('ZRANGE','asynq:servers',0,-1);table.sort(serverkeys)
local live={};for _,key in ipairs(serverkeys) do if redis.call('EXISTS',key)==1 then table.insert(live,key) end end
if #live~=#snap.servers then return redis.error_reply('CONSUMER_CHANGED') end
-- Go protobuf map serialization may reorder queue entries on every heartbeat.
-- Normalize only repeated field 5 order; preserve every other field byte.
local function stableConsumer(raw)
 if not raw or #raw>1048576 then return nil end
 local pos=1;local rest={};local queues={}
 local function number()
  local value=0;local scale=1
  for i=1,10 do
   local byte=string.byte(raw,pos);if not byte then return nil end
   pos=pos+1;value=value+(byte%128)*scale
   if byte<128 then return value end
   scale=scale*128
  end
  return nil
 end
 while pos<=#raw do
  local start=pos;local tag=number();if not tag or tag<8 then return nil end
  local wire=tag%8
  if wire==0 then if not number() then return nil end
  elseif wire==2 then
   local size=number();if not size or size>#raw-pos+1 then return nil end
   pos=pos+size
  elseif wire==1 then pos=pos+8
  elseif wire==5 then pos=pos+4
  else return nil end
  if pos>#raw+1 then return nil end
  local bytes=string.sub(raw,start,pos-1)
  if math.floor(tag/8)==5 then
   if wire~=2 then return nil end
   table.insert(queues,bytes)
  else table.insert(rest,bytes) end
 end
 table.sort(queues)
 return table.concat(rest)..table.concat(queues)
end
for i,row in ipairs(snap.servers) do
 local actual=stableConsumer(redis.call('GET',row.key));local expected=stableConsumer(ARGV[at])
 if live[i]~=row.key or not actual or not expected or actual~=expected then
  return redis.error_reply('CONSUMER_CHANGED') end
 at=at+1
end
if at~=#ARGV+1 or not tonumber(snap.revision) or tonumber(snap.revision)>9007199254740990 or
 string.format('%.0f',tonumber(snap.revision))~=snap.revision then
 return redis.error_reply('INVALID_PLAN') end
local typ=redis.call('TYPE',p..'control_audit').ok
if typ~='none' and typ~='stream' then return redis.error_reply('AUDIT_UNAVAILABLE') end
-- Audit precedes every mutation; a failed XADD cannot partially retire a worker.
local event=redis.call('XADD',p..'control_audit','*','event','expired_worker_retired',
 'old_worker',old,'current_worker',cur,'actor',plan.actor,'plan_sha256',plan.plan_digest,
 'approval',ARGV[3],'incident',plan.incident,'previous_revision',snap.revision,
 'old_lease_until_ms',snap.leases[old])
redis.call('HDEL',p..'workers',old);redis.call('ZREM',p..'leases',old)
redis.call('HDEL',p..'admitted',old);redis.call('ZREM',p..'joining',old);redis.call('ZREM',p..'draining',old)
local revision=redis.call('INCR',p..'membership_revision')
return {event,tostring(revision)}
'''


def _decode(value):
    if isinstance(value, dict) and set(value) == {"b64"}:
        return base64.b64decode(value["b64"], validate=True)
    if isinstance(value, list):
        return [_decode(v) for v in value]
    return value


def _pairs(values):
    return {values[i].decode(): values[i + 1].decode() for i in range(0, len(values), 2)}


def snapshot_rows(rows):
    rows = _decode(rows)
    return dict(now=int(rows[0]), workers=_pairs(rows[1]), leases=_pairs(rows[2]),
                admitted=_pairs(rows[3]), joining=_pairs(rows[4]), draining=_pairs(rows[5]),
                revision=rows[6].decode(), trip=rows[7].decode(), permit=rows[8],
                pause=rows[9].decode(), ids=[x.decode() for x in rows[10]],
                tasks=[[id.decode(), [[base64.b64encode(k).decode(), base64.b64encode(v).decode()]
                                      for k, v in zip(fields[::2], fields[1::2])]]
                       for id, fields in rows[11]],
                servers=[dict(key=k.decode(), raw=base64.b64encode(v).decode()) for k, v in rows[12]],
                recovery=rows[13].decode(), audit=rows[14], active=rows[15], retry=rows[16],
                scheduled=rows[17], archived=rows[18], inflight=rows[19])


def _protobuf(data):
    result = {}
    pos = 0

    def number():
        nonlocal pos
        value = shift = 0
        while pos < len(data) and shift < 70:
            b = data[pos]; pos += 1
            value |= (b & 127) << shift
            if b < 128:
                return value
            shift += 7
        raise ValueError("invalid consumer record")

    while pos < len(data):
        tag = number(); wire = tag & 7
        if wire == 0:
            value = number()
        elif wire == 2:
            size = number(); value = data[pos:pos + size]; pos += size
            if len(value) != size:
                raise ValueError("truncated consumer record")
        else:
            raise ValueError("unsupported consumer record")
        result.setdefault(tag >> 3, []).append(value)
    return result


def validate_snapshot(snap, old, current, incident, trip):
    members = set(snap["workers"])
    if old == current or not 2 <= len(members) <= 64 or not {old, current} <= members or set(snap["leases"]) != members:
        raise ValueError("bounded matching registry with old and current workers required")
    if any(int(snap["leases"][worker]) > snap["now"] - 10000 for worker in members - {current}):
        raise ValueError("another registry member is still live")
    if (snap["trip"] != trip or snap["permit"] != 0 or not snap["pause"] or len(snap["ids"]) > 64 or
            any(snap[k] != 0 for k in ("active", "retry", "scheduled", "archived")) or snap["inflight"]):
        raise ValueError("incident or execution not quiescent")
    if int(snap["leases"][old]) > snap["now"] - 10000 or int(snap["leases"][current]) <= snap["now"]:
        raise ValueError("old lease is not expired or current worker is not live")
    if (not re.fullmatch(r"0|[1-9][0-9]{0,15}", snap["revision"]) or
            not 0 <= int(snap["revision"]) < 9007199254740990):
        raise ValueError("invalid membership revision")
    if len(snap["audit"]) != 1 or snap["audit"][0][0].decode() != incident:
        raise ValueError("incident changed")
    audit = _pairs(snap["audit"][0][1])
    if audit.get("event") != "trip" or audit.get("reason") != trip:
        raise ValueError("unexpected incident")
    if snap["recovery"]:
        previous = json.loads(snap["recovery"])
        if previous.get("phase") != "awaiting_review" or not isinstance(previous.get("expires_ms"), int) or previous["expires_ms"] > snap["now"]:
            raise ValueError("previous recovery is not an expired review state")
    if snap["admitted"].get(old) not in (None, snap["workers"][old]):
        raise ValueError("old admitted identity is inconsistent")
    for name in ("joining", "draining"):
        if old in snap[name] and int(snap[name][old]) > snap["now"]:
            raise ValueError("old lifecycle still active")
    maintenance = []
    for row in snap["servers"]:
        f = _protobuf(base64.b64decode(row["raw"], validate=True))
        identity = f[1][0].decode() + ":" + str(f[2][0])
        if identity in members - {current}:
            raise ValueError("an expired registry member still has a live consumer")
        queues = {}
        for entry in f.get(5, []):
            q = _protobuf(entry); queues[q[1][0].decode()] = q[2][0]
        if queues.get("sync", 0) > 0:
            if f.get(9, [0])[0] != 0:
                raise ValueError("maintenance consumer still has active work")
            if identity != current or queues.get("approved_sync", 0) < 1 or f.get(7, [b""])[0] != b"active":
                raise ValueError("unexpected maintenance consumer")
            maintenance.append(identity)
    if maintenance != [current]:
        raise ValueError("one actual current maintenance consumer required")


class Docker(http.client.HTTPConnection):
    def __init__(self):
        super().__init__("localhost", timeout=10)

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(10)
        self.sock.connect("/var/run/docker.sock")


def container(identity, missing_allowed=False):
    if not re.fullmatch(r"[a-f0-9]{12,64}", identity):
        raise ValueError("canonical container identity required")
    conn = Docker()
    try:
        conn.request("GET", "/containers/" + identity + "/json")
        response = conn.getresponse(); body = response.read()
        if response.status == 404 and missing_allowed:
            return {"absent": True}
        if response.status != 200:
            raise ValueError("Docker identity proof unavailable")
        return json.loads(body)
    finally:
        conn.close()


def redis_app(code):
    p = subprocess.run(["docker", "exec", "-i", "WeKnora-app", "python3", "-"],
                       input=(REDIS_CLIENT + "\n" + code).encode(), capture_output=True, timeout=20)
    if p.returncode:
        raise ValueError("Redis precondition rejected; do not retry automatically")
    return json.loads(p.stdout)


def collect(args):
    current = container(args.current_worker.split(":")[0])
    old = container(args.old_worker.split(":")[0], True)
    if not current["State"]["Running"] or (not old.get("absent") and old["State"]["Running"]):
        raise ValueError("container lifecycle not safe")
    env = dict(x.split("=", 1) for x in current["Config"]["Env"] if "=" in x)
    if (env.get("FEISHU_SYNC_SOURCE_SHA") != args.runtime_sha or
            env.get("FEISHU_SYNC_SAFETY_MODE") != "automatic" or
            current["Config"]["Hostname"] + ":1" != args.current_worker):
        raise ValueError("current runtime identity mismatch")
    prefix = "feishu:safety:{" + args.namespace + "}:"
    snap = snapshot_rows(redis_app("print(json.dumps(enc(cmd('EVAL_RO'," + repr(SNAPSHOT_LUA) +
                                   ",0," + repr(prefix) + "))))"))
    validate_snapshot(snap, args.old_worker, args.current_worker, args.incident, args.trip)
    sql = """BEGIN READ ONLY; SET LOCAL statement_timeout='5s';
SELECT json_build_object('claimed',(SELECT count(*) FROM sync_logs WHERE execution_claimed),
'publication_leases',(SELECT count(*) FROM feishu_legacy_attempts a JOIN feishu_legacy_resources r
 ON r.id=a.resource_id WHERE a.id=r.current_attempt AND a.generation=r.generation
 AND a.status IN ('ready','staging') AND a.lease_until>now())); ROLLBACK;"""
    p = subprocess.run(["docker", "exec", "-i", "WeKnora-postgres", "sh", "-c",
                        'exec psql -XqAt -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1'],
                       input=sql.encode(), capture_output=True, timeout=10)
    if p.returncode:
        raise ValueError("execution fence proof unavailable")
    work = json.loads(next(x for x in p.stdout.decode().splitlines() if x.startswith("{")))
    if work != {"claimed": 0, "publication_leases": 0}:
        raise ValueError("Claim or publication still live")
    public_in_app = env.get("FEISHU_SYNC_RECOVERY_APPROVAL_PUBLIC_KEY_FILE")
    mounts = [m for m in current["Mounts"] if m["Destination"] == public_in_app and not m["RW"]]
    if len(mounts) != 1:
        raise ValueError("runtime public-key trust anchor unavailable")
    return snap, dict(current_id=current["Id"], current_image=current["Image"],
                      old_absent=old.get("absent", False),
                      old_finished_at=None if old.get("absent") else old["State"]["FinishedAt"], workload=work), mounts[0]["Source"]


def public_key(path):
    from cryptography.hazmat.backends import default_backend
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
    key = serialization.load_pem_public_key(restricted(path), backend=default_backend())
    if not isinstance(key, Ed25519PublicKey):
        raise ValueError("Ed25519 trust anchor required")
    raw = key.public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
    return key, hashlib.sha256(raw).hexdigest()


def sign(plan, private, reference):
    from cryptography.hazmat.backends import default_backend
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    if not re.fullmatch(r"[A-Za-z0-9:._/-]{1,180}", reference):
        raise ValueError("bounded approval reference required")
    key = serialization.load_pem_private_key(private, password=None, backend=default_backend())
    if not isinstance(key, Ed25519PrivateKey):
        raise ValueError("offline Ed25519 key required")
    raw = key.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
    if hashlib.sha256(raw).hexdigest() != plan["approval_key_id"]:
        raise ValueError("wrong offline approver")
    approval = dict(domain=DOMAIN, plan_sha256=digest(plan), reference=reference,
                    expires_ms=plan["expires_ms"], signature="")
    approval["signature"] = key.sign(canonical(approval)).hex()
    return approval


def execute(args, plan, approval):
    snap, proof, path = collect(args)
    key, key_id = public_key(path)
    actor = "ssh:" + str(os.getuid())
    for name in ("namespace", "runtime_sha", "old_worker", "current_worker", "incident", "trip"):
        if plan.get(name) != getattr(args, name):
            raise ValueError("approved operation identity changed")
    if (plan.get("domain") != DOMAIN or plan.get("actor") != actor or
            plan.get("approval_key_id") != key_id or plan.get("proof") != proof or
            plan["expires_ms"] - plan["created_ms"] != 60000 or
            not plan["created_ms"] - 5000 <= snap["now"] < plan["expires_ms"]):
        raise ValueError("approved runtime proof expired or changed")
    if (approval.get("domain") != DOMAIN or approval.get("plan_sha256") != digest(plan) or
            approval.get("expires_ms") != plan["expires_ms"]):
        raise ValueError("matching independent signature required")
    unsigned = dict(approval); unsigned["signature"] = ""
    key.verify(bytes.fromhex(approval["signature"]), canonical(unsigned))
    unsigned_plan = dict(plan, plan_digest=digest(plan))
    rawargs = []
    for _, fields in plan["snapshot"]["tasks"]:
        rawargs.append(str(len(fields)).encode())
        for k, v in fields:
            rawargs.extend([base64.b64decode(k, validate=True), base64.b64decode(v, validate=True)])
    for row in plan["snapshot"]["servers"]:
        rawargs.append(base64.b64decode(row["raw"], validate=True))
    prefix = "feishu:safety:{" + args.namespace + "}:"
    payload = [base64.b64encode(x).decode() for x in rawargs]
    code = ("tail=[base64.b64decode(x) for x in " + repr(payload) + "];print(json.dumps(enc(cmd('EVAL'," +
            repr(RETIRE_LUA) + ",0," + repr(prefix) + "," + repr(canonical(unsigned_plan).decode()) + "," +
            repr(canonical(approval).decode()) + ",*tail))))")
    result = _decode(redis_app(code))
    return dict(retired_worker=args.old_worker, current_worker_unchanged=args.current_worker,
                audit_id=result[0].decode(), membership_revision=result[1].decode(),
                trip_or_permit_changed=False, shared_queue_changed=False, plan_sha256=digest(plan))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["plan", "approve", "execute"])
    for key in ("namespace", "runtime-sha", "old-worker", "current-worker", "incident", "trip", "output"):
        parser.add_argument("--" + key, required=True)
    parser.add_argument("--plan-file"); parser.add_argument("--approval-file")
    parser.add_argument("--approval-key-file"); parser.add_argument("--reference")
    args = parser.parse_args()
    if (not re.fullmatch(r"[A-Za-z0-9_.-]{1,100}", args.namespace) or
            not re.fullmatch(r"[0-9a-f]{40}", args.runtime_sha) or
            any(not re.fullmatch(r"[a-f0-9]{12,64}:1", x) for x in (args.old_worker, args.current_worker)) or
            args.old_worker == args.current_worker or not re.fullmatch(r"[0-9]+-[0-9]+", args.incident)):
        raise ValueError("bounded canonical incident identities required")
    if args.action == "plan":
        snap, proof, keypath = collect(args)
        _, key_id = public_key(keypath)
        plan = {k: getattr(args, k) for k in ("namespace", "runtime_sha", "old_worker", "current_worker", "incident", "trip")}
        # Audit bytes are checked during collection; only its immutable ID is needed in Lua.
        del snap["audit"]; del snap["inflight"]
        plan.update(domain=DOMAIN, id=str(uuid.uuid4()), actor="ssh:" + str(os.getuid()),
                    approval_key_id=key_id, created_ms=snap["now"], expires_ms=snap["now"] + 60000,
                    snapshot=snap, proof=proof)
        result = plan
    elif args.action == "approve":
        result = sign(json.loads(restricted(args.plan_file)), restricted(args.approval_key_file), args.reference)
    else:
        result = execute(args, json.loads(restricted(args.plan_file)), json.loads(restricted(args.approval_file)))
    write_new(args.output, result)
    print(json.dumps({"operation": args.action, "opens_gate": False, "shared_queue_resumed": False}))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"error": "worker_retirement_rejected", "category": type(error).__name__}))
        raise SystemExit(1)
