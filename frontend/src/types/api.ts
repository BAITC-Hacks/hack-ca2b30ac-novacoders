export type Decision = 'BUY' | 'NO_BUY' | 'REVIEW'
export type Urgency = 'CRITICAL' | 'HIGH' | 'MEDIUM' | 'LOW'
export type Mode = 'demo' | 'api'
export type Stage = 'IMPORT' | 'CONFIGURE' | 'RESULTS' | 'APPROVED'
export type FileKind = 'moq' | 'salesDetails' | 'monthlyStock' | 'monthlySales' | 'seasonality' | 'inTransit'
export type ImportFiles = Record<FileKind, File | null>
export interface Warning { code: string; message: string; severity: 'info' | 'warning' | 'error' }
export interface Capabilities { demoDataset: boolean; nvidiaReview: boolean; openaiExplanation: boolean }
export interface Dataset {
  datasetId: string; supplier: string; warehouse: string; asOf: string
  productsFound: number; matchedProducts: number; reviewProducts: number
  usable: boolean; warnings: Warning[]; capabilities?: Capabilities
}
export interface RunParams {
  datasetId: string; supplier: string; leadTimeDays: number; safetyDays: number
  includeShowcase: boolean; includeTZStock: boolean; includeRetailStock: boolean
  useNvidiaReview: boolean; useOpenAIExplanation: boolean
}
export interface RunSummary {
  buy: number; noBuy: number; review: number; totalUnits: number; estimatedCost: number | null
}
export interface CalculationBreakdown {
  baseDemand: number; seasonalityFactor: number; growthFactor: number
  stockoutCompensation: number; safetyStock: number; freeStock: number; inTransit: number
  forecast: number; netRequirement: number; moq: number | null; orderMultiple: number | null
  recommendedOrder: number
}
export interface DemandPoint {
  month: string; sales: number | null; cleanedDemand: number | null; forecast: number | null
  stockout?: boolean; anomaly?: number; incomplete?: boolean
}
export type AnomalyDecision = 'EXCLUDE' | 'REGULAR' | 'REVIEW'
export interface Anomaly {
  id: string; source: 'NVIDIA' | 'SYSTEM'; classification: string; date: string
  quantity: number; typicalQuantity: number; monthShare: number; confidence: string
  reason: string; reviewable: boolean; decision: AnomalyDecision
}
export interface Explanation { source: 'AI' | 'SYSTEM'; summary: string; reasons: string[] }
export interface RecommendationItem {
  code1C: string; article: string; name: string; category: string; supplier: string; unit: string
  decision: Decision; urgency: Urgency; recommendedQuantity: number; approvedQuantity?: number
  estimatedCost: number | null; breakdown: CalculationBreakdown; anomalies: Anomaly[]
  warnings: Warning[]; explanation: Explanation; demand: DemandPoint[]; managerComment?: string
}
export interface RunResult {
  runId: string; supplier: string; asOf: string; status: 'COMPLETED'
  summary: RunSummary; items: RecommendationItem[]; warnings: Warning[]
  exportReady: boolean; exportBlockReason?: string
}
export interface ItemPatch {
  approvedQuantity?: number; managerComment?: string
  anomalyId?: string; anomalyDecision?: AnomalyDecision
}
export interface ExportResult { blob: Blob; filename: string }
export interface SupplyLensApi {
  capabilities: Capabilities
  importFiles: (files: ImportFiles, onProgress: (percent: number | null) => void) => Promise<Dataset>
  loadDemoDataset: () => Promise<Dataset>
  createRun: (params: RunParams) => Promise<RunResult>
  patchItem: (runId: string, code: string, patch: ItemPatch) => Promise<RunResult>
  approveExport: (runId: string) => Promise<ExportResult>
}
