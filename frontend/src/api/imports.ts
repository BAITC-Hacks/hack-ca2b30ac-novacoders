import { requestJSON, upload } from './client'
import type { FileKind, ImportFiles, ImportResult, Supplier } from './types'

export const fileKinds: FileKind[] = ['moqFile', 'monthlySalesFile', 'detailedSalesFile', 'monthlyStockFile', 'seasonalityFile', 'inventoryTransitFile']
export const maxFileBytes = 20 * 1024 * 1024
export async function importLocal(supplier: Supplier): Promise<ImportResult> {
  return parseImport(await requestJSON('/api/v1/imports/local', { method: 'POST', body: JSON.stringify({ supplier }) }))
}
export function parseImport(value: unknown): ImportResult {
  const result = value as ImportResult | null
  if (!result || typeof result.importId !== 'string' || !result.importId || result.status !== 'READY' || !['SystemElectric', 'IEK'].includes(result.supplier) || !Number.isFinite(Date.parse(result.asOf)) || !result.summary || !Array.isArray(result.files) || result.files.length !== 6 || !Array.isArray(result.warnings)) throw new Error('Некорректный результат проверки файлов от backend.')
  if (![result.summary.productsFound, result.summary.productsReady, result.summary.productsWithWarnings].every((n) => Number.isInteger(n) && n >= 0)) throw new Error('Некорректная сводка импорта.')
  return result
}
export async function importFiles(files: ImportFiles, supplier: Supplier, onProgress: (value: number | null) => void): Promise<ImportResult> {
  const form = new FormData()
  for (const key of fileKinds) {
    const file = files[key]
    if (!file) throw new Error('Выберите все шесть обязательных файлов.')
    if (!/\.xlsx$/i.test(file.name) || file.size === 0 || file.size > maxFileBytes) throw new Error(`${file.name}: нужен непустой .xlsx размером до 20 MiB.`)
    form.append(key, file)
  }
  form.append('supplier', supplier)
  return parseImport(await upload('/api/v1/imports', form, onProgress))
}
