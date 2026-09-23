import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { demoApi } from './demoApi'
import { getApi, parseDataset, parseRun } from './supplyLensApi'
import type { RunResult } from '../types/api'

describe('Demo workflow and response contracts', () => {
  let run: RunResult
  afterEach(() => vi.unstubAllGlobals())
  beforeEach(async () => {
    run = await demoApi.createRun({ datasetId: 'demo', supplier: 'SystemElectric', leadTimeDays: 30, safetyDays: 14, includeShowcase: false, includeTZStock: false, includeRetailStock: false, useNvidiaReview: true, useOpenAIExplanation: true })
  })
  it('only counts BUY items in the export summary and accepts the contract', () => {
    expect(parseRun(run).summary).toMatchObject({ buy: 8, review: 3, noBuy: 5, totalUnits: 1040 })
    expect(run.items).toHaveLength(16)
  })
  it('updates item, breakdown, KPI and chart after confirming an anomaly', async () => {
    const updated = await demoApi.patchItem(run.runId, '300200428_', { anomalyId: 'anomaly-demo-001', anomalyDecision: 'REGULAR' })
    const item = updated.items.find((entry) => entry.code1C === '300200428_')!
    expect(item.decision).toBe('BUY')
    expect(item.recommendedQuantity).toBe(220)
    expect(item.breakdown.recommendedOrder).toBe(220)
    expect(item.demand.find((point) => point.month === 'Май')?.cleanedDemand).toBe(421)
    expect(updated.summary).toMatchObject({ buy: 9, review: 2, totalUnits: 1260 })
  })
  it('retains unresolved review after a quantity edit and omits it from CSV', async () => {
    const updated = await demoApi.patchItem(run.runId, '300200428_', { approvedQuantity: 95, managerComment: 'Проверка' })
    expect(updated.items[0].decision).toBe('REVIEW')
    expect(updated.items[0].approvedQuantity).toBe(95)
    const csv = await (await demoApi.approveExport(run.runId)).blob.text()
    expect(csv).not.toContain('300200428_')
    expect(csv).toContain('ДЕМО — НЕ ДЛЯ ЗАКУПКИ')
    expect(csv).toContain('300200430_')
  })
  it('rejects incomplete and non-numeric API responses', () => {
    expect(() => parseRun({})).toThrow()
    expect(() => parseRun({ ...run, summary: { ...run.summary, totalUnits: '100' } })).toThrow()
    expect(() => parseDataset({})).toThrow()
  })
  it('uses the live endpoint and preserves all server values without forecasting', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(new Response(JSON.stringify(run), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchSpy)
    const params = { datasetId: 'real-id', supplier: 'IEK', leadTimeDays: 60, safetyDays: 7, includeShowcase: true, includeTZStock: false, includeRetailStock: true, useNvidiaReview: false, useOpenAIExplanation: false }
    const result = await getApi('api').createRun(params)
    expect(result).toEqual(run)
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/runs')
    expect(JSON.parse(fetchSpy.mock.calls[0][1].body)).toEqual(params)
  })
  it('propagates server errors and rejects a non-CSV export response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({ message: 'Набор повреждён' }), { status: 422 })).mockResolvedValueOnce(new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } })))
    await expect(getApi('api').patchItem('run-1', '030001_', { approvedQuantity: 10 })).rejects.toThrow('Набор повреждён')
    await expect(getApi('api').approveExport('run-1')).rejects.toThrow('Сервер не вернул CSV')
  })
})
