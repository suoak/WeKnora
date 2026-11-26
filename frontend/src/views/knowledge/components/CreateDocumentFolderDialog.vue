<template>
  <t-dialog attach="body" :visible="state.visible" :header="t('knowledgeBase.folderCreate.title')" :footer="false"
    width="440px" dialog-class-name="document-folder-create-dialog" :close-btn="!state.pending"
    :close-on-overlay-click="false" :close-on-esc-keydown="!state.pending" destroy-on-close @close="controller.cancel">
    <form class="folder-create-form" @submit.prevent="submit">
      <div class="folder-create-location">
        <span class="folder-create-location-label">{{ t('knowledgeBase.folderCreate.parent') }}</span>
        <div class="folder-create-path" role="group" :title="parentLabel" :aria-label="parentLabel">
          <FolderOpenIcon class="folder-create-path-icon" aria-hidden="true" />
          <template v-for="(segment, index) in parentSegments" :key="index">
            <ChevronRightIcon v-if="index" class="folder-create-path-separator" aria-hidden="true" />
            <span class="folder-create-path-segment" :class="{ 'is-current': index === parentSegments.length - 1, 'is-ellipsis': segment === '…' }">{{ segment }}</span>
          </template>
        </div>
      </div>
      <label class="folder-create-label" for="document-folder-name">{{ t('knowledgeBase.folderCreate.name') }}</label>
      <input id="document-folder-name" ref="nameInput" v-model="state.name" class="folder-create-input"
        autocomplete="off" :placeholder="t('knowledgeBase.folderCreate.placeholder')" :disabled="state.pending" :aria-invalid="!!state.error"
        :aria-describedby="state.error ? 'document-folder-name-error' : undefined" @input="state.error = null" />
      <p v-if="state.error" id="document-folder-name-error" class="folder-create-error" role="alert">{{ errorMessage }}</p>
      <div class="folder-create-actions">
        <t-button type="button" variant="outline" :disabled="state.pending" @click="controller.cancel">{{ t('common.cancel') }}</t-button>
        <t-button type="submit" theme="primary" :loading="state.pending" :disabled="state.pending || !state.name.trim()">{{ t('knowledgeBase.folderCreate.submit') }}</t-button>
      </div>
    </form>
  </t-dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue';
import { MessagePlugin } from 'tdesign-vue-next';
import { FolderOpenIcon, ChevronRightIcon } from 'tdesign-icons-vue-next';
import { useI18n } from 'vue-i18n';
import { createKnowledgeFolder } from '@/api/knowledge-base';
import { createDocumentFolderController, createDocumentFolderState, type CreatedDocumentFolder } from '../folderCreateState';

const props = defineProps<{
  visible: boolean;
  kbId: string;
  kbName?: string;
  parentPath: string;
  canEdit: boolean;
  folderPaths: string[];
  afterCreate: (folder: CreatedDocumentFolder) => Promise<void> | void;
}>();
const emit = defineEmits<{
  (event: 'update:visible', visible: boolean): void;
  (event: 'busy-change', pending: boolean): void;
}>();
const { t } = useI18n();
const state = reactive(createDocumentFolderState());
const nameInput = ref<HTMLInputElement>();
const controller = createDocumentFolderController(state, {
  currentContext: () => ({ kbId: props.kbId, parentPath: props.parentPath, canEdit: props.canEdit }),
  existingPaths: () => props.folderPaths,
  create: createKnowledgeFolder,
  onCreated: folder => props.afterCreate(folder),
});
const allParentSegments = computed(() => [props.kbName || t('knowledgeBase.folderTree.rootRow'), ...state.parentPath.split('/').filter(Boolean)]);
const parentLabel = computed(() => allParentSegments.value.join(' / '));
const parentSegments = computed(() => {
  const segments = allParentSegments.value;
  return segments.length > 3 ? [segments[0], '…', ...segments.slice(-2)] : segments;
});
const errorMessage = computed(() => ({
  invalid: t('knowledgeBase.folderCreate.invalid'),
  tooLong: t('knowledgeBase.folderCreate.tooLong'),
  tooDeep: t('knowledgeBase.folderCreate.tooDeep'),
  duplicate: t('knowledgeBase.folderCreate.duplicate'),
  parentMissing: t('knowledgeBase.folderCreate.parentMissing'),
  forbidden: t('knowledgeBase.folderCreate.forbidden'),
  failed: t('knowledgeBase.folderCreate.failed'),
})[state.error || 'failed']);

