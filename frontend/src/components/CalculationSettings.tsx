import { ArrowLeft, ArrowRight, CheckCircle2, LoaderCircle } from 'lucide-react'
import type { ImportResult, RunParams } from '../api/types'
import { formatDate, formatNumber } from '../lib/format'
import { DataWarnings, ErrorNotice } from './DataWarnings'
import { validRunParams } from '../lib/workflow'

interface Props { dataset: ImportResult; params: RunParams; loading: boolean; error: string | null; onChange: (params: RunParams) => void; onRun: () => void; onBack: () => void }
export default function CalculationSettings({ dataset, params, loading, error, onChange, onRun, onBack }: Props) {
  const valid = validRunParams(params)
  const update = <K extends keyof RunParams>(key: K, value: RunParams[K]) => onChange({ ...params, [key]: value })
  const numericFields = [
    { key: 'forecastHorizonMonths', title: 'Горизонт прогноза', unit: 'мес.', min: 1, max: 36 },
    { key: 'leadTimeDays', title: 'Срок поставки', unit: 'дней', min: 1, max: 365 },
    { key: 'safetyStockDays', title: 'Страховой запас', unit: 'дней', min: 0, max: 365 },
  ] as const
  return <div className="configure-layout animate-in"><div className="space-y-5">
    <section className="panel"><div className="panel-heading"><div><h2>Условия поставки</h2><p>Параметры применяются ко всем товарам {dataset.supplier}</p></div></div><div className="settings-body">
      <div className="field-grid">{numericFields.map(({ key, title, unit, min, max }) => <label key={key} className="field">{title}<div className="number-field"><input aria-label={title} type="number" min={min} max={max} step="1" value={Number.isNaN(params[key]) ? '' : params[key]} disabled={loading} onChange={(e) => update(key, e.target.value === '' ? NaN : Number(e.target.value))} /><span>{unit}</span></div></label>)}</div>
      {!valid && <p role="alert" className="field-error">Горизонт: 1–36 месяцев. Срок поставки: 1–365 дней. Запас: 0–365 дней. Только целые числа.</p>}
      <label className="toggle-row"><div><strong>Исключать неполный месяц</strong><span>Не использовать незавершённый месяц при оценке спроса</span></div><input type="checkbox" role="switch" aria-label="Исключать неполный месяц" checked={params.excludePartialMonth} onChange={(e) => update('excludePartialMonth', e.target.checked)} disabled={loading} /><span className="toggle-track" aria-hidden="true" /></label>
      <p className="quiet-note">Доступный запас включает свободный остаток и товар в пути.</p>
    </div><div className="settings-actions"><button className="btn btn-secondary" onClick={onBack} disabled={loading}><ArrowLeft size={16} /> К файлам</button><button className="btn btn-primary" onClick={onRun} disabled={loading || !valid || dataset.summary.productsFound === 0}>{loading ? <LoaderCircle size={17} className="spin" /> : <ArrowRight size={17} />}{loading ? 'Выполняется расчёт…' : 'Рассчитать заказ'}</button></div></section>
    <ErrorNotice message={error} onRetry={loading || !valid ? undefined : onRun} />
    {loading && <div className="panel calculating" role="status"><strong>Формируем рекомендации</strong><p>Проверяем спрос, сезонность и остатки. Дождитесь результата.</p><div className="indeterminate" /></div>}
    <section className="panel file-validation"><div className="panel-heading"><h2>Результат проверки файлов</h2></div><ul>{dataset.files.map((file) => <li key={file.type}><div><strong>{file.fileName}</strong><span>{formatNumber(file.rows)} строк</span></div><span className={`soft-badge ${file.status === 'WARNING' ? 'text-amber-700' : ''}`}>{file.status === 'VALID' ? 'Проверен' : 'Есть замечания'}</span></li>)}</ul></section>
  </div><aside className="space-y-5"><section className="panel dataset-card"><span className="success-icon"><CheckCircle2 size={25} /></span><h2>Импорт завершён</h2><p>{dataset.supplier} · На {formatDate(dataset.asOf)}</p><dl className="import-summary"><div><dt>Товаров найдено</dt><dd>{formatNumber(dataset.summary.productsFound)}</dd></div><div><dt>Без замечаний</dt><dd>{formatNumber(dataset.summary.productsReady)}</dd></div><div><dt>Требуют проверки</dt><dd>{formatNumber(dataset.summary.productsWithWarnings)}</dd></div></dl></section><DataWarnings warnings={dataset.warnings} compact /></aside></div>
}
