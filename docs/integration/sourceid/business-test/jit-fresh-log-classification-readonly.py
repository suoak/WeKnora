R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018');W=R/'real-window'
before=dict(x.split('=',1) for x in (W/'fresh-before.env').read_text().splitlines() if '=' in x)
level=before.get('LOG_LEVEL','').lower();level=level if level in ('debug','info','warn','warning','error','fatal') else 'DEFAULT'
print(json.dumps({'configured_log_level_during_test':level,'stage_and_http_diagnostics_emit_at':'warn','warn_suppressed_by_level':level in ('error','fatal'),'source_diagnostics_enabled_by_window':True,'actual_stage_events_saved':len(json.loads((W/'evidence.json').read_text()).get('events',[])),'raw_logs_or_credentials_read':False}))
