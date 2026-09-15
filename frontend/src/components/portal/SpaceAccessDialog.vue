<template>
  <t-dialog :visible="visible" :header="space ? t('portalMap.accessDialog.titleWithSpace', { space: space.display_name }) : t('portalMap.accessDialog.title')" :footer="false" width="480px" @close="$emit('close')">
    <div v-if="space" class="access-dialog">
      <div class="access-intro"><span class="lock-mark"><t-icon name="lock-on" /></span><div><h3>{{ t('portalMap.accessDialog.notice') }}</h3><p>{{ t('portalMap.accessDialog.explanation') }}</p></div></div>
      <div class="space-facts">
        <div><span>{{ t('portalMap.accessDialog.spaceLabel') }}</span><strong>{{ space.display_name }}</strong></div>
        <div v-if="stageLabel"><span>{{ t('portalMap.accessDialog.stageLabel') }}</span><strong>{{ stageLabel }}</strong></div>
      </div>
      <div class="inventory"><span>{{ t('portal.knowledgeBaseCount', { count: space.knowledge_base_count }) }}</span><span>{{ t('portal.fileCount', { count: space.file_count }) }}</span></div>
      <div class="access-benefits"><span>{{ t('portalMap.accessDialog.afterAccess') }}</span><ul><li><t-icon name="check" />{{ t('portalMap.accessDialog.browse') }}</li><li><t-icon name="check" />{{ t('portalMap.accessDialog.search') }}</li><li><t-icon name="check" />{{ t('portalMap.accessDialog.ask') }}</li></ul></div>
      <dl v-if="space.responsible_team || space.contact">
        <div v-if="space.responsible_team"><dt>{{ t('portal.responsibleTeam') }}</dt><dd>{{ space.responsible_team }}</dd></div>
        <div v-if="space.contact"><dt>{{ t('portal.contact') }}</dt><dd>{{ space.contact }}</dd></div>
      </dl>
      <template v-if="space.access_request_pending"><div class="pending"><t-icon name="time" />{{ t('portalMap.accessDialog.pending') }}</div></template>
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
import type { PortalSpace, PortalStage } from '@/api/portal'
import { validAccessReason } from '@/stores/portalState'
const props=defineProps<{visible:boolean;space:PortalSpace|null;stages:PortalStage[];submitting:boolean}>()
const emit=defineEmits<{close:[];request:[reason:string]}>()
const {t}=useI18n()
const reason=ref('')
const valid=computed(()=>validAccessReason(reason.value))
const stageLabel=computed(()=>{
  const index=props.stages.findIndex(stage=>props.space?.stages.includes(stage.key))
  if(index<0)return ''
  const stage=props.stages[index]
  return `${String(index+1).padStart(2,'0')} ${stage.name||stage.key}`
})
watch(()=>props.visible,(visible)=>{if(visible)reason.value=''})
function submit(){if(valid.value)emit('request',reason.value.trim())}
</script>

<style scoped lang="less">
.access-dialog{display:flex;flex-direction:column;gap:14px}.access-intro{display:flex;gap:11px}.lock-mark{width:34px;height:34px;display:grid;flex:none;place-items:center;border-radius:50%;background:var(--td-bg-color-secondarycontainer);color:var(--td-text-color-secondary)}.access-dialog h3{margin:0;font-size:15px}.access-dialog p{margin:4px 0 0;color:var(--td-text-color-secondary);font-size:12px;line-height:1.55}.space-facts{display:grid;grid-template-columns:1fr 1fr;gap:8px;padding:11px 12px;border:1px solid var(--td-component-stroke);border-radius:8px;background:color-mix(in srgb,var(--td-bg-color-secondarycontainer) 50%,var(--td-bg-color-container))}.space-facts div{min-width:0}.space-facts span,.space-facts strong{display:block}.space-facts span{color:var(--td-text-color-placeholder);font-size:10px}.space-facts strong{margin-top:3px;overflow:hidden;font-size:12px;text-overflow:ellipsis;white-space:nowrap}.inventory{display:flex;gap:20px;color:var(--td-text-color-placeholder);font-size:11px}.access-benefits>span{color:var(--td-text-color-secondary);font-size:11px}.access-benefits ul{display:grid;grid-template-columns:repeat(3,1fr);gap:6px;margin:7px 0 0;padding:0;list-style:none}.access-benefits li{display:flex;align-items:center;gap:4px;color:var(--td-text-color-secondary);font-size:11px}.access-benefits .t-icon{color:var(--td-success-color)}.access-dialog dl{margin:0;font-size:11px}.access-dialog dl div{display:flex;gap:8px;margin-top:4px}.access-dialog dt{color:var(--td-text-color-placeholder)}.access-dialog dd{margin:0}.pending,.contact-admin{padding:10px 12px;border-radius:8px;background:var(--td-bg-color-secondarycontainer);color:var(--td-text-color-secondary);font-size:12px}.pending{display:flex;align-items:center;gap:6px}.dialog-actions{display:flex;justify-content:flex-end;gap:6px}@media(max-width:520px){.space-facts,.access-benefits ul{grid-template-columns:1fr}}
</style>
