import type { RunSummary } from '../api/types'
import { formatMoney, formatNumber } from '../lib/format'

export default function SummaryCards({ summary }: { summary: RunSummary }) {
  return <dl className="summary-strip" aria-label="Итоги расчёта">
    <div className="summary-buy"><dt>К заказу</dt><dd>{formatNumber(summary.buy)} <span>поз.</span></dd></div>
    <div><dt>Не заказывать</dt><dd>{formatNumber(summary.noBuy)} <span>поз.</span></dd></div>
    <div className="summary-review"><dt>На проверке</dt><dd>{formatNumber(summary.review)} <span>поз.</span></dd></div>
    <div><dt>Предварительный объём</dt><dd>{formatNumber(summary.totalUnits)} <span>ед.</span></dd></div>
    <div className="summary-cost"><dt>Ориентировочная стоимость</dt><dd>{formatMoney(summary.unpricedItems > 0 ? null : summary.estimatedCost)}</dd></div>
  </dl>
}
