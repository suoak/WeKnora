import { get } from '@/utils/request';

export interface SourceRestriction {
  restricted: boolean;
  source_state: 'available' | 'hold' | 'revoked' | 'unverified';
  reason_code?: string;
  historical_processing_state: 'unverified';
}

export async function getSourceRestriction(kbId: string): Promise<SourceRestriction> {
  const response: any = await get(`/api/v1/knowledge-bases/${encodeURIComponent(kbId)}/source-restriction`);
  if (!response?.success || !response.data) throw new Error('source_status_unavailable');
  return response.data;
}
