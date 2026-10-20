import importlib.util,urllib.parse
s=importlib.util.spec_from_file_location('legacy','/home/aqyfzx/ai/knowhub-legacy79-worker-20261016.py');m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
rows=m.scalar("SELECT json_agg(json_build_object('path',file_path,'bytes',file_size)) FROM knowledges WHERE deleted_at IS NULL")
root=m.BASE/'files';actual=[p for p in root.rglob('*') if p.is_file()]
out={'documents':len(rows),'physical_files':len(actual),'local_scheme':0,'absolute_paths':0,'other_scheme':0,'relative_paths':0,'basename_matches':0,'size_matches':0,'suffix_matches':0,'exact_matches':0}
for row in rows:
    raw=row['path'];parsed=urllib.parse.urlsplit(raw)
    out['local_scheme' if raw.startswith('local://') else 'other_scheme' if parsed.scheme else 'absolute_paths' if raw.startswith('/') else 'relative_paths']+=1
    candidates=[p for p in actual if p.name==pathlib.Path(raw).name]
    out['basename_matches']+=len(candidates)==1
    out['size_matches']+=any(p.stat().st_size==row['bytes'] for p in actual)
    out['suffix_matches']+=any(raw.endswith(str(p.relative_to(root))) for p in actual)
print(json.dumps(out))
