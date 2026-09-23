import { afterEach, describe, expect, it, vi } from 'vitest'
import { responseError } from './client'
import { fileKinds, importFiles, importLocal, parseImport } from './imports'
import { createRecommendations, downloadExport, exportOrder, getRecommendations, parseRun, patchItem } from './recommendations'
import type { ImportFiles, ImportResult, RunResult } from './types'
import { defaultRunParams } from '../lib/workflow'

const imported: ImportResult = {
  importId: 'import-001', status: 'READY', supplier: 'IEK', asOf: '2026-09-22',
  files: fileKinds.map((type) => ({ type, fileName: `${type}.xlsx`, status: 'VALID', rows: 1 })),
  summary: { productsFound: 1, productsReady: 0, productsWithWarnings: 1 }, warnings: [],
}
const run: RunResult = {
  runId: 'run-1', importId: imported.importId, supplier: 'IEK', asOf: '2026-09-22', createdAt: '2026-09-23T10:00:00Z', status: 'COMPLETED_WITH_WARNINGS', settings: defaultRunParams(),
  summary: { buy: 0, noBuy: 0, review: 1, totalUnits: 25, estimatedCost: 0, unpricedItems: 1, approvedItems: 0 }, orderSummary: { positions: 0, totalUnits: 0, estimatedCost: 0 },
  items: [{ code1C: '003_', name: 'Товар', article: 'A', supplier: 'IEK', category: '', decision: 'REVIEW', urgency: 'NONE', recommendedQuantity: 25, approvedQuantity: null, approved: false, finalQuantity: 25, updatedAt: '2026-09-23T10:00:00Z', estimatedCost: null, unitCost: null, calculation: { freeStock: null, inTransit: 0, roundedRequirement: 40 }, anomalyDecision: 'PENDING_REVIEW', warnings: [], managerComment: '', requiresManualReview: true }],
  warnings: [{ code: 'OPENAI_UNAVAILABLE', severity: 'WARNING', message: 'Использовано шаблонное объяснение' }], providers: { openai: { status: 'FALLBACK' } },
}
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); vi.useRealTimers() })

