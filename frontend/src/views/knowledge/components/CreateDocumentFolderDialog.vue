<template>
  <t-dialog attach="body" :visible="state.visible" :footer="false"
    width="420px" dialog-class-name="document-folder-create-dialog kb-compact-dialog" :close-btn="!state.pending"
    :close-on-overlay-click="false" :close-on-esc-keydown="!state.pending" destroy-on-close @close="controller.cancel">
    <template #header>
      <div class="folder-create-heading">
        <FolderAddIcon size="16px" class="folder-create-heading-icon" aria-hidden="true" />
        <span class="folder-create-title">{{ t('knowledgeBase.folderCreate.title') }}</span>
      </div>
    </template>
    <form class="folder-create-form" @submit.prevent="submit">
      <div class="folder-create-location">
        <span class="folder-create-location-label">{{ t('knowledgeBase.folderCreate.parent') }}</span>
        <div class="folder-create-path" role="group" :title="parentLabel" :aria-label="parentLabel">
          <template v-for="(segment, index) in parentSegments" :key="index">
            <ChevronRightIcon v-if="index" size="12px" class="folder-create-path-separator" aria-hidden="true" />
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
        <t-button type="button" variant="outline" size="small" :disabled="state.pending" @click="controller.cancel">{{ t('common.cancel') }}</t-button>
        <t-button type="submit" theme="primary" size="small" :loading="state.pending" :disabled="state.pending || !state.name.trim()">{{ t('knowledgeBase.folderCreate.submit') }}</t-button>
      </div>
    </form>
  </t-dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue';
import './knowledgeCompactDialog.css';
import { MessagePlugin } from 'tdesign-vue-next';
import { FolderAddIcon, ChevronRightIcon } from 'tdesign-icons-vue-next';
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
.document-folder-create-dialog .t-dialog__body { padding-top: 16px; }
</style>
<style scoped>
.folder-create-heading { display: flex; align-items: center; gap: var(--app-space-2); min-width: 0; padding-right: 28px; }
.folder-create-heading-icon { flex-shrink: 0; font-size: var(--app-text-xl); color: var(--td-text-color-secondary); }
.folder-create-title { font-size: var(--app-text-lg); font-weight: 600; line-height: 22px; letter-spacing: .2px; color: var(--td-text-color-primary); }
.folder-create-form { min-width: 0; }
.folder-create-location { display: flex; align-items: center; gap: var(--app-space-2); min-width: 0; margin-bottom: var(--app-space-5); font-size: var(--app-text-sm); line-height: 20px; color: var(--td-text-color-secondary); }
.folder-create-location-label { flex-shrink: 0; }
.folder-create-path { display: flex; align-items: center; gap: var(--app-space-1); flex: 1; min-width: 0; }
.folder-create-path-separator { flex-shrink: 0; color: var(--td-text-color-placeholder); }
.folder-create-path-segment { flex: 1 1 0; min-width: 0; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
.folder-create-path-segment.is-current { flex-grow: 2; }
.folder-create-path-segment.is-ellipsis { flex: 0 0 auto; color: var(--td-text-color-placeholder); }
.folder-create-label { display: block; margin-bottom: var(--app-space-2); color: var(--td-text-color-primary); font-size: var(--app-text-base); line-height: 20px; }
.folder-create-input { box-sizing: border-box; width: 100%; height: 32px; padding: 0 var(--app-space-3); border: 1px solid var(--td-component-border); border-radius: var(--app-radius-xs); outline: none; background: var(--td-bg-color-container); color: var(--td-text-color-primary); font: inherit; font-size: var(--app-text-base); transition: border-color .15s, box-shadow .15s; }
.folder-create-input::placeholder { color: var(--td-text-color-placeholder); }
.folder-create-input:hover:not(:disabled) { border-color: var(--td-brand-color); }
.folder-create-input:focus { border-color: var(--td-brand-color); box-shadow: 0 0 0 2px var(--td-brand-color-focus); }
.folder-create-input[aria-invalid="true"] { border-color: var(--td-error-color); }
.folder-create-input[aria-invalid="true"]:focus { box-shadow: 0 0 0 2px var(--td-error-color-focus); }
.folder-create-input:disabled { background: var(--td-bg-color-component-disabled); color: var(--td-text-color-disabled); cursor: not-allowed; }
.folder-create-error { margin: var(--app-space-2) 0 0; color: var(--td-error-color); font-size: var(--app-text-sm); }
.folder-create-actions { display: flex; justify-content: flex-end; gap: var(--app-space-2); margin-top: 14px; padding-top: 14px; border-top: 1px solid var(--td-component-stroke); }
</style>
