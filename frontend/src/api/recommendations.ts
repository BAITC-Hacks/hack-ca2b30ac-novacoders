import { request, requestJSON } from './client'
import type { ExportResult, ItemPatch, ItemPatchResult, RunParams, RunResult } from './types'

const numeric = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value)
export function parseRun(value: unknown): RunResult {
  const run = value as RunResult | null
  if (!run || !run.runId || !run.importId || !['COMPLETED', 'COMPLETED_WITH_WARNINGS'].includes(run.status) || !Number.isFinite(Date.parse(run.asOf)) || !Array.isArray(run.items) || !run.summary || !run.orderSummary || !Array.isArray(run.warnings)) throw new Error('Формат рекомендаций backend не соответствует контракту.')
  if (![run.summary.buy, run.summary.noBuy, run.summary.review, run.summary.totalUnits, run.summary.estimatedCost, run.summary.unpricedItems, run.summary.approvedItems, run.orderSummary.positions, run.orderSummary.totalUnits].every(numeric) || (run.orderSummary.estimatedCost !== null && !numeric(run.orderSummary.estimatedCost))) throw new Error('Backend вернул некорректные итоговые показатели.')
  const seen = new Set<string>()
  for (const item of run.items) {
    if (typeof item.code1C !== 'string' || !item.code1C || seen.has(item.code1C) || !['BUY', 'NO_BUY', 'REVIEW'].includes(item.decision) || !['HIGH', 'MEDIUM', 'LOW', 'NONE'].includes(item.urgency) || !numeric(item.recommendedQuantity) || !numeric(item.finalQuantity) || typeof item.approved !== 'boolean' || (item.approvedQuantity !== null && !numeric(item.approvedQuantity)) || (item.estimatedCost !== null && !numeric(item.estimatedCost)) || !item.calculation || !Array.isArray(item.warnings)) throw new Error('Backend вернул некорректную позицию заказа.')
    if (item.approved && item.approvedQuantity === null) throw new Error('В подтверждённой позиции отсутствует количество.')
    seen.add(item.code1C)
  }
  return run
}
export async function createRecommendations(importId: string, params: RunParams): Promise<RunResult> {
  return parseRun(await requestJSON(`/api/v1/imports/${encodeURIComponent(importId)}/recommendations`, { method: 'POST', body: JSON.stringify(params) }))
}
export async function getRecommendations(runId: string): Promise<RunResult> {
  return parseRun(await requestJSON(`/api/v1/recommendations/${encodeURIComponent(runId)}`))
}
export async function patchItem(runId: string, code1C: string, patch: ItemPatch): Promise<ItemPatchResult> {
  const result = await requestJSON(`/api/v1/recommendations/${encodeURIComponent(runId)}/items/${encodeURIComponent(code1C)}`, { method: 'PATCH', body: JSON.stringify(patch) }) as ItemPatchResult | null
  if (!result || result.code1C !== code1C || typeof result.approved !== 'boolean' || (result.approvedQuantity !== null && !numeric(result.approvedQuantity)) || !Number.isFinite(Date.parse(result.updatedAt))) throw new Error('Некорректное подтверждение сохранения. Обновите расчёт перед экспортом.')
  return result
}
export async function exportOrder(runId: string): Promise<ExportResult> {
  const response = await request(`/api/v1/recommendations/${encodeURIComponent(runId)}/export`, { method: 'POST', headers: { Accept: 'text/csv' } })
  if (!response.headers.get('Content-Type')?.toLowerCase().includes('text/csv')) throw new Error('Сервер не вернул CSV. Заказ не отмечен как экспортированный.')
  const disposition = response.headers.get('Content-Disposition') || ''
  const match = disposition.match(/filename\*?=(?:UTF-8''|"?)([^";]+)/i)
  let filename = 'supplier-order.csv'
  if (match) { try { filename = decodeURIComponent(match[1]).replace(/[\\/:*?"<>|]/g, '_').split('').map((c) => c.charCodeAt(0) < 32 ? '_' : c).join('') } catch { /* Keep safe fallback filename. */ } }
  return { blob: await response.blob(), filename }
}
export function downloadExport(result: ExportResult): void {
  const url = URL.createObjectURL(result.blob)
  const link = document.createElement('a')
  try {
    link.href = url; link.download = result.filename
    document.body.append(link); link.click()
  } finally {
    link.remove()
    // Defer cleanup until the browser has handled the download click.
    setTimeout(() => URL.revokeObjectURL(url), 0)
  }
}
