import type { Capabilities, Dataset, RunParams, RunResult, Stage, SupplyLensApi } from '../types/api'

export function defaultRunParams(dataset: Dataset, capabilities: Capabilities): RunParams {
  const caps = dataset.capabilities || capabilities
  return { datasetId: dataset.datasetId, supplier: dataset.supplier, leadTimeDays: 30, safetyDays: 14, includeShowcase: false, includeTZStock: false, includeRetailStock: false, useNvidiaReview: caps.nvidiaReview, useOpenAIExplanation: caps.openaiExplanation }
}

export function validRunParams(params: RunParams): boolean {
  return Number.isInteger(params.leadTimeDays) && params.leadTimeDays >= 1 && params.leadTimeDays <= 365 && Number.isInteger(params.safetyDays) && params.safetyDays >= 0 && params.safetyDays <= 365
}

interface PreparedStage { dataset: Dataset; params: RunParams; run: RunResult | null }
interface CurrentData { dataset: Dataset | null; params: RunParams | null; run: RunResult | null }

// Only the explicitly selected demo mode uses this shortcut. Navigation never approves an order.
export async function prepareDemoStage(api: SupplyLensApi, stage: Exclude<Stage, 'IMPORT'>, current: CurrentData): Promise<PreparedStage> {
  const dataset = current.dataset || await api.loadDemoDataset()
  const params = current.params || defaultRunParams(dataset, api.capabilities)
  if (stage === 'CONFIGURE' || current.run) return { dataset, params, run: current.run }
  if (!dataset.usable) throw new Error('Набор данных требует проверки. Вернитесь к импорту.')
  if (!validRunParams(params)) throw new Error('Проверьте параметры: срок поставки — 1–365 дней, страховой запас — 0–365 дней. Только целые числа.')
  return { dataset, params, run: await api.createRun(params) }
}
