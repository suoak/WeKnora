<script setup lang="ts">
withDefaults(defineProps<{ canEdit?: boolean }>(), { canEdit: false })
defineEmits<{ upload: []; url: []; manual: [] }>()
</script>
<template>
    <div class="empty">
        <img class="empty-img" src="@/assets/img/upload.svg" alt="">
        <span class="empty-eyebrow">{{ $t('knowledgeBase.emptyCreated') }}</span>
        <span class="empty-txt">{{ $t('knowledgeBase.emptyAddFirst') }}</span>
        <span class="empty-type-txt">{{ $t('knowledgeBase.emptyUploadHint') }}</span>
        <div v-if="canEdit" class="empty-actions">
            <t-button theme="primary" @click="$emit('upload')">
                <template #icon><t-icon name="upload" /></template>
                {{ $t('knowledgeBase.emptyUploadFile') }}
            </t-button>
            <div class="empty-secondary-actions">
                <t-button variant="text" theme="default" @click="$emit('url')">{{ $t('knowledgeBase.importURL') }}</t-button>
                <t-button variant="text" theme="default" @click="$emit('manual')">{{ $t('upload.onlineEdit') }}</t-button>
            </div>
        </div>
    </div>
</template>
<style scoped lang="less">
.empty {
    flex: 1;
    display: flex;
    flex-flow: column;
    justify-content: center;
    align-items: center;
}

.empty-txt {
    color: var(--td-text-color-placeholder);
    font-family: var(--app-font-family);
    font-size: 16px;
    font-weight: 600;
    line-height: 26px;
    margin: 6px 0 8px;
}

.empty-eyebrow {
    color: var(--td-success-color);
    font-size: 13px;
    font-weight: 600;
}

.empty-type-txt {
    color: var(--td-text-color-disabled);
    text-align: center;
    font-family: var(--app-font-family);
    font-size: 12px;
    font-weight: 400;
    width: 217px;
}

.empty-img {
    width: 162px;
    height: 162px;
}

.empty-actions {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    margin-top: 20px;
}

.empty-secondary-actions {
    display: flex;
    gap: 4px;
}
</style>
