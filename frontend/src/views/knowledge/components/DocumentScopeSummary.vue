<template>
  <span>{{ filtered ? t('knowledgeBase.folderTree.filteredScopeCount', { count }) : t('knowledgeBase.folderTree.currentFolderCount', { count }) }}<template v-if="wholeCount != null"> · {{ t('knowledgeBase.folderTree.wholeKnowledgeBaseCount', { count: wholeCount }) }}</template></span>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
defineProps<{ count: number; wholeCount?: number; filtered: boolean }>()
const { t } = useI18n()
</script>
