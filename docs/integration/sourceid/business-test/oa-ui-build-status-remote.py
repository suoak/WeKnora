"""Only synthetic build phase markers; no service logs or environment."""
base = pathlib.Path(UI_BUILD_DIRECTORY)
state = json.loads((base / 'build-status.json').read_text())
logs = {}
for name in ('controller-frontend.log', 'frontend-types.log', 'frontend-build.log', 'npm-ci.log', 'controller-image.log'):
    path = base / 'artifacts' / name
    if path.exists():
        raw = path.read_bytes()
        logs[name] = {'bytes': len(raw), 'build_complete': b'built in ' in raw,
                      'transforming': b'transforming' in raw, 'rendering': b'rendering chunks' in raw,
                      'out_of_memory': b'heap out of memory' in raw, 'type_error': b'error TS' in raw}
print(json.dumps({'state': state, 'logs': logs}))
