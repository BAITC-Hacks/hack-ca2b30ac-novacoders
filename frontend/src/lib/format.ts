import type { Decision, RecommendationItem, Urgency } from '../api/types'
const numberFormat = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 2 })
export const formatNumber = (value: number | null | undefined) => value == null ? 'Нет данных' : numberFormat.format(value)
export const formatMoney = (value: number | null) => value === null ? 'Нет данных' : `${formatNumber(value)} ₸`
export const formatDate = (value: string) => new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC' }).format(new Date(value))
export const decisionLabels: Record<Decision, string> = { BUY: 'Заказать', NO_BUY: 'Не заказывать', REVIEW: 'Проверить' }
export const urgencyLabels: Record<Urgency, string> = { HIGH: 'Высокая', MEDIUM: 'Средняя', LOW: 'Низкая', NONE: 'Нет' }
export const sortItems = (items: RecommendationItem[]) => [...items].sort((a, b) => {
  const rank = (item: RecommendationItem) => item.decision === 'REVIEW' ? 0 : item.decision === 'BUY' ? item.urgency === 'HIGH' ? 1 : 2 : 3
  return rank(a) - rank(b) || a.name.localeCompare(b.name, 'ru')
})
export function quantityError(value: string): string | null {
  if (!/^\d+$/.test(value) || (!Number.isSafeInteger(Number(value)) || Number(value) > 1e12)) return 'Введите целое неотрицательное число.'
  return null
}
export function quantityWarning(value: string, moq: number | null, multiple: number | null): string | null {
  if (quantityError(value)) return null
  const quantity = Number(value)
  if (quantity === 0) return null
  if (moq !== null && quantity < moq) return `Количество меньше минимальной партии ${formatNumber(moq)}.`
  if (multiple !== null && multiple > 0 && quantity % multiple !== 0) return `Количество не соответствует кратности ${formatNumber(multiple)}. Значение сохранится без округления.`
  return null
}
