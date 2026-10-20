"""Export only whitelisted aggregate candidate receipts."""
import pathlib,json
b=pathlib.Path('/home/aqyfzx/ai/knowhub-legacy-clean-target-20261016')
p=b/'acceptance-result.json'
print(p.read_text() if p.exists() else (b/'preparation-status.json').read_text())
