import { Download, LoaderCircle } from 'lucide-react'
import type { RunResult } from '../api/types'
import { formatMoney, formatNumber } from '../lib/format'

export default function ExportBar({ run, busy, onExport }: { run: RunResult; busy: boolean; onExport: () => void }) {
  const order = run.orderSummary
  return <div className="export-bar"><div className="export-summary"><strong>{formatNumber(order.positions)} подтверждённых позиций <span>·</span> {formatMoney(order.estimatedCost)}</strong><p>{formatNumber(order.totalUnits)} единиц. В CSV попадут только подтверждённые количества больше нуля.</p></div><button className="btn btn-primary" disabled={busy || order.positions === 0} onClick={onExport}>{busy ? <LoaderCircle size={17} className="spin" /> : <Download size={17} />} Скачать подтверждённый заказ</button>{order.positions === 0 && <p className="export-block-reason">Откройте позицию и подтвердите количество для заказа.</p>}</div>
}
