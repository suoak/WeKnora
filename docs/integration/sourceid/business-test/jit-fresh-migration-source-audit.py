I='sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b';D=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-implementation-20261017/source/migrations/versioned')
z=subprocess.run(['docker','run','--rm','--network','none','--log-driver','none','--entrypoint','migrate',I,'-help'],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
print(json.dumps({'migration_up_files':len(list(D.glob('*.up.sql'))),'first_files':[p.name for p in sorted(D.glob('*.up.sql'))[:4]],'help':(z.stdout+z.stderr).decode()[:4500],'image_user':inspect(I,'.Config.User')}))
