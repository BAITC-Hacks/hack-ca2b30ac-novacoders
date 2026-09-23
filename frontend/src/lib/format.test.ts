import { describe, expect, it } from 'vitest'
import { formatMoney, quantityError, quantityWarning, sortItems } from './format'
import type { RecommendationItem } from '../types/api'

describe('Manager quantity validation', () => {
  it('accepts zero and whole quantities, rejects invalid and unsafe values', () => {
    for (const value of ['0', '10', '100']) expect(quantityError(value)).toBeNull()
    for (const value of ['', '-1', '1.5', '1e3', 'NaN', '9007199254740992']) expect(quantityError(value)).not.toBeNull()
  })
  it('warns without silently rounding, and allows zero', () => {
    expect(quantityWarning('0', 10, 10)).toBeNull()
    expect(quantityWarning('5', 10, 10)).toContain('минимальной партии')
    expect(quantityWarning('95', 10, 10)).toContain('кратности')
    expect(quantityWarning('100', 10, 10)).toBeNull()
    expect(quantityWarning('15', null, null)).toBeNull()
  })
  it('distinguishes a zero cost from unknown cost', () => {
    expect(formatMoney(0)).toBe('0 ₸')
    expect(formatMoney(null)).toBe('Нет данных')
  })
})
describe('Recommendation priority', () => {
  it('puts review before critical buy, other buy and no-buy, without mutating input', () => {
    const items = [
      { name: 'A', decision: 'NO_BUY', urgency: 'LOW' },
      { name: 'B', decision: 'BUY', urgency: 'LOW' },
      { name: 'C', decision: 'BUY', urgency: 'CRITICAL' },
      { name: 'D', decision: 'REVIEW', urgency: 'LOW' },
    ] as RecommendationItem[]
    expect(sortItems(items).map((item) => item.name)).toEqual(['D', 'C', 'B', 'A'])
    expect(items[0].name).toBe('A')
  })
})
