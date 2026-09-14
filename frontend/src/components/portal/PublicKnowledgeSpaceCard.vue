<template>
  <article class="public-space-card">
    <div class="heading">
      <span class="mark">{{ space.display_name.slice(0, 1).toUpperCase() }}</span>
    </div>
    <h3>{{ space.display_name }}</h3>
    <p>{{ space.description || t('portal.noDescription') }}</p>
    <div class="counts">
      <span><t-icon name="folder" />{{ t('portal.knowledgeBaseCount', { count: space.knowledge_base_count }) }}</span>
      <span><t-icon name="file" />{{ t('portal.fileCount', { count: space.file_count }) }}</span>
    </div>
    <div class="footer">
      <span class="state">{{ stateLabel }}</span>
      <t-button v-if="action" size="small" variant="text" @click="activate">
        {{ action.label }} <t-icon name="chevron-right" />
      </t-button>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PortalSpace } from '@/api/portal'

const props = defineProps<{ space: PortalSpace }>()
const emit = defineEmits<{
  request: [space: PortalSpace]
  enter: [space: PortalSpace]
}>()
const { t } = useI18n()

const stateLabel = computed(() => {
  if (props.space.access_state === 'accessible') {
    const role = props.space.current_role ? t(`portal.roles.${props.space.current_role}`) : ''
    return t('portal.joined', { role })
  }
  if (props.space.access_request_pending) return t('portal.pending')
  if (props.space.membership_suspended) return t('portal.suspended')
  return props.space.can_request_access ? t('portal.noAccess') : t('portal.restricted')
})

const action = computed(() => {
  if (props.space.access_state === 'accessible') return { event: 'enter' as const, label: t('portal.enterWorkspace') }
  if (props.space.can_request_access) return { event: 'request' as const, label: t('portal.requestAccess') }
  return null
})

function activate() {
  if (action.value?.event === 'enter') emit('enter', props.space)
  else if (action.value?.event === 'request') emit('request', props.space)
}
</script>

<style scoped lang="less">
.public-space-card{display:flex;min-height:220px;flex-direction:column;padding:18px;border:1px solid var(--td-component-stroke);border-radius:12px;background:var(--td-bg-color-container)}
.heading{display:flex;align-items:center;justify-content:space-between;color:var(--td-text-color-placeholder)}
.mark{width:36px;height:36px;display:grid;place-items:center;border-radius:10px;background:var(--td-brand-color-light);color:var(--td-brand-color);font-weight:700}
h3{margin:14px 0 7px;font-size:17px}
p{display:-webkit-box;min-height:44px;margin:0;overflow:hidden;color:var(--td-text-color-secondary);line-height:1.55;-webkit-box-orient:vertical;-webkit-line-clamp:2}
.counts{display:flex;flex-wrap:wrap;gap:8px 16px;margin-top:16px;color:var(--td-text-color-secondary);font-size:12px}.counts span{display:flex;align-items:center;gap:5px}
.footer{display:flex;align-items:center;justify-content:space-between;gap:8px;margin-top:auto;padding-top:15px}.state{min-width:0;overflow:hidden;color:var(--td-text-color-placeholder);font-size:12px;text-overflow:ellipsis;white-space:nowrap}
@media(max-width:680px){.public-space-card{min-height:200px}}
</style>