describe('Shared frontend/backend contract', () => {
  it('imports the selected server dataset through Go without a browser file or mock fallback', async () => {
    const fetch = vi.fn().mockResolvedValueOnce(json(imported, 201)).mockResolvedValueOnce(json({ error: { message: 'Нет папки system_electric' } }, 422))
    vi.stubGlobal('fetch', fetch)
    expect(await importLocal('IEK')).toEqual(imported)
    expect(fetch.mock.calls[0][0]).toBe('http://127.0.0.1:8080/api/v1/imports/local')
    expect(fetch.mock.calls[0][1].method).toBe('POST')
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual({ supplier: 'IEK' })
    await expect(importLocal('SystemElectric')).rejects.toThrow('Нет папки')
  })
  it('uploads exactly six named files and the supplier, with a browser boundary', async () => {
    let sent: FormData | undefined
    let url = ''
    const progress = vi.fn()
    class XHR {
      upload = { onprogress: (event: unknown) => { void event } }
      timeout = 0; status = 201; responseText = JSON.stringify(imported)
      onload = () => {}
      open(method: string, path: string) { expect(method).toBe('POST'); url = path }
      send(body: FormData) { sent = body; this.upload.onprogress({ lengthComputable: true, loaded: 1, total: 1 }); this.onload() }
    }
    vi.stubGlobal('XMLHttpRequest', XHR)
    const files = Object.fromEntries(fileKinds.map((key) => [key, new File(['xlsx bytes'], `${key}.xlsx`)])) as ImportFiles
    expect(await importFiles(files, 'IEK', progress)).toEqual(imported)
    expect(url).toBe('http://127.0.0.1:8080/api/v1/imports')
    expect([...sent!.keys()].sort()).toEqual([...fileKinds, 'supplier'].sort())
    expect(sent!.get('supplier')).toBe('IEK'); expect(progress).toHaveBeenCalledWith(100)
    await expect(importFiles({ ...files, moqFile: null }, 'IEK', progress)).rejects.toThrow('шесть')
    await expect(importFiles({ ...files, moqFile: new File(['old'], 'old.xls') }, 'IEK', progress)).rejects.toThrow('.xlsx')
  })
  it('passes calculation settings and leaves all AI numbers and nulls unchanged', async () => {
    const fetch = vi.fn().mockResolvedValue(json(run))
    vi.stubGlobal('fetch', fetch)
    const params = { ...defaultRunParams(), safetyStockDays: 0, excludePartialMonth: false }
    expect(await createRecommendations('import-001', params)).toEqual(run)
    expect(fetch.mock.calls[0][0]).toBe('http://127.0.0.1:8080/api/v1/imports/import-001/recommendations')
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual(params)
    expect(parseRun(run).items[0].calculation).toEqual({ freeStock: null, inTransit: 0, roundedRequirement: 40 })
  })
  it('sends explicit approval and preserves the exact code, including underscore', async () => {
    const result = { code1C: '003_', approvedQuantity: 0, approved: true, updatedAt: '2026-09-23T11:00:00Z' }
    const fetch = vi.fn().mockResolvedValueOnce(json(result)).mockResolvedValueOnce(json(run))
    vi.stubGlobal('fetch', fetch)
    const patch = { approvedQuantity: 0, approved: true, comment: 'Отказ от заказа' }
    expect(await patchItem('run-1', '003_', patch)).toEqual(result)
    expect(fetch.mock.calls[0][0]).toBe('http://127.0.0.1:8080/api/v1/recommendations/run-1/items/003_')
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual(patch)
    expect(await getRecommendations('run-1')).toEqual(run)
  })
  it('reads CSV as a Blob and handles its filename', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('code1C,quantity\n003_,25\n', { headers: { 'Content-Type': 'text/csv; charset=utf-8', 'Content-Disposition': 'attachment; filename="supplier-order.csv"' } })))
    const result = await exportOrder('run-1')
    expect(result.filename).toBe('supplier-order.csv'); expect(await result.blob.text()).toContain('003_,25')
  })
  it('cleans up the download object URL', () => {
    vi.useFakeTimers()
    const link = { href: '', download: '', click: vi.fn(), remove: vi.fn() }
    vi.stubGlobal('document', { createElement: () => link, body: { append: vi.fn() } })
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:test')
    const revoke = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    downloadExport({ blob: new Blob(['csv']), filename: 'supplier-order.csv' })
    expect(link.click).toHaveBeenCalledOnce(); expect(link.remove).toHaveBeenCalledOnce()
    vi.runAllTimers(); expect(revoke).toHaveBeenCalledWith('blob:test')
  })
  it('uses nested backend errors and field diagnostics', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json({ error: { code: 'AI_VALIDATION_ERROR', message: 'Невозможно выполнить расчёт', details: { fields: [{ field: 'products[0].code1C', message: 'Поле обязательно' }] } } }, 422)))
    await expect(createRecommendations('import-001', defaultRunParams())).rejects.toThrow('products[0].code1C: Поле обязательно')
    expect(responseError(503, null).message).toContain('недоступен')
    expect(responseError(422, { error: { message: 'Ошибка файла', details: { errors: [{ file: 'moq', message: 'Нет заголовков' }] } } }).message).toContain('moq: Нет заголовков')
  })
  it('does not substitute fixtures on network, timeout or content-type failures', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValueOnce(new TypeError('network')).mockRejectedValueOnce(new DOMException('timed out', 'TimeoutError')).mockResolvedValueOnce(json({})))
    await expect(getRecommendations('run-1')).rejects.toThrow('Не удалось связаться')
    await expect(getRecommendations('run-1')).rejects.toThrow('вовремя')
    await expect(exportOrder('run-1')).rejects.toThrow('не вернул CSV')
  })
  it('rejects incompatible or incomplete responses without silently coercing numbers', () => {
    expect(() => parseImport({})).toThrow()
    expect(() => parseRun({ ...run, summary: { ...run.summary, totalUnits: '25' } })).toThrow()
    expect(() => parseRun({ ...run, items: [{ ...run.items[0], code1C: 3 }] })).toThrow()
    expect(() => parseRun({ ...run, items: [{ ...run.items[0], approved: true, approvedQuantity: null }] })).toThrow()
  })
})
