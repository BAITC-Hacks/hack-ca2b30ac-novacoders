import { Download, LoaderCircle } from 'lucide-react'
import type { RunResult } from '../types/api'
import { formatMoney, formatNumber } from '../lib/format'

export default function ExportBar({ run, busy, onExport }: { run: RunResult; busy: boolean; onExport: () => void }) {
  return <div className="export-bar"><div className="export-summary"><strong>{formatNumber(run.summary.buy)} позиций <span>·</span> {formatMoney(run.summary.estimatedCost)}</strong><p>{formatNumber(run.summary.totalUnits)} единиц{run.summary.review > 0 && <span className="export-review"> · {run.summary.review} на проверке не включены</span>}</p></div><button className="btn btn-primary" disabled={busy || !run.exportReady || run.summary.buy === 0} onClick={onExport}>{busy ? <LoaderCircle size={17} className="spin" /> : <Download size={17} />} {busy ? 'Формирование CSV…' : 'Подтвердить и скачать CSV'}</button>{!run.exportReady && run.exportBlockReason && <p className="export-block-reason">{run.exportBlockReason}</p>}</div>
}
