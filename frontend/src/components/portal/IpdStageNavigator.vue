<template>
  <nav class="stage-nav" aria-label="IPD 流程阶段">
    <button :class="{ active: modelValue === 'all' }" @click="$emit('update:modelValue', 'all')">
      <strong>全部</strong><small>所有已发布空间</small>
    </button>
    <button v-for="stage in stages" :key="stage.key" :class="{ active: modelValue === stage.key }"
      @click="$emit('update:modelValue', stage.key)">
      <strong>{{ stage.name || stage.key }}</strong><small>{{ stage.description }}</small>
    </button>
  </nav>
</template>

<script setup lang="ts">
import type { PortalStage } from '@/api/portal'
defineProps<{ modelValue: string; stages: PortalStage[] }>()
defineEmits<{ 'update:modelValue': [value: string] }>()
</script>

<style scoped lang="less">
.stage-nav { display:grid; grid-template-columns:repeat(8,minmax(96px,1fr)); gap:8px; overflow-x:auto; padding:2px; }
button { min-width:96px; border:1px solid var(--td-component-border); background:var(--td-bg-color-container); color:var(--td-text-color-primary); border-radius:12px; padding:12px 10px; text-align:left; cursor:pointer; transition:.18s ease; }
button:hover { border-color:var(--td-brand-color); transform:translateY(-1px); }
button.active { color:var(--td-brand-color); border-color:var(--td-brand-color); background:var(--td-brand-color-light); box-shadow:inset 0 0 0 1px var(--td-brand-color); }
strong,small { display:block; white-space:nowrap; }
strong { font-size:14px; } small { color:var(--td-text-color-secondary); font-size:11px; margin-top:4px; }
@media(max-width:900px){.stage-nav{grid-template-columns:repeat(8,120px)}}
</style>
