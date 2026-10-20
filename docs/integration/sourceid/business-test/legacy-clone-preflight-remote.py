"""Read-only packaging capacity and pinned runtime baseline, no credentials."""
import shutil
root = pathlib.Path('/home/aqyfzx/ai')
du = shutil.disk_usage(root)
available = int(next(x.split()[1] for x in pathlib.Path('/proc/meminfo').read_text().splitlines() if x.startswith('MemAvailable:'))) * 1024
projects = {}
for project in ('weknora', 'knowhub-sourceid-business'):
    ids = run(['docker','ps','-a','--filter','label=com.docker.compose.project='+project,'--format','{{.ID}}']).decode().split()
    projects[project] = [metadata(cid) for cid in ids]
images = {}
for ref in ('knowhub-build/golang:1.26-bookworm-ustc','sha256:b2751ba9c8797b0c2440b9435ad92ba9ab2c30d4a95b0c28a2b1b34e4a4c3af1'):
    images[ref] = run(['docker','image','inspect','--format','{{.Id}}',ref]).decode().strip()
assert du.free > 12 * 1024**3 and available > 7 * 1024**3
assert next(x for x in projects['knowhub-sourceid-business'] if x['service']=='app')['image_id'] == 'sha256:b2751ba9c8797b0c2440b9435ad92ba9ab2c30d4a95b0c28a2b1b34e4a4c3af1'
print(json.dumps({'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'target':'172.30.232.42','disk_free_bytes':du.free,'memory_available_bytes':available,'projects':projects,'images':images,'result':'PASS','changes':False}))
