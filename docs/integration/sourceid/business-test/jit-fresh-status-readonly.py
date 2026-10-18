R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018')
print((R/'prepare-result.json').read_text() if (R/'prepare-result.json').exists() else json.dumps({'result':'PREPARING'}))
