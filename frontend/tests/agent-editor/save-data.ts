import { testAgent } from './fixtures'
export const saveAgent = { ...testAgent, config: { ...testAgent.config,
  agent_mode: 'quick-answer', allowed_tools: [], kb_selection_mode: 'selected', knowledge_bases: ['local-kb'],
  system_prompt: 'Original system', context_template: 'Original context', multi_turn_enabled: true, enable_rewrite: true,
  rewrite_prompt_system: 'Original rewrite system', rewrite_prompt_user: 'Original rewrite user {{query}}',
  fallback_strategy: 'model', fallback_prompt: 'Original fallback {{query}}', fallback_response: 'Original fixed response',
  embedding_top_k: 7, keyword_threshold: 0.21, vector_threshold: 0.32, rerank_top_k: 4, rerank_threshold: 0.43,
  enable_query_expansion: false, query_understand_model_id: 'existing-query-model',
} }
export const preservedKeys = ['model_id', 'rerank_model_id', 'mcp_selection_mode', 'mcp_services', 'skills_selection_mode', 'selected_skills',
  'kb_selection_mode', 'knowledge_bases', 'allowed_tools', 'context_template', 'rewrite_prompt_system', 'rewrite_prompt_user',
  'fallback_strategy', 'fallback_prompt', 'fallback_response', 'embedding_top_k', 'keyword_threshold', 'vector_threshold',
  'rerank_top_k', 'rerank_threshold', 'enable_query_expansion', 'query_understand_model_id']
