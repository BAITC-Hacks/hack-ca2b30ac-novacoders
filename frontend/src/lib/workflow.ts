import type { RunParams } from '../api/types'
export function defaultRunParams(): RunParams {
  return { forecastHorizonMonths: 2, leadTimeDays: 30, safetyStockDays: 14, excludePartialMonth: true }
}
export function validRunParams(params: RunParams): boolean {
  return Number.isInteger(params.forecastHorizonMonths) && params.forecastHorizonMonths >= 1 && params.forecastHorizonMonths <= 36 && Number.isInteger(params.leadTimeDays) && params.leadTimeDays >= 1 && params.leadTimeDays <= 365 && Number.isInteger(params.safetyStockDays) && params.safetyStockDays >= 0 && params.safetyStockDays <= 365 && typeof params.excludePartialMonth === 'boolean'
}
