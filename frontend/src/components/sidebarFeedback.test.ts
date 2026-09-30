import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = (relative: string) => readFileSync(new URL(relative, import.meta.url), 'utf8')

test('feedback uses one pinned secure external link without changing the SPA route', () => {
  const navigation = source('../config/navigation.ts')
  const sidebar = source('./SidebarNavigation.vue')

  assert.match(navigation, /export const FEEDBACK_URL = 'https:\/\/ruijie\.feishu\.cn\/wiki\/GC4AwjoNUicPyEkyv9fcrC3Wnlb'/)
  assert.match(navigation, /id: 'feedback'[^\n]*externalUrl: FEEDBACK_URL[^\n]*group: 'workspace'/)
  assert.ok(navigation.indexOf("id: 'members'") < navigation.indexOf("id: 'feedback'"))
  assert.ok(navigation.indexOf("id: 'feedback'") < navigation.indexOf("id: 'management-center'"))

  assert.match(sidebar, /<a v-if="entry\.externalUrl" :href="entry\.externalUrl" target="_blank" rel="noopener noreferrer"/)
  assert.match(sidebar, /:content="t\(entry\.labelKey\)" placement="right" :disabled="!collapsed"/)
  assert.match(sidebar, /<span v-if="!collapsed">\{\{ t\(entry\.labelKey\) \}\}<\/span>/)
  assert.doesNotMatch(sidebar, /window\.open/)
  assert.doesNotMatch(sidebar, /router\.push\(entry\.externalUrl\)/)
})

test('feedback label is localized in Chinese and English', () => {
  assert.match(source('../i18n/locales/zh-CN.ts'), /feedback: '问题反馈'/)
  assert.match(source('../i18n/locales/en-US.ts'), /feedback: 'Feedback'/)
})
