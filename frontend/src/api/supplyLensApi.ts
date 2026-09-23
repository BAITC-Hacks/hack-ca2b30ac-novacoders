import type { Dataset, ExportResult, ImportFiles, RunResult, SupplyLensApi } from '../types/api'
import { demoApi } from './demoApi'

const baseUrl = (import.meta.env.VITE_API_BASE_URL || '').replace(/\/$/, '')
const endpoint = (path: string) => `${baseUrl}${path}`
const fallbackError = 'Не удалось связаться с сервером. Проверьте подключение к Go API и повторите запрос.'
function responseError(status: number, body: unknown): Error {
  if (body && typeof body === 'object' && 'message' in body && typeof body.message === 'string') return new Error(body.message)
  const messages: Record<number, string> = {
    413: 'Файлы слишком большие для сервера. Уменьшите размер и повторите загрузку.',
    422: 'Сервер не смог обработать данные. Проверьте файлы и параметры.',
    409: 'Расчёт изменился на сервере. Обновите его перед подтверждением.',
    404: 'Метод API не найден. Проверьте адрес и контракт Go-сервера.',
  }
  return new Error(messages[status] || `Сервер вернул ошибку ${status}. Повторите запрос.`)
}
async function request(path: string, options?: RequestInit): Promise<Response> {
  let response: Response
  try {
    response = await fetch(endpoint(path), { ...options, signal: AbortSignal.timeout(120_000), headers: { 'Content-Type': 'application/json', ...options?.headers } })
  } catch (error) {
    if (error instanceof Error && error.name === 'TimeoutError') throw new Error('Сервер не ответил за две минуты. Предыдущие результаты сохранены; повторите запрос.')
    throw new Error(fallbackError)
  }
  if (!response.ok) throw responseError(response.status, await response.json().catch(() => null))
  return response
}
export function parseRun(value: unknown): RunResult {
  if (!value || typeof value !== 'object') throw new Error('API вернул пустой результат расчёта.')
  const run = value as RunResult
  if (run.status !== 'COMPLETED' || !run.runId || !Array.isArray(run.items) || !run.summary || !Array.isArray(run.warnings)) {
    throw new Error('Формат ответа API не совпадает с контрактом frontend. Ожидается завершённый расчёт с items и summary.')
  }
  const numeric = (n: unknown) => typeof n === 'number' && Number.isFinite(n)
  if (![run.summary.buy, run.summary.noBuy, run.summary.review, run.summary.totalUnits].every(numeric) || (run.summary.estimatedCost !== null && !numeric(run.summary.estimatedCost))) throw new Error('API вернул некорректные итоговые показатели.')
  for (const item of run.items) {
    if (!item.code1C || !item.name || !['BUY', 'NO_BUY', 'REVIEW'].includes(item.decision) || !item.breakdown || !item.explanation || !Array.isArray(item.warnings) || !Array.isArray(item.anomalies) || !Array.isArray(item.demand)) throw new Error('В ответе API отсутствуют обязательные данные позиции. Проверьте контракт.')
  }
  return run
}
export function parseDataset(value: unknown): Dataset {
  const dataset = value as Dataset | null
  if (!dataset?.datasetId || !dataset.supplier || typeof dataset.usable !== 'boolean' || !Array.isArray(dataset.warnings)) throw new Error('API вернул некорректный результат проверки файлов.')
  return dataset
}
function importFiles(files: ImportFiles, onProgress: (value: number | null) => void): Promise<Dataset> {
  return new Promise((resolve, reject) => {
    const form = new FormData()
    for (const [key, file] of Object.entries(files)) {
      if (!file) { reject(new Error('Выберите все шесть обязательных файлов.')); return }
      form.append(key, file)
    }
    const xhr = new XMLHttpRequest()
    xhr.open('POST', endpoint('/api/v1/import'))
    xhr.timeout = 120_000
    xhr.upload.onprogress = (event) => onProgress(event.lengthComputable ? Math.round(event.loaded / event.total * 100) : null)
    xhr.onerror = () => reject(new Error(fallbackError))
    xhr.ontimeout = () => reject(new Error('Загрузка заняла больше двух минут. Попробуйте ещё раз.'))
    xhr.onload = () => {
      let body: unknown
      try { body = JSON.parse(xhr.responseText) } catch { reject(new Error('Сервер вернул ответ в неизвестном формате.')); return }
      if (xhr.status < 200 || xhr.status >= 300) { reject(responseError(xhr.status, body)); return }
      try { resolve(parseDataset(body)) } catch (error) { reject(error) }
    }
    xhr.send(form)
  })
}
const liveApi: SupplyLensApi = {
  capabilities: {
    demoDataset: Boolean(import.meta.env.VITE_DEMO_DATASET_ENDPOINT),
    nvidiaReview: import.meta.env.VITE_ENABLE_NVIDIA_REVIEW === 'true',
    openaiExplanation: import.meta.env.VITE_ENABLE_OPENAI_EXPLANATION === 'true',
  },
  importFiles,
  loadDemoDataset: async () => {
    const path = import.meta.env.VITE_DEMO_DATASET_ENDPOINT
    if (!path) throw new Error('Go API не настроен для загрузки демо-набора.')
    return parseDataset(await (await request(path, { method: 'POST' })).json())
  },
  createRun: async (params) => parseRun(await (await request('/api/v1/runs', { method: 'POST', body: JSON.stringify(params) })).json()),
  patchItem: async (runId, code, patch) => parseRun(await (await request(`/api/v1/runs/${encodeURIComponent(runId)}/items/${encodeURIComponent(code)}`, { method: 'PATCH', body: JSON.stringify(patch) })).json()),
  approveExport: async (runId): Promise<ExportResult> => {
    const response = await request(`/api/v1/runs/${encodeURIComponent(runId)}/approve-export`, { method: 'POST', body: '{}' })
    const type = response.headers.get('content-type') || ''
    if (!/csv|octet-stream/i.test(type)) throw new Error('Сервер не вернул CSV. Заказ не отмечен как экспортированный.')
    const disposition = response.headers.get('content-disposition') || ''
    const match = disposition.match(/filename\*?=(?:UTF-8''|"?)([^";]+)/i)
    let filename = `SupplyLens-${runId}.csv`
    if (match) { try { filename = decodeURIComponent(match[1]).replace(/[\\/:*?"<>|]/g, '_') } catch { /* Keep safe fallback filename. */ } }
    return { blob: await response.blob(), filename }
  },
}
export const getApi = (mode: 'demo' | 'api'): SupplyLensApi => mode === 'demo' ? demoApi : liveApi
export function downloadExport(result: ExportResult) {
  const url = URL.createObjectURL(result.blob)
  const link = document.createElement('a')
  link.href = url; link.download = result.filename
  document.body.append(link); link.click(); link.remove()
  setTimeout(() => URL.revokeObjectURL(url), 30_000)
}
