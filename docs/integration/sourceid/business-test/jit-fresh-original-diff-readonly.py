import sys
R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018');W=R/'real-window';sys.path.insert(0,str(W))
from runtime import baseline
old=json.loads((W/'original-baseline.json').read_text());new=baseline();diff={}
for table,rows in old.items():
 a={str(x['row']['id']):x['row'] for x in rows};b={str(x['row']['id']):x['row'] for x in new[table]}
 fields={k for uid in a.keys()&b.keys() for k in a[uid] if a[uid].get(k)!=b[uid].get(k)}
 if a!=b:diff[table]={'before_count':len(a),'after_count':len(b),'added':len(b.keys()-a.keys()),'removed':len(a.keys()-b.keys()),'changed_columns':sorted(fields)}
print(json.dumps({'result':'READ_ONLY_DIFF','differences':diff,'identity_values_exported':False}))
