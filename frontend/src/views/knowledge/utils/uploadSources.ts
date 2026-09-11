import { shouldRejectKnowledgeFileType } from '../../../utils/fileTypeVerification'
import { isFileSizeOverLimit, MAX_FILE_SIZE_BYTES } from '../../../utils/uploadLimits'

export const UPLOAD_VIDEO_EXTENSIONS = ['mp4', 'mov', 'avi', 'mkv', 'webm', 'wmv', 'flv']

export function getUploadFileExt(file: File): string {
  const dot = file.name.lastIndexOf('.')
  if (dot < 0) return ''
  return file.name.substring(dot + 1).toLowerCase()
}

export function getUploadFileKey(file: File): string {
  const path = (file as File & { webkitRelativePath?: string }).webkitRelativePath || ''
  return `${path || file.name}\0${file.size}`
}

export interface FilterUploadFilesOptions {
  supportedFileTypes?: Set<string> | string[]
  fromFolder?: boolean
  maxFileSizeBytes?: number
}

export type UploadFileRejectReason =
  | 'unsupported_type'
  | 'oversize'
  | 'hidden'
  | 'video_filtered'

export interface RejectedUploadFile {
  file: File
  reason: UploadFileRejectReason
}

export interface FilterUploadFilesResult {
  validFiles: File[]
  rejectedFiles: RejectedUploadFile[]
}

export interface UploadRejectionSummary {
  unsupportedTypeCount: number
  oversizeCount: number
  videoFilteredCount: number
  hiddenFileCount: number
}

export interface UploadRejectionNotice {
  key: 'knowledgeBase.filesSkippedNoEngine'
    | 'knowledgeBase.filesSkippedOversize'
    | 'knowledgeBase.filesSkippedMixed'
  params: Record<string, number>
}

export function summarizeUploadRejections(
  rejectedFiles: RejectedUploadFile[],
): UploadRejectionSummary {
  const summary: UploadRejectionSummary = {
    unsupportedTypeCount: 0,
    oversizeCount: 0,
    videoFilteredCount: 0,
    hiddenFileCount: 0,
  }
  for (const rejected of rejectedFiles) {
    switch (rejected.reason) {
      case 'unsupported_type': summary.unsupportedTypeCount++; break
      case 'oversize': summary.oversizeCount++; break
      case 'video_filtered': summary.videoFilteredCount++; break
      case 'hidden': summary.hiddenFileCount++; break
    }
  }
  return summary
}

export function getUploadRejectionNotice(
  summary: UploadRejectionSummary,
  maxFileSizeMB: number,
): UploadRejectionNotice | undefined {
  const { unsupportedTypeCount, oversizeCount } = summary
  if (oversizeCount > 0 && unsupportedTypeCount > 0) {
    return {
      key: 'knowledgeBase.filesSkippedMixed',
      params: {
        count: oversizeCount + unsupportedTypeCount,
        oversize: oversizeCount,
        unsupported: unsupportedTypeCount,
        size: maxFileSizeMB,
      },
    }
  }
  if (oversizeCount > 0) {
    return {
      key: 'knowledgeBase.filesSkippedOversize',
      params: { count: oversizeCount, size: maxFileSizeMB },
    }
  }
  if (unsupportedTypeCount > 0) {
    return {
      key: 'knowledgeBase.filesSkippedNoEngine',
      params: { count: unsupportedTypeCount },
    }
  }
  return undefined
}

export function filterUploadFiles(
  files: FileList | File[],
  options: FilterUploadFilesOptions = {},
): FilterUploadFilesResult {
  const list = Array.from(files)
  const dynamicTypesRaw = options.supportedFileTypes
    ? options.supportedFileTypes instanceof Set
      ? options.supportedFileTypes
      : new Set(options.supportedFileTypes)
    : undefined
  // An empty set means the parser-engine list hasn't loaded yet (race with the
  // async fetch on mount). Treat it as "unknown" and fall back to the default
  // whitelist instead of rejecting every file as unsupported.
  const dynamicTypes = dynamicTypesRaw && dynamicTypesRaw.size > 0 ? dynamicTypesRaw : undefined

  const validFiles: File[] = []
  const rejectedFiles: RejectedUploadFile[] = []
  const maxFileSizeBytes = options.maxFileSizeBytes ?? MAX_FILE_SIZE_BYTES

  for (const file of list) {
    if (options.fromFolder) {
      const relativePath = (file as File & { webkitRelativePath?: string }).webkitRelativePath || file.name
      if (relativePath.split('/').some(part => part.startsWith('.'))) {
        rejectedFiles.push({ file, reason: 'hidden' })
        continue
      }
    }

    const fileExt = getUploadFileExt(file)
    if (UPLOAD_VIDEO_EXTENSIONS.includes(fileExt)) {
      rejectedFiles.push({ file, reason: 'video_filtered' })
      continue
    }

    if (shouldRejectKnowledgeFileType(file.name, dynamicTypes)) {
      rejectedFiles.push({ file, reason: 'unsupported_type' })
      continue
    }

    if (isFileSizeOverLimit(file, maxFileSizeBytes)) {
      rejectedFiles.push({ file, reason: 'oversize' })
      continue
    }

    validFiles.push(file)
  }

  return { validFiles, rejectedFiles }
}
