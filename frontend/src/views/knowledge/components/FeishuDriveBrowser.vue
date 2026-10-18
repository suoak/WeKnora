<template>
  <section class="drive-browser" :aria-busy="busy">
    <t-button variant="outline" :disabled="disabled || busy" @click="openRoot">{{ t('browse') }}</t-button>
    <template v-if="opened">
      <p class="drive-scope">{{ t('driveScope') }}</p>
      <nav class="drive-nav" :aria-label="t('root')">
        <t-button variant="text" :disabled="disabled || busy || parents.length === 0" @click="back">{{ t('parent') }}</t-button>
        <span>{{ folderName }}</span>
      </nav>
      <p v-if="busy" role="status">{{ t('browse') }}…</p>
      <p v-else-if="!error && items.length === 0">{{ t('empty') }}</p>
      <ul class="drive-items">
        <li v-for="item in items" :key="`${item.type}:${item.token}`">
          <span class="drive-name">{{ item.name }}</span>
          <t-button v-if="item.type === 'folder'" variant="text" :disabled="disabled || busy" @click="enter(item)">{{ t('openFolder') }}</t-button>
          <t-button v-else-if="item.importable" variant="text" :disabled="disabled || busy" @click="emit('select', item.url)">{{ t('select') }}</t-button>
          <span v-else class="drive-scope">{{ t('unsupported') }}</span>
        </li>
      </ul>
      <t-button v-if="nextPage && !error" variant="outline" :disabled="disabled || busy" @click="load(folder, folderName, nextPage)">{{ t('more') }}</t-button>
    </template>
    <p v-if="error" role="alert">{{ t('failed') }}</p>
  </section>
</template>

<script setup lang="ts">
import { ref, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { browseFeishuDrive, type FeishuDriveItem } from '@/api/feishuPersonal'
import { feishuMessages } from '@/i18n/feishuPersonal'

defineProps<{ disabled?: boolean }>()
const emit = defineEmits<{ select: [url: string] }>()
const { t } = useI18n({ useScope: 'local', messages: feishuMessages })
const opened = ref(false), busy = ref(false), error = ref(false)
const folder = ref(''), folderName = ref(t('root')), nextPage = ref('')
const items = ref<FeishuDriveItem[]>([])
const parents = ref<Array<{ token: string; name: string }>>([])
let generation = 0
onBeforeUnmount(() => { generation++ })

async function load(token: string, name: string, cursor = '') {
  const current = ++generation
  busy.value = true; error.value = false; items.value = []; nextPage.value = ''
  folder.value = token; folderName.value = name; opened.value = true
  try {
    const page = await browseFeishuDrive(token, cursor)
    if (current !== generation) return
    items.value = page.items
    nextPage.value = page.has_more ? page.next_page_token : ''
  } catch {
    if (current === generation) error.value = true
  } finally {
    if (current === generation) busy.value = false
  }
}
function openRoot() { parents.value = []; void load('', t('root')) }
function enter(item: FeishuDriveItem) {
  parents.value.push({ token: folder.value, name: folderName.value })
  void load(item.token, item.name)
}
function back() {
  const parent = parents.value.pop()
  if (parent) void load(parent.token, parent.name)
}
</script>

<style scoped>
.drive-browser { margin-block: 16px; }
.drive-scope { color: var(--td-text-color-secondary); font-size: 12px; }
.drive-nav { display: flex; align-items: center; gap: 8px; overflow-wrap: anywhere; }
.drive-items { list-style: none; padding: 0; max-height: 280px; overflow-y: auto; }
.drive-items li { display: flex; align-items: center; gap: 8px; padding-block: 4px; }
.drive-name { flex: 1; min-width: 0; overflow-wrap: anywhere; }
</style>
