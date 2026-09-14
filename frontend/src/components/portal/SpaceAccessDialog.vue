<template>
  <t-dialog :visible="visible" :header="t('portalMap.accessDialog.title')" :footer="false" width="480px" @close="$emit('close')">
    <div v-if="space" class="access-dialog">
      <div><h3>{{ space.display_name }}</h3><p>{{ space.description || t('portal.noDescription') }}</p></div>
      <div class="inventory">
        <span>{{ t('portal.knowledgeBaseCount', { count: space.knowledge_base_count }) }}</span>
        <span>{{ t('portal.fileCount', { count: space.file_count }) }}</span>
      </div>
      <p class="notice"><t-icon name="lock-on" />{{ t('portalMap.accessDialog.notice') }}</p>
      <dl v-if="space.responsible_team || space.contact">
        <div v-if="space.responsible_team"><dt>{{ t('portal.responsibleTeam') }}</dt><dd>{{ space.responsible_team }}</dd></div>
        <div v-if="space.contact"><dt>{{ t('portal.contact') }}</dt><dd>{{ space.contact }}</dd></div>
      </dl>
      <template v-if="space.access_request_pending">
        <div class="pending"><t-icon name="time" />{{ t('portalMap.accessDialog.pending') }}</div>
      </template>
      <template v-else-if="space.can_request_access">
        <t-textarea v-model="reason" :aria-label="t('portal.reasonPlaceholder')" :placeholder="t('portal.reasonPlaceholder')" :maxlength="1000" :autosize="{ minRows: 3, maxRows: 6 }" />
        <div class="dialog-actions"><t-button type="button" variant="text" @click="$emit('close')">{{ t('common.cancel') }}</t-button><t-button type="button" :loading="submitting" :disabled="submitting || !valid" @click="submit">{{ t('portal.requestAccess') }}</t-button></div>
      </template>
      <p v-else class="contact-admin">{{ t('portalMap.accessDialog.contactAdmin') }}</p>
    </div>
  </t-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PortalSpace } from '@/api/portal'
import { validAccessReason } from '@/stores/portalState'
const props=defineProps<{visible:boolean;space:PortalSpace|null;submitting:boolean}>()
const emit=defineEmits<{close:[];request:[reason:string]}>()
const {t}=useI18n()
const reason=ref('')
const valid=computed(()=>validAccessReason(reason.value))
watch(()=>props.visible,(visible)=>{if(visible)reason.value=''})
function submit(){if(valid.value)emit('request',reason.value.trim())}
</script>

<style scoped lang="less">
.access-dialog{display:flex;flex-direction:column;gap:15px}.access-dialog h3{margin:0;font-size:17px}.access-dialog p{margin:5px 0 0;color:var(--td-text-color-secondary);line-height:1.55}.inventory{display:flex;gap:20px;padding:11px 13px;border-radius:8px;background:var(--td-bg-color-secondarycontainer);color:var(--td-text-color-secondary);font-size:12px}.notice{display:flex;align-items:center;gap:7px}.access-dialog dl{margin:0;font-size:12px}.access-dialog dl div{display:flex;gap:8px;margin-top:4px}.access-dialog dt{color:var(--td-text-color-placeholder)}.access-dialog dd{margin:0}.pending,.contact-admin{padding:10px 12px;border-radius:8px;background:var(--td-bg-color-secondarycontainer);color:var(--td-text-color-secondary);font-size:12px}.pending{display:flex;align-items:center;gap:6px}.dialog-actions{display:flex;justify-content:flex-end;gap:6px}
</style>
