import { get, put } from '@/utils/request'
import type { DefaultModelPolicy } from './index'

export type WorkspaceModelDefaults = Omit<DefaultModelPolicy, 'summary_model_id'> & { utility_model_id: string }

export function emptyWorkspaceDefaults(): WorkspaceModelDefaults {
  return { chat_model_id: '', utility_model_id: '', embedding_model_id: '', rerank_model_id: '', vlm_model_id: '', asr_model_id: '' }
}

export function getWorkspaceModelDefaults() {
  return get<{ success: boolean; data: WorkspaceModelDefaults }>('/api/v1/tenants/kv/default-settings')
}

export function saveWorkspaceModelDefaults(settings: WorkspaceModelDefaults, previous: WorkspaceModelDefaults) {
  return put<{ success: boolean; data: WorkspaceModelDefaults }>('/api/v1/tenants/kv/default-settings', { settings, previous })
}
