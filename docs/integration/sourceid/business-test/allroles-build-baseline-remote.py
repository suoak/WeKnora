base=pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b')
print(json.dumps({'build_app_script':(base/'build-app.sh').read_text(),'dockerfile':(base/'Dockerfile.app').read_text(),'builder_tools_exist':pathlib.Path('/home/aqyfzx/ai/knowhub-feishu-personal-20261014/tools').is_dir()}))
