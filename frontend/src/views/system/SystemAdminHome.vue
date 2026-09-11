<template>
  <div class="system-admin-home">
    <header class="system-admin-home__header">
      <span class="system-admin-home__eyebrow">{{ t('settings.navGroups.systemAdministration') }}</span>
      <h2>{{ t('settings.navGroups.systemAdministration') }}</h2>
      <p>{{ t('system.globalSettings.description') }}</p>
    </header>

    <nav class="system-admin-home__tasks" :aria-label="t('settings.navGroups.systemAdministration')">
      <button v-for="task in tasks" :key="task.section" type="button" @click="emit('navigate', task.section)">
        <span class="system-admin-home__icon"><t-icon :name="task.icon" /></span>
        <span class="system-admin-home__copy">
          <strong>{{ t(task.title) }}</strong>
          <small>{{ t(task.description) }}</small>
        </span>
        <t-icon name="chevron-right" class="system-admin-home__arrow" />
      </button>
    </nav>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const emit = defineEmits<{ navigate: [section: string] }>()
const { t } = useI18n()

const tasks = [
  { section: 'usage-analytics', icon: 'chart-bar', title: 'usageAnalytics.title', description: 'usageAnalytics.governance.description' },
  { section: 'models', icon: 'control-platform', title: 'settings.modelManagement', description: 'modelSettings.description' },
  { section: 'runtime-queues', icon: 'queue', title: 'settings.taskQueue', description: 'system.globalSettings.runtime.description' },
  { section: 'system-audit-log', icon: 'history', title: 'system.globalSettings.audit.tabLabel', description: 'system.globalSettings.audit.description' },
  { section: 'platform-api-keys', icon: 'secured', title: 'platformApiKeys.title', description: 'platformApiKeys.description' },
  { section: 'system-global', icon: 'setting', title: 'settings.system', description: 'system.globalSettings.description' },
] as const
</script>

<style scoped lang="less">
.system-admin-home {
  max-width: 960px;
  margin: 0 auto;
  padding: 36px 40px 48px;
}

.system-admin-home__header {
  padding-bottom: 24px;
  border-bottom: 1px solid var(--td-component-stroke);
}

.system-admin-home__eyebrow {
  color: var(--td-brand-color);
  font-size: 12px;
  font-weight: 600;
  letter-spacing: .08em;
  text-transform: uppercase;
}

h2 {
  margin: 8px 0;
  color: var(--td-text-color-primary);
  font-size: 24px;
}

p {
  max-width: 680px;
  margin: 0;
  color: var(--td-text-color-secondary);
  line-height: 1.6;
}

.system-admin-home__tasks {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 28px;
  margin-top: 20px;
}

.system-admin-home__tasks button {
  display: grid;
  grid-template-columns: 38px minmax(0, 1fr) auto;
  gap: 12px;
  align-items: center;
  min-height: 82px;
  padding: 14px 4px;
  border: 0;
  border-bottom: 1px solid var(--td-component-stroke);
  background: transparent;
  color: inherit;
  text-align: left;
  cursor: pointer;
}

.system-admin-home__tasks button:hover,
.system-admin-home__tasks button:focus-visible {
  color: var(--td-brand-color);
}

.system-admin-home__icon {
  display: grid;
  place-items: center;
  width: 38px;
  height: 38px;
  border-radius: 8px;
  background: var(--td-brand-color-light);
  color: var(--td-brand-color);
  font-size: 18px;
}

.system-admin-home__copy {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 4px;
}

.system-admin-home__copy strong {
  color: var(--td-text-color-primary);
  font-size: 15px;
}

.system-admin-home__copy small {
  display: -webkit-box;
  overflow: hidden;
  color: var(--td-text-color-secondary);
  font-size: 12px;
  line-height: 1.45;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}

.system-admin-home__arrow {
  color: var(--td-text-color-placeholder);
}

@media (max-width: 760px) {
  .system-admin-home {
    padding: 24px 20px 36px;
  }

  .system-admin-home__tasks {
    grid-template-columns: 1fr;
  }
}
</style>
