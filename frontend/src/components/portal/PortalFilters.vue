<template>
  <div class="filters">
    <t-input :value="search" clearable placeholder="搜索空间名称、描述、知识领域或阶段"
      @update:value="$emit('update:search', String($event || ''))">
      <template #prefix-icon><t-icon name="search" /></template>
    </t-input>
    <t-select :value="category" clearable placeholder="全部知识领域" @change="$emit('update:category', String($event || ''))">
      <t-option v-for="item in categories" :key="item" :value="item" :label="item" />
    </t-select>
  </div>
</template>
<script setup lang="ts">
defineProps<{ search: string; category: string; categories: string[] }>()
defineEmits<{ 'update:search': [value: string]; 'update:category': [value: string] }>()
</script>
<style scoped>.filters{display:grid;grid-template-columns:minmax(260px,1fr) 220px;gap:12px}@media(max-width:680px){.filters{grid-template-columns:1fr}}</style>
