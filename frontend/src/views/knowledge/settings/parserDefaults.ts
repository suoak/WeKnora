export interface ParserEngineDefaultCandidate {
  name: string
  available: boolean
}

const EXCEL_FILE_TYPES = new Set(['xlsx', 'xls', 'xlsm'])

export function isExcelFileType(fileType: string): boolean {
  return EXCEL_FILE_TYPES.has(fileType.trim().toLowerCase().replace(/^\./, ''))
}

export function pickDefaultParserEngineName(
  engines: ParserEngineDefaultCandidate[],
  extensions: string[],
): string {
  const allExcel = extensions.length > 0 && extensions.every(isExcelFileType)
  if (allExcel && engines.some(engine => engine.name === 'builtin')) {
    return 'builtin'
  }

  const available = engines.filter(engine => engine.available)
  const simpleExts = new Set(['md', 'markdown', 'txt', 'csv', 'json'])
  const allSimple = extensions.length > 0 && extensions.every(ext => simpleExts.has(ext))
  if (!allSimple) {
    const anydoc = available.find(engine => engine.name === 'anydoc')
    if (anydoc) return anydoc.name
  }
  return available[0]?.name ?? ''
}

export function excelFirstRowAsHeaderValue(configured: boolean | undefined): boolean {
  return configured ?? true
}
