<template>
  <div class="settings-group" style="padding: 16px; margin-bottom: 16px">
    <h3>{{ t('app') }}</h3>
    <t-input v-model="config.app_id" :placeholder="t('appID')" style="margin-bottom: 12px" />
    <t-input v-model="config.client_secret" type="password" autocomplete="new-password" :placeholder="t('secret')" style="margin-bottom: 12px" />
    <t-input v-model="config.redirect_uri" :placeholder="t('callback')" style="margin-bottom: 12px" />
    <label>{{ t('enabled') }} <t-switch v-model="config.enabled" /></label>
    <div style="margin-top: 12px"><t-button :loading="busy" @click="save">{{ t('save') }}</t-button> <t-button :disabled="busy" variant="outline" @click="test">{{ t('test') }}</t-button></div>
    <p v-if="message" role="status">{{ message }}</p>
  </div>
</template>
<script setup lang="ts">
import { reactive, ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { feishuMessages } from '@/i18n/feishuPersonal'
import { getFeishuApp, saveFeishuApp, testFeishuApp } from '@/api/feishuPersonal'
const { t } = useI18n({ useScope: 'local', messages: feishuMessages })
const config = reactive({ app_id: '', client_secret: '', redirect_uri: `${location.origin}/feishu/callback`, enabled: false })
const busy = ref(false), message = ref('')
onMounted(async () => { try { const saved = await getFeishuApp(); if (saved.configured) Object.assign(config, saved) } catch { message.value = t('failed') } })
async function save() { busy.value = true; try { await saveFeishuApp(config); config.client_secret = ''; message.value = t('saved') } catch { message.value = t('failed') } finally { busy.value = false } }
async function test() { busy.value = true; try { await testFeishuApp(); message.value = t('tested') } catch { message.value = t('failed') } finally { busy.value = false } }
</script>
