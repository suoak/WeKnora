<template>
  <section class="my-spaces">
    <div class="heading"><div><h2>{{ t('portal.mySpacesTitle') }}</h2><p>{{ t('portal.mySpacesDescription') }}</p></div></div>
    <div v-if="spaces.length" class="items">
      <button v-for="space in spaces" :key="space.tenant_id" @click="$emit('enter', space)">
        <span class="avatar">{{ space.tenant_name.slice(0, 1).toUpperCase() }}</span>
        <span><strong>{{ space.tenant_name }}</strong><small>{{ roleName(space.role) }}</small></span>
        <t-icon name="chevron-right" />
      </button>
    </div>
    <div v-else class="empty">{{ t('portal.noMySpaces') }}</div>
  </section>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { PortalMySpace } from '@/api/portal'
defineProps<{ spaces: PortalMySpace[] }>()
defineEmits<{ enter: [space: PortalMySpace] }>()
const { t } = useI18n()
const roleName=(role:string)=>t(`portal.roles.${role}`)
</script>
<style scoped lang="less">
.heading h2{margin:0;font-size:20px}.heading p{margin:5px 0 0;color:var(--td-text-color-secondary);font-size:13px}.items{display:grid;grid-template-columns:repeat(auto-fill,minmax(230px,1fr));gap:10px;margin-top:14px}.items button{display:grid;grid-template-columns:38px 1fr 18px;align-items:center;gap:10px;border:1px solid var(--td-component-border);background:var(--td-bg-color-container);border-radius:12px;padding:12px;text-align:left;cursor:pointer;color:var(--td-text-color-primary)}.items button:hover{border-color:var(--td-brand-color)}.avatar{width:38px;height:38px;display:grid;place-items:center;border-radius:10px;background:linear-gradient(135deg,#115e59,#2563eb);color:white;font-weight:700}strong,small{display:block}small{margin-top:3px;color:var(--td-text-color-secondary)}.empty{margin-top:14px;border:1px dashed var(--td-component-border);border-radius:12px;padding:20px;color:var(--td-text-color-secondary);background:var(--td-bg-color-secondarycontainer)}
</style>