watch(() => state.visible, async visible => {
  emit('update:visible', visible);
  if (visible) { await nextTick(); nameInput.value?.focus(); }
});
watch(() => state.pending, pending => emit('busy-change', pending));
watch(() => [props.kbId, props.parentPath, props.canEdit], controller.contextChanged, { flush: 'sync' });
watch(() => props.visible, visible => {
  if (visible) {
    if (!state.visible && !controller.open()) emit('update:visible', false);
  } else if (!state.pending) controller.cancel();
}, { immediate: true });

async function submit() {
  const result = await controller.submit();
  if (result === 'created') MessagePlugin.success(t('knowledgeBase.folderCreate.success'));
  else if (result === 'refreshFailed') MessagePlugin.warning(t('knowledgeBase.folderCreate.refreshFailed'));
}
</script>

<style>
.document-folder-create-dialog { max-width: calc(100vw - 32px); }
</style>
<style scoped>
.folder-create-form { min-width: 0; }
.folder-create-location { margin-bottom: 24px; padding: 12px 14px; border-radius: 6px; background: var(--td-bg-color-secondarycontainer, #f3f3f3); }
.folder-create-location-label { display: block; margin-bottom: 6px; color: var(--td-text-color-secondary); font-size: var(--app-text-sm, 12px); line-height: 20px; }
.folder-create-path { display: flex; align-items: center; gap: 4px; min-width: 0; color: var(--td-text-color-primary); font-size: var(--app-text-sm, 12px); line-height: 22px; }
.folder-create-path-icon { flex-shrink: 0; margin-right: 4px; font-size: 16px; color: var(--td-text-color-secondary); }
.folder-create-path-separator { flex-shrink: 0; color: var(--td-text-color-placeholder); }
.folder-create-path-segment { flex: 1 1 0; min-width: 0; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
.folder-create-path-segment.is-current { flex-grow: 2; font-weight: 500; }
.folder-create-path-segment.is-ellipsis { flex: 0 0 auto; color: var(--td-text-color-secondary); }
.folder-create-label { display: block; margin-bottom: 8px; color: var(--td-text-color-primary); font-size: 14px; line-height: 22px; }
.folder-create-input { box-sizing: border-box; width: 100%; height: 36px; padding: 0 12px; border: 1px solid var(--td-border-level-2-color, #dcdcdc); border-radius: 4px; outline: none; background: var(--td-bg-color-container); color: var(--td-text-color-primary); font: inherit; font-size: 14px; transition: border-color .15s, box-shadow .15s; }
.folder-create-input::placeholder { color: var(--td-text-color-placeholder); }
.folder-create-input:hover:not(:disabled) { border-color: var(--td-brand-color); }
.folder-create-input:focus { border-color: var(--td-brand-color); box-shadow: 0 0 0 2px var(--td-brand-color-focus, #d9f5e6); }
.folder-create-input[aria-invalid="true"] { border-color: var(--td-error-color); }
.folder-create-input[aria-invalid="true"]:focus { box-shadow: 0 0 0 2px var(--td-error-color-focus, #fdecee); }
.folder-create-input:disabled { background: var(--td-bg-color-component-disabled); color: var(--td-text-color-disabled); cursor: not-allowed; }
.folder-create-error { margin: 8px 0 0; color: var(--td-error-color); font-size: var(--app-text-sm, 12px); }
.folder-create-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 24px; }
</style>
