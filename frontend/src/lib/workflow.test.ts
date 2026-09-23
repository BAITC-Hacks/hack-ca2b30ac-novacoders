import { describe, expect, it } from 'vitest'
import { defaultRunParams, validRunParams } from './workflow'

describe('Calculation settings', () => {
  it('supplies the four fields of the backend contract', () => {
    expect(defaultRunParams()).toEqual({ forecastHorizonMonths: 2, leadTimeDays: 30, safetyStockDays: 14, excludePartialMonth: true })
  })
  it('preserves zero safety and explicit false', () => {
    expect(validRunParams({ ...defaultRunParams(), safetyStockDays: 0, excludePartialMonth: false })).toBe(true)
  })
  it('rejects fractions and values outside supported horizons', () => {
    for (const values of [{ forecastHorizonMonths: 0 }, { forecastHorizonMonths: 37 }, { leadTimeDays: 0 }, { leadTimeDays: 366 }, { safetyStockDays: -1 }, { safetyStockDays: 366 }, { safetyStockDays: NaN }, { forecastHorizonMonths: 1.5 }]) expect(validRunParams({ ...defaultRunParams(), ...values })).toBe(false)
  })
})
