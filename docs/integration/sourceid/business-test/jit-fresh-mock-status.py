R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018')
print((R/'mock-result.json').read_text() if (R/'mock-result.json').exists() else json.dumps({'result':'PREPARING'}))
