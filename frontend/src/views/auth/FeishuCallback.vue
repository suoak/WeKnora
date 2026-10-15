<template><main style="padding: 40px"><p role="status">{{ message }}</p><t-button @click="back">{{ t('back') }}</t-button></main></template>
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { completeFeishu } from '@/api/feishuPersonal'
import { feishuMessages, FEISHU_PENDING } from '@/i18n/feishuPersonal'
const { t } = useI18n({ useScope: 'local', messages: feishuMessages })
const router = useRouter(), message = ref('…')
let path = '/platform/knowledge-bases'
onMounted(async () => {
  const query = new URLSearchParams(location.search), code = query.get('code') || '', state = query.get('state') || ''
  history.replaceState(null, '', '/feishu/callback')
  try { const pending = JSON.parse(sessionStorage.getItem(FEISHU_PENDING) || '{}'); if (typeof pending.path === 'string' && pending.path.startsWith('/platform/')) path = pending.path } catch { /* default route */ }
  try { if (query.has('error')) throw new Error('authorization_denied'); await completeFeishu(state, code); message.value = t('connected'); await router.replace(path) } catch { message.value = t('callbackFailed') }
})
function back() { router.replace(path) }
</script>
