import assert from 'node:assert/strict'
import test from 'node:test'

import { resolveMaxFileSizeMB } from '../../../utils/uploadLimits.ts'

import {
  filterUploadFiles,
  getUploadRejectionNotice,
  summarizeUploadRejections,
  type UploadFileRejectReason,
} from './uploadSources.ts'

const MiB = 1024 * 1024

function file(name: string, sizeMiB: number): File {
  return { name, size: sizeMiB * MiB } as File
}

function reasons(files: File[], limitMiB = 100): UploadFileRejectReason[] {
  return filterUploadFiles(files, { maxFileSizeBytes: limitMiB * MiB })
    .rejectedFiles.map(item => item.reason)
}

test('accepts DOCX files at and below the configured size limit', () => {
  const result = filterUploadFiles([
    file('under.docx', 49),
    file('boundary.docx', 50),
  ], { maxFileSizeBytes: 50 * MiB })

  assert.deepEqual(result.validFiles.map(item => item.name), ['under.docx', 'boundary.docx'])
  assert.equal(result.rejectedFiles.length, 0)
})

test('uses an exact MiB byte boundary', () => {
  const accepted = { name: 'boundary.docx', size: 104857600 } as File
  const oversized = { name: 'over.docx', size: 104857601 } as File
  const result = filterUploadFiles([accepted, oversized], {
    maxFileSizeBytes: 100 * MiB,
  })

  assert.deepEqual(result.validFiles.map(item => item.name), ['boundary.docx'])
  assert.deepEqual(result.rejectedFiles.map(item => item.reason), ['oversize'])
})

test('invalid runtime limits fall through to build-time config and then the default', () => {
  assert.equal(resolveMaxFileSizeMB('invalid', '80'), 80)
  assert.equal(resolveMaxFileSizeMB(0, -1), 100)
  assert.equal(resolveMaxFileSizeMB(-20, Number.NaN), 100)
})

test('accepts a 60 MiB DOCX with the 100 MiB deployment limit', () => {
  assert.deepEqual(reasons([file('manual.docx', 60)]), [])
})

test('classifies a 101 MiB DOCX as oversize', () => {
  assert.deepEqual(reasons([file('large.docx', 101)]), ['oversize'])
})

test('classifies an unsupported extension separately from oversize', () => {
  assert.deepEqual(reasons([file('payload.exe', 1)]), ['unsupported_type'])
})

test('an oversize-only rejection summary cannot be rendered as no-parser', () => {
  const result = filterUploadFiles([file('large.docx', 101)], {
    maxFileSizeBytes: 100 * MiB,
  })
  const summary = summarizeUploadRejections(result.rejectedFiles)

  assert.equal(summary.oversizeCount, 1)
  assert.equal(summary.unsupportedTypeCount, 0)
  assert.equal(
    getUploadRejectionNotice(summary, 100)?.key,
    'knowledgeBase.filesSkippedOversize',
  )
})

test('an unsupported file remains eligible for the no-parser message', () => {
  const result = filterUploadFiles([file('payload.exe', 1)], {
    maxFileSizeBytes: 100 * MiB,
  })
  const summary = summarizeUploadRejections(result.rejectedFiles)

  assert.equal(summary.oversizeCount, 0)
  assert.equal(summary.unsupportedTypeCount, 1)
  assert.equal(
    getUploadRejectionNotice(summary, 100)?.key,
    'knowledgeBase.filesSkippedNoEngine',
  )
})

test('mixed batches preserve accepted, oversize, and unsupported states', () => {
  const result = filterUploadFiles([
    file('normal.docx', 20),
    file('large.docx', 101),
    file('payload.exe', 1),
  ], { maxFileSizeBytes: 100 * MiB })
  const summary = summarizeUploadRejections(result.rejectedFiles)

  assert.deepEqual(result.validFiles.map(item => item.name), ['normal.docx'])
  assert.equal(summary.oversizeCount, 1)
  assert.equal(summary.unsupportedTypeCount, 1)
  assert.deepEqual(getUploadRejectionNotice(summary, 100), {
    key: 'knowledgeBase.filesSkippedMixed',
    params: { count: 2, oversize: 1, unsupported: 1, size: 100 },
  })
})

test('all 67 reported user-manual file sizes fit under 100 MiB', () => {
  const actualSizes = [
    41776285, 35663050, 49358407, 54747425, 59521303, 55197424, 31479835,
    28613831, 53421257, 59130883, 63568654, 41061388, 58700506, 30600375,
    39922099, 29469286, 46203400, 53431188, 53387539, 45546695, 50281495,
    53986533, 34644900, 48328555, 27622853, 47635331, 40307987, 41925566,
    49346324, 51398549, 44149270, 51286529, 26420395, 37246528, 53973567,
    44934687, 48089991, 52613330, 37433978, 30367690, 31472184, 18479649,
    22288273, 27354285, 33874259, 51592186, 42977011, 47421276, 55840940,
    34134002, 29527211, 30821275, 34800388, 42443960, 42283405, 44638052,
    40735336, 35772726, 7289394, 26349183, 27726296, 11262871, 11372704,
    13778846, 17685492, 20529627, 22879335,
  ]
  const files = actualSizes.map((size, index) => ({
    name: index === 58 ? `manual-${index}.pdf` : `manual-${index}.docx`,
    size,
  } as File))
  const result = filterUploadFiles(files, { maxFileSizeBytes: 100 * MiB })

  assert.equal(result.validFiles.length, 67)
  assert.equal(result.rejectedFiles.length, 0)
})
