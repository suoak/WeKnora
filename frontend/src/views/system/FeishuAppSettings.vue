<template>
  <div class="settings-group" style="padding: 16px; margin-bottom: 16px">
    <h3>{{ t('app') }}</h3>
    <label>{{ t('dualIdentity') }} <t-switch v-model="v2" :disabled="!v2Available || savedV2" /></label>
    <template v-if="v2">
      <p><label>{{ t('applicationMode') }} <t-switch v-model="capabilities.application_enabled" /></label></p>
      <p><label>{{ t('personalMode') }} <t-switch v-model="capabilities.personal_enabled" /></label></p>
    </template>
    <t-select v-else v-model="config.auth_mode" :options="[{ label: t('applicationMode'), value: 'application_grant' }, { label: t('personalMode'), value: 'user_oauth' }]" style="margin-bottom: 12px" />
    <p v-if="config.auth_mode === 'application_grant'">{{ t('applicationRestricted') }}</p>
    <t-input v-model="config.app_id" :placeholder="t('appID')" style="margin-bottom: 12px" />
    <t-input v-model="config.client_secret" type="password" autocomplete="new-password" :placeholder="t('secret')" style="margin-bottom: 12px" />
    <t-input v-if="v2 ? capabilities.personal_enabled : config.auth_mode === 'user_oauth'" v-model="config.redirect_uri" :placeholder="t('callback')" style="margin-bottom: 12px" />
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
const v2 = ref(false), savedV2 = ref(false), v2Available = ref(false)
const capabilities = reactive({ application_enabled: false, personal_enabled: false })
onMounted(async () => { try { const saved = await getFeishuApp(); v2Available.value = !!saved.v2_available; if (saved.configured) { Object.assign(config, saved); if (saved.capabilities) { v2.value = true; savedV2.value = true; Object.assign(capabilities, saved.capabilities) } } } catch { message.value = t('failed') } })
async function save() { busy.value = true; try { const { capabilities: _old, ...base } = config as typeof config & { capabilities?: unknown }; await saveFeishuApp({ ...base, ...(v2.value ? { capabilities: { ...capabilities } } : {}), redirect_uri: v2.value ? config.redirect_uri : config.auth_mode === 'application_grant' ? '' : config.redirect_uri }); savedV2.value = v2.value; config.client_secret = ''; message.value = t('saved') } catch { message.value = t('failed') } finally { busy.value = false } }
async function test() { busy.value = true; try { await testFeishuApp(); message.value = t('tested') } catch { message.value = t('failed') } finally { busy.value = false } }
</script>
