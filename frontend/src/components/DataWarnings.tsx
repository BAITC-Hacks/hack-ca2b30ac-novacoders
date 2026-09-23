import { useState } from 'react'
import { AlertCircle, AlertTriangle, Info } from 'lucide-react'
import type { Warning } from '../api/types'

export function DataWarnings({ warnings, compact = false }: { warnings: Warning[]; compact?: boolean }) {
  const [visible, setVisible] = useState(8)
  if (!warnings.length) return null
  return <div className={`warnings ${compact ? 'compact' : ''}`}>
    {warnings.slice(0, visible).map((warning, index) => {
      const Icon = warning.severity === 'ERROR' ? AlertCircle : warning.severity === 'INFO' ? Info : AlertTriangle
      return <div key={`${warning.code}-${index}`} className={`notice notice-${warning.severity === 'ERROR' ? 'error' : warning.severity === 'INFO' ? 'info' : 'warning'}`}><Icon size={17} aria-hidden="true" /><span>{warning.code1C && <strong>{warning.code1C}: </strong>}{warning.message}{warning.row && <small> Строка {warning.row}</small>}</span></div>
    })}
    {visible < warnings.length && <button className="text-button" onClick={() => setVisible((n) => n + 20)}>Показать ещё замечания ({warnings.length - visible})</button>}
  </div>
}
export function ErrorNotice({ message, onRetry }: { message: string | null; onRetry?: () => void }) {
  if (!message) return null
  return <div className="notice notice-error" role="alert"><AlertCircle size={18} aria-hidden="true" /><span>{message}</span>{onRetry && <button className="text-button shrink-0" onClick={onRetry}>Повторить</button>}</div>
}
