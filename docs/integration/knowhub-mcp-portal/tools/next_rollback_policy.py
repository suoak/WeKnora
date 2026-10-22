"""Read-only rollback admission; never changes APP, DB or feature flags.

An image build alone is not approval. The proof must bind exact image IDs,
schema, candidate and the retained denial/restore verification artifact.
"""
from pathlib import Path
import hashlib,json,sys

REQUIRED={
 'forward_upgrade','compatibility_cold_start','revoked_hmac_denied',
 'revoked_rest_denied','historical_reference_denied','feature_off_denied',
 'restore_denial_persisted','return_to_candidate','schema_retained',
}
UNSAFE='sha256:0c84cac5af5103b73102c6261f32130d3e51eeed1b8cbf77fda61de20c4bd983'

def admit(image_id, approval, proof_bytes):
 if image_id==UNSAFE:raise ValueError('known unsafe RC3 Reader: revoked HMAC returned 200')
 if approval.get('image_id')!=image_id or approval.get('schema')!=3037:
  raise ValueError('image/schema mismatch')
 if approval.get('proof_sha256')!=hashlib.sha256(proof_bytes).hexdigest():
  raise ValueError('proof fingerprint mismatch')
 proof=json.loads(proof_bytes)
 if proof.get('image_id')!=image_id or proof.get('candidate_sha256')!=approval.get('candidate_sha256'):
  raise ValueError('proof identity mismatch')
 if not all(proof.get('checks',{}).get(n)=='PASS'for n in REQUIRED):
  raise ValueError('denial, restore and forward safety gates incomplete')
 if proof.get('production_input')is not False:raise ValueError('requires isolated synthetic verification')
 return {'admitted':True,'image_id':image_id,'schema':3037,'down_migration_allowed':False,
         'limitation':'admission is not production deployment authorization'}

if __name__=='__main__':
 image_id,approval,proof=sys.argv[1:]
 print(json.dumps(admit(image_id,json.loads(Path(approval).read_text()),Path(proof).read_bytes())))
