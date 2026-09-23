import { AlertCircle, AlertTriangle, Info } from 'lucide-react'
import type { Warning } from '../types/api'

export function DataWarnings({ warnings, compact = false }: { warnings: Warning[]; compact?: boolean }) {
  if (!warnings.length) return null
  return <div className={`warnings ${compact ? 'compact' : ''}`}>{warnings.map((warning, index) => {
    const Icon = warning.severity === 'error' ? AlertCircle : warning.severity === 'warning' ? AlertTriangle : Info
    return <div key={`${warning.code}-${index}`} className={`notice notice-${warning.severity}`}><Icon size={17} aria-hidden="true" /><span>{warning.message}</span></div>
  })}</div>
}

export function ErrorNotice({ message, onRetry }: { message: string | null; onRetry?: () => void }) {
  if (!message) return null
  return <div className="notice notice-error" role="alert"><AlertCircle size={18} aria-hidden="true" /><span>{message}</span>{onRetry && <button className="text-button shrink-0" onClick={onRetry}>Повторить</button>}</div>
}
