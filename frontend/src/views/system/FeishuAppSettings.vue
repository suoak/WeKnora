<template>
  <div class="settings-group" style="padding: 16px; margin-bottom: 16px">
    <h3>{{ t('app') }}</h3>
    <t-select v-model="config.auth_mode" :options="[{ label: t('applicationMode'), value: 'application_grant' }, { label: t('personalMode'), value: 'user_oauth' }]" style="margin-bottom: 12px" />
    <p v-if="config.auth_mode === 'application_grant'">{{ t('applicationRestricted') }}</p>
    <t-input v-model="config.app_id" :placeholder="t('appID')" style="margin-bottom: 12px" />
    <t-input v-model="config.client_secret" type="password" autocomplete="new-password" :placeholder="t('secret')" style="margin-bottom: 12px" />
    <t-input v-if="config.auth_mode === 'user_oauth'" v-model="config.redirect_uri" :placeholder="t('callback')" style="margin-bottom: 12px" />
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
const config = reactive({ auth_mode: 'application_grant' as 'user_oauth' | 'application_grant', app_id: '', client_secret: '', redirect_uri: `${location.origin}/feishu/callback`, enabled: false })
const busy = ref(false), message = ref('')
onMounted(async () => { try { const saved = await getFeishuApp(); if (saved.configured) Object.assign(config, saved) } catch { message.value = t('failed') } })
async function save() { busy.value = true; try { await saveFeishuApp({ ...config, redirect_uri: config.auth_mode === 'application_grant' ? '' : config.redirect_uri }); config.client_secret = ''; message.value = t('saved') } catch { message.value = t('failed') } finally { busy.value = false } }
async function test() { busy.value = true; try { await testFeishuApp(); message.value = t('tested') } catch { message.value = t('failed') } finally { busy.value = false } }
</script>
