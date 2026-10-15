"""Candidate tool capability check, network disabled, no credentials."""
image = 'sha256:b2751ba9c8797b0c2440b9435ad92ba9ab2c30d4a95b0c28a2b1b34e4a4c3af1'
paths = run(['docker', 'run', '--rm', '--network', 'none', '--log-driver', 'none', '--entrypoint', 'sh', image,
             '-c', 'command -v python3; command -v migrate']).decode().splitlines()
assert len(paths) == 2 and all(re.fullmatch(r'/[A-Za-z0-9_./-]+', p) for p in paths)
print(json.dumps({'python3': paths[0], 'migrate': paths[1], 'credential_use': False}))
