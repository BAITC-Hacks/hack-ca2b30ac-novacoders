/** Isolated demo transport. No real Excel parsing, forecast, NVIDIA or OpenAI calls.
 * Values below are synthetic fixtures; only mock edits and totals are simulated here.
 */
import type { Dataset, DemandPoint, RecommendationItem, RunResult, SupplyLensApi, Warning } from '../types/api'

const delay = (ms = 550) => new Promise<void>((resolve) => setTimeout(resolve, ms))
const partialMonth: Warning = { code: 'PARTIAL_MONTH', severity: 'info', message: 'Сентябрь 2026 — неполный месяц. Данные по 22 сентября; не сравнивайте его с полными месяцами.' }
const demoDataset: Dataset = {
  datasetId: 'demo-systeme-2026', supplier: 'SystemElectric', warehouse: 'Алматы', asOf: '2026-09-22',
  productsFound: 16, matchedProducts: 13, reviewProducts: 3, usable: true,
  capabilities: { demoDataset: true, nvidiaReview: true, openaiExplanation: true },
  warnings: [partialMonth, { code: 'DEMO', severity: 'info', message: 'Демонстрационный набор: 16 синтетических позиций. Файлы и AI-сервисы в этом режиме не обрабатываются.' }],
}
const chart: DemandPoint[] = [
  { month: 'Апр', sales: 84, cleanedDemand: 84, forecast: null },
  { month: 'Май', sales: 421, cleanedDemand: 1, forecast: null, anomaly: 420 },
  { month: 'Июн', sales: 98, cleanedDemand: 98, forecast: null },
  { month: 'Июл', sales: 42, cleanedDemand: 42, forecast: null, stockout: true },
  { month: 'Авг', sales: 113, cleanedDemand: 113, forecast: null },
  { month: 'Сен*', sales: 68, cleanedDemand: 68, forecast: null, incomplete: true },
  { month: 'Окт', sales: null, cleanedDemand: null, forecast: 140 },
  { month: 'Ноя', sales: null, cleanedDemand: null, forecast: 132 },
]
type Seed = [string, string, string, string, number, number, number, number, number | null, RecommendationItem['decision'], RecommendationItem['urgency']]
const seeds: Seed[] = [
  ['300200428_', 'ATN000343', 'Розетка AtlasDesign с заземлением, алюминий', 'Розетки', 40, 40, 140, 90, 108_900, 'REVIEW', 'HIGH'],
  ['300200589_', 'ATN000351', 'Выключатель AtlasDesign двухклавишный, алюминий', 'Выключатели', 12, 0, 108, 120, 152_040, 'REVIEW', 'MEDIUM'],
  ['030200128_', 'VS616-157B-18', 'Переключатель Wessen 59, IP44, белый', 'Выключатели', 0, 0, 8, 10, null, 'REVIEW', 'LOW'],
  ['300200430_', 'ATN000312', 'Выключатель AtlasDesign одноклавишный, алюминий', 'Выключатели', 8, 20, 170, 180, 231_507, 'BUY', 'CRITICAL'],
  ['300200588_', 'ATN000311', 'Механизм выключателя AtlasDesign, алюминий', 'Выключатели', 5, 0, 155, 170, 152_957.5, 'BUY', 'CRITICAL'],
  ['300200432_', 'ATN000352', 'Выключатель AtlasDesign двухклавишный, алюминий', 'Выключатели', 34, 20, 178, 160, 176_844.8, 'BUY', 'HIGH'],
  ['300200433_', 'ATN000383', 'Розетка компьютерная AtlasDesign RJ45, кат. 5E', 'Розетки', 18, 0, 76, 80, 257_053.6, 'BUY', 'HIGH'],
  ['300200439_', 'ATN000361', 'Переключатель AtlasDesign одноклавишный', 'Выключатели', 25, 10, 87, 70, 80_336.9, 'BUY', 'MEDIUM'],
  ['300200445_', 'ATN000305', 'Рамка AtlasDesign на 5 постов, алюминий', 'Рамки', 32, 0, 93, 80, 107_200, 'BUY', 'MEDIUM'],
  ['300200450_', 'ATN000101', 'Рамка AtlasDesign на 1 пост, белый', 'Рамки', 48, 100, 296, 200, 38_000, 'BUY', 'MEDIUM'],
  ['300200451_', 'ATN000102', 'Рамка AtlasDesign на 2 поста, белый', 'Рамки', 90, 0, 168, 100, 37_000, 'BUY', 'LOW'],
  ['300200443_', 'ATN000313', 'Выключатель AtlasDesign с индикацией, алюминий', 'Выключатели', 144, 50, 65, 0, 0, 'NO_BUY', 'LOW'],
  ['300200444_', 'ATN000353', 'Выключатель AtlasDesign двухклавишный с индикацией', 'Выключатели', 96, 0, 31, 0, 0, 'NO_BUY', 'LOW'],
  ['300200449_', 'ATN000331', 'Выключатель AtlasDesign трёхклавишный', 'Выключатели', 56, 30, 40, 0, 0, 'NO_BUY', 'LOW'],
  ['300200442_', 'ATN000333', 'Розетка AtlasDesign USB, 2 порта, алюминий', 'Розетки', 24, 0, 10, 0, 0, 'NO_BUY', 'LOW'],
  ['030200382_', 'VS510-252-18', 'Выключатель Wessen 59 двухклавишный, белый', 'Выключатели', 80, 0, 32, 0, 0, 'NO_BUY', 'LOW'],
]
function fixtureItems(): RecommendationItem[] {
  return seeds.map(([code1C, article, name, category, freeStock, inTransit, forecast, quantity, cost, decision, urgency], index) => ({
    code1C, article, name, category, supplier: 'SystemElectric', unit: 'шт.', decision, urgency,
    recommendedQuantity: quantity, estimatedCost: cost,
    breakdown: index === 0 ? { baseDemand: 100, seasonalityFactor: 1.2, growthFactor: 1, stockoutCompensation: 20, safetyStock: 25, freeStock, inTransit, forecast, netRequirement: 85, moq: 10, orderMultiple: 10, recommendedOrder: 90 } : {
      baseDemand: forecast, seasonalityFactor: 1, growthFactor: 1, stockoutCompensation: 0,
      safetyStock: quantity ? Math.max(0, quantity + freeStock + inTransit - forecast) : 0,
      freeStock, inTransit, forecast, netRequirement: quantity,
      moq: index === 2 ? null : 10, orderMultiple: index === 2 ? null : 10, recommendedOrder: quantity,
    },
    anomalies: index === 0 ? [{ id: 'anomaly-demo-001', source: 'NVIDIA', classification: 'Вероятно разовая продажа', date: '2026-05-14', quantity: 420, typicalQuantity: 10, monthShare: 99.8, confidence: 'Высокая', reason: 'Операция значительно превышает обычный размер заказа и не повторяется регулярно. Требуется решение закупщика.', reviewable: true, decision: 'REVIEW' }] : [],
    warnings: index === 0 ? [{ code: 'ANOMALY_REVIEW', severity: 'warning', message: 'Крупная операция требует вашего подтверждения.' }] : index === 1 ? [{ code: 'SALES_CONFLICT', severity: 'warning', message: 'Месячные и детальные продажи расходятся. Проверьте исходные данные перед заказом.' }] : index === 2 ? [{ code: 'MOQ_MISSING', severity: 'warning', message: 'Нет минимальной партии и кратности. Уточните условия поставщика.' }, { code: 'COST_UNKNOWN', severity: 'info', message: 'Закупочная стоимость неизвестна.' }, { code: 'SHORT_HISTORY', severity: 'info', message: 'Недостаточно истории для устойчивого прогноза.' }] : [],
    explanation: { source: index === 0 ? 'AI' : 'SYSTEM', summary: decision === 'NO_BUY' ? 'Текущий запас покрывает потребность на горизонт поставки.' : decision === 'REVIEW' ? 'Перед заказом проверьте данные и подтвердите решение.' : 'Пополните запас, чтобы покрыть ожидаемый спрос до следующей поставки.', reasons: index === 0 ? ['Разовая продажа 420 шт. выделена для проверки.', 'Учтены сезонность и компенсация периода отсутствия товара.', 'Потребность 85 шт. округлена до кратности 10.'] : decision === 'NO_BUY' ? ['Свободного остатка и ожидаемого прихода достаточно.', 'Дополнительный заказ сейчас не требуется.'] : ['Учтены свободный остаток и подтверждённый товар в пути.', 'Рекомендация учитывает страховой запас и кратность поставки.'] },
    demand: index === 2 ? [] : index === 0 ? structuredClone(chart) : [
      { month: 'Апр', sales: 80, cleanedDemand: 80, forecast: null },
      { month: 'Май', sales: 72, cleanedDemand: 72, forecast: null },
      { month: 'Июн', sales: 94, cleanedDemand: 94, forecast: null },
      { month: 'Июл', sales: 86, cleanedDemand: 86, forecast: null },
      { month: 'Авг', sales: 110, cleanedDemand: 110, forecast: null },
      { month: 'Сен*', sales: 66, cleanedDemand: 66, forecast: null, incomplete: true },
      { month: 'Окт', sales: null, cleanedDemand: null, forecast },
    ],
  }))
}
let currentRun: RunResult | null = null
function refreshMockSummary(): RunResult {
  if (!currentRun) throw new Error('Сначала запустите демонстрационный расчёт.')
  const buys = currentRun.items.filter((item) => item.decision === 'BUY')
  currentRun.summary = {
    buy: buys.length, noBuy: currentRun.items.filter((item) => item.decision === 'NO_BUY').length,
    review: currentRun.items.filter((item) => item.decision === 'REVIEW').length,
    totalUnits: buys.reduce((sum, item) => sum + (item.approvedQuantity ?? item.recommendedQuantity), 0),
    estimatedCost: buys.some((item) => item.estimatedCost === null) ? null : buys.reduce((sum, item) => sum + (item.estimatedCost ?? 0), 0),
  }
  currentRun.exportReady = buys.length > 0
  return structuredClone(currentRun)
}
const csvCell = (value: unknown) => `"${String(value ?? '').replace(/^[=+@-]/, "'$&").replace(/"/g, '""')}"`
export const demoApi: SupplyLensApi = {
  capabilities: { demoDataset: true, nvidiaReview: true, openaiExplanation: true },
  importFiles: async (_files, onProgress) => { onProgress(null); await delay(); return structuredClone(demoDataset) },
  loadDemoDataset: async () => { await delay(); return structuredClone(demoDataset) },
  createRun: async (params) => {
    await delay(1100)
    const items = fixtureItems()
    if (!params.useOpenAIExplanation) for (const item of items) item.explanation.source = 'SYSTEM'
    if (!params.useNvidiaReview) for (const item of items) for (const anomaly of item.anomalies) anomaly.source = 'SYSTEM'
    currentRun = { runId: `demo-${Date.now()}`, supplier: demoDataset.supplier, asOf: demoDataset.asOf, status: 'COMPLETED', items, summary: { buy: 0, noBuy: 0, review: 0, totalUnits: 0, estimatedCost: null }, warnings: [partialMonth], exportReady: true }
    return refreshMockSummary()
  },
  patchItem: async (runId, code, patch) => {
    await delay()
    if (!currentRun || currentRun.runId !== runId) throw new Error('Демо-расчёт не найден. Запустите его ещё раз.')
    const item = currentRun.items.find((entry) => entry.code1C === code)
    if (!item) throw new Error('Позиция не найдена.')
    if (patch.anomalyId && patch.anomalyDecision) {
      const anomaly = item.anomalies.find((entry) => entry.id === patch.anomalyId)
      if (!anomaly?.reviewable) throw new Error('Недостаточно данных для подтверждения аномалии.')
      anomaly.decision = patch.anomalyDecision
      item.decision = patch.anomalyDecision === 'REVIEW' ? 'REVIEW' : 'BUY'
      if (patch.anomalyDecision === 'REGULAR') {
        // Canned alternate backend response; this is not a forecasting algorithm.
        item.recommendedQuantity = 220; item.breakdown = { ...item.breakdown, baseDemand: 210, forecast: 272, netRequirement: 217, recommendedOrder: 220 }
        item.estimatedCost = 266_200; item.demand = item.demand.map((point) => point.month === 'Май' ? { ...point, cleanedDemand: 421 } : point.month === 'Окт' ? { ...point, forecast: 272 } : point)
      } else {
        item.recommendedQuantity = 90; item.breakdown = { ...item.breakdown, baseDemand: 100, forecast: 140, netRequirement: 85, recommendedOrder: 90 }
        item.estimatedCost = 108_900; item.demand = structuredClone(chart)
      }
      delete item.approvedQuantity
      item.warnings = patch.anomalyDecision === 'REVIEW' ? [{ code: 'ANOMALY_REVIEW', severity: 'warning', message: 'Крупная операция оставлена на проверку.' }] : []
      item.explanation = { source: 'SYSTEM', summary: patch.anomalyDecision === 'EXCLUDE' ? 'Разовая операция исключена из регулярного спроса.' : patch.anomalyDecision === 'REGULAR' ? 'Крупная операция учтена как регулярный спрос.' : 'Операция оставлена на ручную проверку.', reasons: ['Учтено решение менеджера.', 'Количество и объяснение обновлены демонстрационным API.'] }
    }
    if (patch.approvedQuantity !== undefined) {
      if (!Number.isSafeInteger(patch.approvedQuantity) || patch.approvedQuantity < 0) throw new Error('Количество должно быть целым и неотрицательным.')
      const price = item.estimatedCost !== null && (item.approvedQuantity ?? item.recommendedQuantity) > 0 ? item.estimatedCost / (item.approvedQuantity ?? item.recommendedQuantity) : null
      item.approvedQuantity = patch.approvedQuantity
      item.estimatedCost = patch.approvedQuantity === 0 ? 0 : price === null ? null : Math.round(price * patch.approvedQuantity * 100) / 100
      item.managerComment = patch.managerComment || ''
      const unresolved = item.anomalies.some((entry) => entry.reviewable && entry.decision === 'REVIEW')
      item.decision = unresolved ? 'REVIEW' : patch.approvedQuantity === 0 ? 'NO_BUY' : 'BUY'
    }
    return refreshMockSummary()
  },
  approveExport: async (runId) => {
    await delay()
    if (!currentRun || currentRun.runId !== runId) throw new Error('Демо-расчёт не найден.')
    const rows = currentRun.items.filter((item) => item.decision === 'BUY')
    if (!rows.length) throw new Error('Нет позиций, готовых к заказу.')
    const csv = [['Режим', 'Поставщик', 'Код 1С', 'Артикул', 'Товар', 'Количество', 'Ед.', 'Ориентировочная стоимость', 'Комментарий'], ...rows.map((item) => ['ДЕМО — НЕ ДЛЯ ЗАКУПКИ', item.supplier, item.code1C, item.article, item.name, item.approvedQuantity ?? item.recommendedQuantity, item.unit, item.estimatedCost, item.managerComment || ''])].map((row) => row.map(csvCell).join(';')).join('\r\n')
    return { blob: new Blob(['\uFEFF' + csv], { type: 'text/csv;charset=utf-8' }), filename: 'DEMO-SupplyLens-SystemElectric.csv' }
  },
}
