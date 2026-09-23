export type Decision = 'BUY' | 'NO_BUY' | 'REVIEW'
export type Urgency = 'HIGH' | 'MEDIUM' | 'LOW' | 'NONE'
export type Supplier = 'SystemElectric' | 'IEK'
export type Stage = 'IMPORT' | 'CONFIGURE' | 'RESULTS' | 'APPROVED'
export type FileKind = 'moqFile' | 'monthlySalesFile' | 'detailedSalesFile' | 'monthlyStockFile' | 'seasonalityFile' | 'inventoryTransitFile'
export type ImportFiles = Record<FileKind, File | null>
export interface Warning {
  code: string; message: string; severity: 'INFO' | 'WARNING' | 'ERROR'
  code1C?: string; file?: string; row?: number; month?: string
}
export interface ImportResult {
  importId: string; status: 'READY'; supplier: Supplier; asOf: string
  files: { type: string; fileName: string; status: 'VALID' | 'WARNING'; rows: number }[]
  summary: { productsFound: number; productsReady: number; productsWithWarnings: number }
  warnings: Warning[]
}
export interface RunParams {
  forecastHorizonMonths: number; leadTimeDays: number; safetyStockDays: number; excludePartialMonth: boolean
}
export interface RunSummary {
  buy: number; noBuy: number; review: number; totalUnits: number; estimatedCost: number
  unpricedItems: number; approvedItems: number
}
export interface OrderSummary { positions: number; totalUnits: number; estimatedCost: number | null }
export interface Calculation {
  historyPeriod?: { from: string; to: string }
  baseMonthlyDemand?: number | null; anomalyExcludedQuantity?: number | null
  stockoutCompensation?: number | null; correctedMonthlyDemand?: number | null
  growthFactor?: number | null; seasonalityFactor?: number | null
  forecastHorizonMonths?: number | null; forecastDemand?: number | null; safetyStock?: number | null
  freeStock?: number | null; inTransit?: number | null; availableStock?: number | null
  rawRequirement?: number | null; moq?: number | null; roundedRequirement?: number | null
}
export type AnomalyDecision = 'EXCLUDED' | 'INCLUDED' | 'PENDING_REVIEW'
export interface Anomaly {
  transactionId: string; date: string; quantity: number
  medianTransactionQuantity?: number | null; deviationRatio?: number | null
  nvidiaVerdict: 'ONE_OFF' | 'RECURRING' | 'UNCERTAIN' | 'NOT_EVALUATED'
  confidence?: number | null; systemDecision: AnomalyDecision; reason?: string
}
export interface Explanation { short: string; details: string[]; generatedBy: string }
export interface RecommendationItem {
  code1C: string; article: string; name: string; category: string; supplier: Supplier
  decision: Decision; urgency: Urgency; recommendedQuantity: number
  approvedQuantity: number | null; approved: boolean; finalQuantity: number; updatedAt: string
  estimatedCost: number | null; unitCost: number | null; calculation: Calculation
  anomalyAnalysis?: { status: string; candidates: Anomaly[] } | null
  anomalyDecision: AnomalyDecision; warnings: Warning[]; explanation?: Explanation | null
  requiresManualReview: boolean; confidence?: number | null; managerComment: string
}
export interface RunResult {
  runId: string; importId: string; supplier: Supplier; asOf: string; createdAt: string
  status: 'COMPLETED' | 'COMPLETED_WITH_WARNINGS'; settings: RunParams
  summary: RunSummary; orderSummary: OrderSummary; items: RecommendationItem[]; warnings: Warning[]
  providers: Record<string, { status: 'USED' | 'SKIPPED' | 'FALLBACK' | 'FAILED' }>
}
export interface ItemPatch { approvedQuantity?: number; approved?: boolean; anomalyDecision?: AnomalyDecision; comment?: string }
export interface ItemPatchResult { code1C: string; approvedQuantity: number | null; approved: boolean; updatedAt: string }
export interface ExportResult { blob: Blob; filename: string }
