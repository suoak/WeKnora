declare global {
  interface Window {
    __RUNTIME_CONFIG__?: {
      MAX_FILE_SIZE_MB?: number
      MAX_SKILL_BUNDLE_SIZE_MB?: number
      DEFAULT_LOCALE?: string
    }
  }
}

export const DEFAULT_MAX_FILE_SIZE_MB = 100

export function positiveMegabytes(value: unknown, fallback: number): number {
  const n = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(n) && n > 0 ? n : fallback
}

export function resolveMaxFileSizeMB(
  runtimeValue: unknown,
  buildTimeValue: unknown,
  defaultValue = DEFAULT_MAX_FILE_SIZE_MB,
): number {
  const buildTimeOrDefault = positiveMegabytes(buildTimeValue, defaultValue)
  return positiveMegabytes(runtimeValue, buildTimeOrDefault)
}

const runtimeMaxFileSize = typeof window !== 'undefined'
  ? window.__RUNTIME_CONFIG__?.MAX_FILE_SIZE_MB
  : undefined
const buildTimeMaxFileSize = import.meta.env?.VITE_MAX_FILE_SIZE_MB

// Docker writes MAX_FILE_SIZE_MB into window.__RUNTIME_CONFIG__ at container
// startup. VITE_MAX_FILE_SIZE_MB remains a build-time fallback for non-Docker
// deployments; the final fallback is the product default.
export const MAX_FILE_SIZE_MB = resolveMaxFileSizeMB(runtimeMaxFileSize, buildTimeMaxFileSize)
export const MAX_FILE_SIZE_BYTES = MAX_FILE_SIZE_MB * 1024 * 1024

export function isFileSizeOverLimit(
  file: Pick<File, 'size'>,
  maxFileSizeBytes = MAX_FILE_SIZE_BYTES,
): boolean {
  return file.size > maxFileSizeBytes
}
