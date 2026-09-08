<template>
  <div class="filters">
    <t-input :value="search" clearable :placeholder="t('portal.searchPlaceholder')"
      @update:value="$emit('update:search', String($event || ''))">
      <template #prefix-icon><t-icon name="search" /></template>
    </t-input>
    <t-select v-if="showCategory" :value="category" clearable :placeholder="t('portal.allCategories')" @change="$emit('update:category', String($event || ''))">
      <t-option v-for="item in categories" :key="item" :value="item" :label="categoryLabel(item)" />
    </t-select>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
withDefaults(defineProps<{ search: string; category: string; categories: string[]; showCategory?: boolean }>(), { showCategory: true })
defineEmits<{ 'update:search': [value: string]; 'update:category': [value: string] }>()
const { t, te } = useI18n()
const categoryLabel = (value: string) => te(`portal.categories.${value}`) ? t(`portal.categories.${value}`) : value
</script>
<style scoped>.filters{display:grid;grid-template-columns:minmax(260px,1fr) 220px;gap:12px}@media(max-width:680px){.filters{grid-template-columns:1fr}}</style>
