import { describe, expect, it, vi } from 'vitest'
import type { Dataset, RunResult, SupplyLensApi } from '../types/api'
import { defaultRunParams, prepareDemoStage } from './workflow'

const dataset: Dataset = { datasetId: 'demo-test', supplier: 'Test supplier', warehouse: 'Test warehouse', asOf: '2026-09-22', usable: true, productsFound: 0, matchedProducts: 0, reviewProducts: 0, warnings: [] }
const run: RunResult = { runId: 'demo-run', supplier: dataset.supplier, asOf: dataset.asOf, status: 'COMPLETED', summary: { buy: 0, noBuy: 0, review: 0, totalUnits: 0, estimatedCost: null }, items: [], warnings: [], exportReady: false }
const empty = { dataset: null, params: null, run: null }
function mockApi(): SupplyLensApi {
  return { capabilities: { demoDataset: true, nvidiaReview: true, openaiExplanation: false }, importFiles: vi.fn().mockResolvedValue(dataset), loadDemoDataset: vi.fn().mockResolvedValue(dataset), createRun: vi.fn().mockResolvedValue(run), patchItem: vi.fn(), approveExport: vi.fn() }
}

describe('Direct navigation in demo mode', () => {
  it('opens parameters without calculating or approving an order', async () => {
    const api = mockApi()
    const result = await prepareDemoStage(api, 'CONFIGURE', empty)
    expect(result.dataset).toBe(dataset)
    expect(result.params).toMatchObject({ leadTimeDays: 30, safetyDays: 14, useNvidiaReview: true, useOpenAIExplanation: false })
    expect(result.run).toBeNull()
    expect(api.createRun).not.toHaveBeenCalled()
    expect(api.approveExport).not.toHaveBeenCalled()
  })

  it('opens recommendations directly from a new session', async () => {
    const api = mockApi()
    const result = await prepareDemoStage(api, 'RESULTS', empty)
    expect(api.loadDemoDataset).toHaveBeenCalledTimes(1)
    expect(api.createRun).toHaveBeenCalledWith(result.params)
    expect(result.run).toBe(run)
    expect(api.approveExport).not.toHaveBeenCalled()
  })

  it('prepares the order preview without approving or exporting it', async () => {
    const api = mockApi()
    const result = await prepareDemoStage(api, 'APPROVED', empty)
    expect(result.run).toBe(run)
    expect(api.createRun).toHaveBeenCalledTimes(1)
    expect(api.approveExport).not.toHaveBeenCalled()
  })

  it('keeps existing recommendations and manual decisions when switching sections', async () => {
    const api = mockApi()
    const params = defaultRunParams(dataset, api.capabilities)
    const result = await prepareDemoStage(api, 'APPROVED', { dataset, params, run })
    expect(result.run).toBe(run)
    expect(result.params).toBe(params)
    expect(api.loadDemoDataset).not.toHaveBeenCalled()
    expect(api.createRun).not.toHaveBeenCalled()
    expect(api.approveExport).not.toHaveBeenCalled()
  })

  it('uses edited parameters when preparing the first run', async () => {
    const api = mockApi()
    const params = { ...defaultRunParams(dataset, api.capabilities), leadTimeDays: 47, safetyDays: 20, includeRetailStock: true }
    await prepareDemoStage(api, 'RESULTS', { dataset, params, run: null })
    expect(api.createRun).toHaveBeenCalledWith(params)
    expect(api.loadDemoDataset).not.toHaveBeenCalled()
  })

  it('does not bypass validation through navigation', async () => {
    const api = mockApi()
    const params = { ...defaultRunParams(dataset, api.capabilities), leadTimeDays: 0 }
    await expect(prepareDemoStage(api, 'RESULTS', { dataset, params, run: null })).rejects.toThrow('Проверьте параметры')
    await expect(prepareDemoStage(api, 'APPROVED', { dataset: { ...dataset, usable: false }, params, run: null })).rejects.toThrow('Набор данных требует проверки')
    expect(api.createRun).not.toHaveBeenCalled()
    expect(api.approveExport).not.toHaveBeenCalled()
  })
})
