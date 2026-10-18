<template>
  <t-dialog :visible="visible" :header="t('title')" :footer="false" @close="emit('update:visible', false)">
    <p v-if="!status.available">{{ t('unavailable') }}</p>
    <template v-else>
      <t-button v-if="!status.connected" :loading="busy" @click="connect">{{ t('connect') }}</t-button>
      <div v-else>{{ t('connected') }} <t-button variant="text" :disabled="busy" @click="disconnect">{{ t('disconnect') }}</t-button></div>
      <FeishuDriveBrowser v-if="visible && status.connected" :disabled="busy" @select="url = $event" />
      <t-input v-model="url" :placeholder="t('url')" :disabled="busy" style="margin: 16px 0" />
      <p>{{ t('coverage') }}</p>
      <t-button :disabled="!status.connected || !url.trim()" :loading="busy" @click="runImport">{{ t('import') }}</t-button>
      <p v-if="rows.length">{{ t('recent') }}</p>
      <div v-for="row in rows" :key="row.id" style="margin-top: 8px">
        <t-button variant="text" :disabled="busy || !status.connected" @click="url = row.url; runImport()">{{ row.resource_type }} · {{ row.status }} · {{ row.url }}</t-button>
        <a :href="row.url" target="_blank" rel="noopener noreferrer">↗</a>
      </div>
    </template>
    <p v-if="resultMessage" role="status">{{ resultMessage }}</p>
  </t-dialog>
</template>
<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import FeishuDriveBrowser from './FeishuDriveBrowser.vue'
import { feishuMessages, FEISHU_PENDING } from '@/i18n/feishuPersonal'
import { feishuStatus, authorizeFeishu, disconnectFeishu, importFeishu, listFeishuImports, type FeishuImport } from '@/api/feishuPersonal'
const props = defineProps<{ visible: boolean; kbId: string; initialUrl?: string }>()
const emit = defineEmits<{ 'update:visible': [value: boolean]; imported: [] }>()
const { t } = useI18n({ useScope: 'local', messages: feishuMessages })
const status = ref({ configured: false, available: false, connected: false })
const url = ref(''), busy = ref(false), resultMessage = ref(''), rows = ref<FeishuImport[]>([])
watch(() => props.visible, async value => {
  if (!value) return
  url.value = props.initialUrl || ''; resultMessage.value = ''
  try { status.value = await feishuStatus(); rows.value = await listFeishuImports(props.kbId) } catch { resultMessage.value = t('failed') }
})
async function connect() {
  busy.value = true
  try {
    sessionStorage.setItem(FEISHU_PENDING, JSON.stringify({ kbId: props.kbId, url: url.value, path: location.pathname }))
    const result = await authorizeFeishu(), target = new URL(result.authorization_url)
    if (target.origin !== 'https://accounts.feishu.cn') throw new Error('invalid_authorization_origin')
    location.assign(target.href)
  } catch { resultMessage.value = t('failed') } finally { busy.value = false }
}
async function disconnect() {
  busy.value = true
  try { await disconnectFeishu(); status.value = await feishuStatus() } catch { resultMessage.value = t('failed') } finally { busy.value = false }
}
async function runImport() {
  busy.value = true; resultMessage.value = ''
  try { await importFeishu(props.kbId, url.value); rows.value = await listFeishuImports(props.kbId); resultMessage.value = t('queued'); emit('imported') }
  catch (error: any) { resultMessage.value = error?.message || t('failed') }
  finally { busy.value = false }
}
</script>
