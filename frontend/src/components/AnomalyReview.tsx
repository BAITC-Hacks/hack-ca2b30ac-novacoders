import { Check, CircleHelp, Minus, RotateCcw } from 'lucide-react'
import type { Anomaly, AnomalyDecision } from '../api/types'
import { formatDate, formatNumber } from '../lib/format'

const verdicts = { ONE_OFF: 'Возможная разовая продажа', RECURRING: 'Регулярная крупная продажа', UNCERTAIN: 'Нужна проверка', NOT_EVALUATED: 'Не оценивалась' }
export default function AnomalyReview({ anomalies, decision, busy, onDecision }: { anomalies: Anomaly[]; decision: AnomalyDecision; busy: boolean; onDecision: (decision: AnomalyDecision) => void }) {
  if (!anomalies.length) return null
  return <section className="anomaly-card"><h3>Проверка аномалий</h3>
    {anomalies.map((a, index) => <div key={`${a.transactionId}-${index}`} className="anomaly-event"><h4>{verdicts[a.nvidiaVerdict] || 'Нужна проверка'}</h4><p className="muted">{a.transactionId} · {formatDate(a.date)}</p><dl className="anomaly-metrics"><div><dt>Объём продажи</dt><dd>{formatNumber(a.quantity)}</dd></div><div><dt>Обычная операция</dt><dd>{formatNumber(a.medianTransactionQuantity)}</dd></div><div><dt>Отклонение</dt><dd>{formatNumber(a.deviationRatio)}</dd></div></dl>{a.reason && <p className="anomaly-reason">{a.reason}</p>}<div className="confidence">Уверенность: {a.confidence == null ? 'Нет данных' : new Intl.NumberFormat('ru', { style: 'percent', maximumFractionDigits: 0 }).format(a.confidence)}</div></div>)}
    <p>Решение применяется ко всем показанным операциям этой позиции. После изменения решения подтвердите окончательное количество вручную: текущая рекомендация автоматически не пересчитывается.</p>
    <div className="anomaly-actions">{([
      ['EXCLUDED', 'Подтвердить исключение', Minus], ['INCLUDED', 'Считать регулярным спросом', RotateCcw], ['PENDING_REVIEW', 'Оставить на проверке', CircleHelp],
    ] as const).map(([value, label, Icon]) => <button key={value} disabled={busy} aria-pressed={decision === value} onClick={() => onDecision(value)}><Icon size={14} />{label}{decision === value && <Check size={14} />}</button>)}</div>
  </section>
}
