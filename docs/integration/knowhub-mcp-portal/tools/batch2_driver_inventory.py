"""Source-only prefilter inventory. Does not certify a driver or query data."""
import json
from pathlib import Path
import sys

root=Path(__file__).resolve().parents[4]
drivers=[]
for directory in sorted((root/'internal/application/repository/retriever').iterdir()):
    if not directory.is_dir():continue
    locations=[]
    for path in directory.rglob('*.go'):
        if path.name.endswith('_test.go'):continue
        for number,line in enumerate(path.read_text(encoding='utf-8').splitlines(),1):
            if 'params.KnowledgeIDs' in line or 'p.KnowledgeIDs' in line or 'ExcludeKnowledgeIDs' in line:
                locations.append({'path':path.relative_to(root).as_posix(),'line':number,'expression':line.strip()})
    drivers.append({'driver':directory.name,'allow_set_source_found':bool(locations),'locations':locations,
                    'new_document_acl_runtime_validation':'NOT_TESTED','deny_all_integration':'NOT_TESTED',
                    'activation':'HOLD until actual mode-specific canary verification'})
output=Path(sys.argv[1])
output.parent.mkdir(parents=True,exist_ok=True)
output.write_text(json.dumps(drivers,indent=2)+'\n',encoding='utf-8')
print(json.dumps({'drivers':len(drivers),'source_locations':sum(len(x['locations'])for x in drivers),
                  'runtime_certified':0}))
