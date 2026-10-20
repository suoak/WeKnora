import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createI18n } from 'vue-i18n'
import embed from '../../src/i18n/embed'
import { LOCALE_BUNDLES, getLocaleValueAtPath } from '../../src/i18n/localeKeyAudit'
import { localTranslationScopes, missingKeys, scanExpression, scanSources } from './i18nStaticAudit'
const baseline = JSON.parse(readFileSync(new URL('./i18nHistoricalBaseline.json', import.meta.url), 'utf8'))
const repaired: Record<string,string[]> = JSON.parse(readFileSync(new URL('./rc24-translations.json', import.meta.url), 'utf8'))

test('local Feishu messages and aliases are checked without accepting missing or global-only keys', () => {
  const scopes = localTranslationScopes(`
    import { feishuMessages as messages } from '@/i18n/feishuPersonal'
    const { t: ft } = useI18n({ useScope: 'local', messages: messages })
  `)
  const calls = scanExpression(`ft('sharingShort'); ft('new.missing'); ft('knowledgeBase.documentCount')`, 'sample.vue', 0, scopes)
  assert.equal(calls.length, 3)
  assert.deepEqual(missingKeys(calls).map(x => x.key), ['new.missing', 'knowledgeBase.documentCount'])
  const unresolved = localTranslationScopes(`const { t } = useI18n({ useScope: 'local', messages: unknown })`)
  assert.equal(missingKeys(scanExpression(`t('title')`, 'sample.vue', 0, unresolved)).length, 1)
})

test('AST skips comments and handles literal conditional branches and template literals', () => {
  assert.deepEqual(scanExpression(`// t('comment.fake')\n t(ready ? 'a.yes' : 'a.no'); $t(\`a.template\`); t('prefix.' + dynamic);`, 'sample.ts').map(c=>c.key), ['a.yes','a.no','a.template'])
  assert.equal(missingKeys(scanExpression(`t('new.missing');`, 'views/knowledge/New.vue'))[0].key, 'new.missing')
})
test('static all-language gaps equal reviewed baseline; core gaps and new gaps fail', () => {
  assert.deepEqual(baseline, []) // All 28 reachable candidates repaired; no historical exceptions.
  const normalize = (entries:any[]) => entries.map(x => ({key:x.key,calls:x.calls.map((c:any)=>`${c.file}:${c.line}`).sort()})).sort((a,b)=>a.key.localeCompare(b.key))
  assert.deepEqual(normalize(missingKeys()), normalize(baseline))
})
test('indirect menu/status metadata and prefixed sort configuration are audited', () => {
  assert.deepEqual(scanExpression(`const menu={title:'new.menu',descriptionKey:'new.hint'};`, 'config/menu.ts').map(c=>c.key),['new.menu','new.hint'])
  assert.equal(scanExpression(`const option={labelKey:'recentlyUpdated'};`, 'utils/resourceSorting.ts')[0].key,'resourceSort.recentlyUpdated')
  assert.equal(missingKeys(scanExpression(`const status={labelKey:'new.missingStatus'};`, 'config/status.ts'))[0].key,'new.missingStatus')
})
test('repaired messages resolve in all five locales with correct interpolations', () => {
  const calls = scanSources()
  for(const [locale,bundle] of Object.entries(LOCALE_BUNDLES)) {
    const i18n = createI18n({legacy:false,locale,fallbackLocale:false,messages:{[locale]:bundle},missingWarn:false,fallbackWarn:false})
    for(const key of [...Object.keys(repaired),'knowledgeBase.documentCount','knowledgeBase.filters','knowledgeBase.clearFilters','embedPublish.nameDefaultHint']) {
      const value = getLocaleValueAtPath(bundle,key)
      assert.equal(typeof value,'string',`${locale}:${key}`)
      const vars = [...String(value).matchAll(/\{(\w+)\}/g)].map(m=>m[1])
      for(const call of calls.filter(c=>c.key===key)) for(const name of vars) assert.ok(call.params.includes(name),`${call.file}:${call.line}:${key} missing ${name}`)
      const params = Object.fromEntries(vars.map(name=>[name,name==='count'?7:'RC24_TEST']))
      const translated = i18n.global.t(key, params)
      assert.notEqual(translated,key,`${locale}:${key}`)
      assert.ok(!/\{\w+\}/.test(translated),`${locale}:${key}`)
    }
  }
})
test('eight embed-only historical findings exist in the separate runtime bundle', () => {
  const calls=scanSources().filter(c=>c.file==='components/EmbedInputField.vue'||c.file==='composables/useEmbedChatSession.ts')
  const keys=[...new Set(calls.map(c=>c.key))]
  for(const locale of Object.keys(LOCALE_BUNDLES)) for(const key of keys) assert.ok(embed.global.te(key,locale),`${locale}: embed:${key}`)
})
