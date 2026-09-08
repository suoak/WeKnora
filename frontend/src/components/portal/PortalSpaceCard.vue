<template>
  <article class="card" :class="{ featured: space.featured }">
    <div class="top"><span class="mark">{{ space.display_name.slice(0,1).toUpperCase() }}</span><t-tag v-if="space.featured" theme="primary" variant="light">{{ t('portal.featured') }}</t-tag></div>
    <h3>{{ space.display_name }}</h3>
    <p class="description">{{ space.description || t('portal.noDescription') }}</p>
    <div class="tags"><t-tag v-if="space.category" theme="success" variant="light">{{ t('portal.categoryTag', { category: space.category }) }}</t-tag><t-tag v-for="stage in space.stages" :key="stage" variant="light">{{ stageName(stage) }}</t-tag></div>
    <div class="resource-counts"><span><t-icon name="folder" />{{ t('portal.knowledgeBaseCount', { count: space.knowledge_base_count }) }}</span><span><t-icon name="file" />{{ t('portal.fileCount', { count: space.file_count }) }}</span></div>
    <dl v-if="space.responsible_team || space.contact"><div v-if="space.responsible_team"><dt>{{ t('portal.responsibleTeam') }}</dt><dd>{{ space.responsible_team }}</dd></div><div v-if="space.contact"><dt>{{ t('portal.contact') }}</dt><dd>{{ space.contact }}</dd></div></dl>
    <div class="footer">
      <span v-if="space.access_state==='member'" class="state ok">✓ {{ t('portal.joined', { role: roleName(space.current_role) }) }}</span>
      <span v-else-if="space.access_state==='pending'" class="state pending">◷ {{ t('portal.pending') }}</span>
      <span v-else-if="space.access_state==='suspended'" class="state suspended">⚠ {{ t('portal.suspended') }}</span>
      <span v-else-if="space.can_request_access" class="state">🔒 {{ t('portal.noAccess') }}</span>
      <span v-else class="state">🔒 {{ t('portal.restricted') }}</span>
      <div class="actions">
        <t-button v-if="space.access_state==='member'" size="small" @click="$emit('enter', space)">{{ t('portal.enterWorkspace') }}</t-button>
        <t-button v-else-if="space.can_request_access" size="small" variant="outline" @click="$emit('request', space)">{{ t('portal.requestAccess') }}</t-button>
        <t-button v-if="space.interaction_action==='enter'" size="small" variant="text" @click="$emit('interaction', space)">{{ t('portal.enterInteraction') }}</t-button>
      </div>
    </div>
  </article>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { PortalSpace } from '@/api/portal'
const props=defineProps<{space:PortalSpace;stageLabels:Record<string,string>}>()
defineEmits<{request:[space:PortalSpace];enter:[space:PortalSpace];interaction:[space:PortalSpace]}>()
const { t } = useI18n()
const roleName=(role:string|null)=>role ? t(`portal.roles.${role}`) : ''
const stageName=(key:string)=>props.stageLabels[key]||key
</script>
<style scoped lang="less">
.card{display:flex;flex-direction:column;min-height:320px;border:1px solid var(--td-component-border);border-radius:16px;padding:20px;background:var(--td-bg-color-container);box-shadow:0 8px 24px rgba(15,23,42,.05)}.card.featured{border-color:color-mix(in srgb,var(--td-brand-color) 48%,var(--td-component-border))}.top{display:flex;justify-content:space-between}.mark{width:42px;height:42px;display:grid;place-items:center;border-radius:12px;background:linear-gradient(135deg,#0f766e,#1d4ed8);color:#fff;font-size:18px;font-weight:700}h3{font-size:18px;margin:15px 0 7px}.description{color:var(--td-text-color-secondary);line-height:1.65;margin:0;min-height:52px}.tags{display:flex;flex-wrap:wrap;gap:6px;margin:14px 0}.resource-counts{display:flex;gap:18px;margin:0 0 14px;color:var(--td-text-color-secondary);font-size:13px}.resource-counts span{display:flex;align-items:center;gap:5px}dl{margin:0 0 14px;font-size:12px}dl div{display:flex;gap:8px;margin-top:4px}dt{color:var(--td-text-color-placeholder)}dd{margin:0;color:var(--td-text-color-secondary);overflow-wrap:anywhere}.footer{margin-top:auto;border-top:1px solid var(--td-component-stroke);padding-top:14px}.state{font-size:13px;color:var(--td-text-color-secondary)}.state.ok{color:var(--td-success-color)}.state.pending{color:var(--td-warning-color)}.state.suspended{color:var(--td-error-color)}.actions{display:flex;flex-wrap:wrap;gap:6px;margin-top:10px}
</style>
