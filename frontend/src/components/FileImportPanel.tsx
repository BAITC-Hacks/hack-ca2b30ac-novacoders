import { useRef, useState } from 'react'
import { ArrowRight, Check, FileSpreadsheet, LoaderCircle, Upload, X } from 'lucide-react'
import type { FileKind, ImportFiles, Mode } from '../types/api'
import { ErrorNotice } from './DataWarnings'

const slots: { key: FileKind; title: string; description: string; number: string }[] = [
  { key: 'moq', title: 'MOQ и кратность', description: 'Минимальные партии поставщика', number: '01' },
  { key: 'salesDetails', title: 'Детальные продажи', description: 'История отгрузок по документам', number: '02' },
  { key: 'monthlyStock', title: 'Ежемесячные остатки', description: 'История наличия товаров на складе', number: '03' },
  { key: 'monthlySales', title: 'Ежемесячные продажи', description: 'Объём продаж в разрезе месяцев', number: '04' },
  { key: 'seasonality', title: 'Сезонность', description: 'Сезонные коэффициенты спроса', number: '05' },
  { key: 'inTransit', title: 'Товар в пути', description: 'Ожидаемые поставки и даты прихода', number: '06' },
]
const emptyFiles: ImportFiles = { moq: null, salesDetails: null, monthlyStock: null, monthlySales: null, seasonality: null, inTransit: null }
function FileSlot({ slot, file, loading, onChange }: { slot: typeof slots[number]; file: File | null; loading: boolean; onChange: (file: File | null) => void }) {
  const input = useRef<HTMLInputElement>(null)
  const [dragging, setDragging] = useState(false)
  const [error, setError] = useState('')
  function accept(files: FileList | null) {
    if (loading || !files?.length) return
    if (files.length > 1) { setError('Выберите один файл для этого источника.'); return }
    const value = files[0]
    if (!/\.xlsx?$/i.test(value.name)) { setError('Нужен файл Excel в формате .xlsx или .xls.'); return }
    if (value.size === 0) { setError('Файл пустой. Выберите другой файл.'); return }
    setError(''); onChange(value)
  }
  return <div className={`file-slot ${file ? 'has-file' : ''} ${dragging ? 'dragging' : ''} ${error ? 'file-error' : ''}`} onDragOver={(e) => { e.preventDefault(); if (!loading) setDragging(true) }} onDragLeave={() => setDragging(false)} onDrop={(e) => { e.preventDefault(); setDragging(false); accept(e.dataTransfer.files) }}>
    <span className="slot-number">{slot.number}</span><div className="slot-info"><h3>{slot.title}</h3><p className="slot-description">{slot.description}</p></div>
    <input ref={input} type="file" accept=".xlsx,.xls" className="sr-only" aria-label={`Файл: ${slot.title}`} disabled={loading} onChange={(e) => { accept(e.target.files); e.target.value = '' }} />
    {file ? <div className="selected-file"><FileSpreadsheet size={16} /><div><strong title={file.name}>{file.name}</strong><span>{file.size < 1024 * 1024 ? `${Math.ceil(file.size / 1024)} КБ` : `${(file.size / (1024 * 1024)).toFixed(1)} МБ`}</span></div><button className="icon-button" disabled={loading} aria-label={`Удалить ${file.name}`} onClick={() => { onChange(null); setError('') }}><X size={15} /></button></div> : <button className="file-picker" disabled={loading} onClick={() => input.current?.click()}><Upload size={16} /> Выбрать Excel <span>или перетащить сюда</span></button>}
    <div className={`file-state ${error ? 'state-error' : file ? 'state-ready' : ''}`} role={error ? 'alert' : undefined}>{error ? error : loading && file ? <><LoaderCircle size={12} className="spin" /> Загружается</> : file ? <><Check size={13} /> Готов</> : 'Не выбран'}</div>
  </div>
}
interface Props { mode: Mode; canDemo: boolean; loading: boolean; progress: number | null; error: string | null; onImport: (files: ImportFiles) => void; onDemo: () => void }
export default function FileImportPanel({ mode, canDemo, loading, progress, error, onImport, onDemo }: Props) {
  const [files, setFiles] = useState<ImportFiles>(emptyFiles)
  const count = Object.values(files).filter(Boolean).length
  const [lastAction, setLastAction] = useState<'files' | 'demo'>('files')
  return <div className="import-layout animate-in">
    <section className="panel import-panel">
      <div className="panel-heading"><div><h2>Источники данных</h2><p>Шесть Excel-файлов одного поставщика</p></div><span className="soft-badge">Выбрано {count} из 6</span></div>
      <div className="file-grid">{slots.map((slot) => <FileSlot key={slot.key} slot={slot} file={files[slot.key]} loading={loading} onChange={(file) => setFiles((current) => ({ ...current, [slot.key]: file }))} />)}</div>
      <div className="import-foot">
        <p>Форматы .xlsx и .xls · 6 обязательных файлов</p>
        <button className="btn btn-primary" disabled={count !== 6 || loading} onClick={() => { setLastAction('files'); onImport(files) }}>{loading && lastAction === 'files' ? <LoaderCircle size={17} className="spin" /> : <Upload size={17} />} Загрузить и проверить</button>
      </div>
      {loading && <div className="upload-state" role="status">{mode === 'api' && progress !== null ? <><progress max="100" value={progress} aria-label="Отправка файлов" /><span>{progress === 100 ? 'Файлы переданы. Сервер проверяет данные…' : `Отправлено ${progress}%`}</span></> : <><div className="indeterminate" /><span>{mode === 'demo' ? 'Подготовка демонстрационного набора…' : 'Загрузка и проверка данных…'}</span></>}</div>}
      <ErrorNotice message={error} onRetry={loading ? undefined : () => lastAction === 'demo' ? onDemo() : onImport(files)} />
    </section>
    <aside className="import-aside">
      <section className="source-note"><h2>Перед загрузкой</h2><ol><li>Соберите отчёты одного поставщика за один период.</li><li>Сохраните исходные названия колонок и листов.</li><li>Проверьте дату отчёта по товарам в пути.</li></ol><p>После загрузки будут показаны результаты проверки и сопоставления товаров.</p></section>
      {canDemo && <section className="demo-card"><h2>{mode === 'demo' ? 'Тестовый набор' : 'Демонстрационный набор'}</h2><p>{mode === 'demo' ? '16 синтетических товаров SystemElectric. Можно пройти весь сценарий до CSV.' : 'Загрузите демонстрационный набор, подготовленный сервером.'}</p><button className="btn btn-secondary" disabled={loading} onClick={() => { setLastAction('demo'); onDemo() }}>Использовать демо-данные <ArrowRight size={16} /></button></section>}
    </aside>
  </div>
}
