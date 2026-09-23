import { useRef, useState } from 'react'
import { Check, FileSpreadsheet, FolderOpen, LoaderCircle, Upload, X } from 'lucide-react'
import type { FileKind, ImportFiles, Supplier } from '../api/types'
import { maxFileBytes } from '../api/imports'
import { ErrorNotice } from './DataWarnings'

const slots: { key: FileKind; title: string; description: string; number: string }[] = [
  { key: 'moqFile', title: 'MOQ и кратность', description: 'Минимальные партии поставщика', number: '01' },
  { key: 'detailedSalesFile', title: 'Детальные продажи', description: 'История отгрузок по документам', number: '02' },
  { key: 'monthlyStockFile', title: 'Ежемесячные остатки', description: 'История наличия товаров на складе', number: '03' },
  { key: 'monthlySalesFile', title: 'Ежемесячные продажи', description: 'Объём продаж в разрезе месяцев', number: '04' },
  { key: 'seasonalityFile', title: 'Сезонность', description: 'Сезонные коэффициенты спроса', number: '05' },
  { key: 'inventoryTransitFile', title: 'Товар в пути', description: 'Ожидаемые поставки и даты прихода', number: '06' },
]
const emptyFiles: ImportFiles = { moqFile: null, detailedSalesFile: null, monthlyStockFile: null, monthlySalesFile: null, seasonalityFile: null, inventoryTransitFile: null }
function FileSlot({ slot, file, loading, onChange }: { slot: typeof slots[number]; file: File | null; loading: boolean; onChange: (file: File | null) => void }) {
  const input = useRef<HTMLInputElement>(null)
  const [dragging, setDragging] = useState(false)
  const [error, setError] = useState('')
  function accept(files: FileList | null) {
    if (loading || !files?.length) return
    if (files.length > 1) { setError('Выберите один файл для этого источника.'); return }
    const value = files[0]
    if (!/\.xlsx$/i.test(value.name)) { setError('Нужен файл Excel в формате .xlsx.'); return }
    if (value.size > maxFileBytes) { setError('Файл превышает 20 MiB.'); return }
    if (value.size === 0) { setError('Файл пустой. Выберите другой файл.'); return }
    setError(''); onChange(value)
  }
  return <div className={`file-slot ${file ? 'has-file' : ''} ${dragging ? 'dragging' : ''} ${error ? 'file-error' : ''}`} onDragOver={(e) => { e.preventDefault(); if (!loading) setDragging(true) }} onDragLeave={() => setDragging(false)} onDrop={(e) => { e.preventDefault(); setDragging(false); accept(e.dataTransfer.files) }}>
    <span className="slot-number">{slot.number}</span><div className="slot-info"><h3>{slot.title}</h3><p className="slot-description">{slot.description}</p></div>
    <input ref={input} type="file" accept=".xlsx" className="sr-only" aria-label={`Файл: ${slot.title}`} disabled={loading} onChange={(e) => { accept(e.target.files); e.target.value = '' }} />
    {file ? <div className="selected-file"><FileSpreadsheet size={16} /><div><strong title={file.name}>{file.name}</strong><span>{file.size < 1024 * 1024 ? `${Math.ceil(file.size / 1024)} КБ` : `${(file.size / (1024 * 1024)).toFixed(1)} МБ`}</span></div><button className="icon-button" disabled={loading} aria-label={`Удалить ${file.name}`} onClick={() => { onChange(null); setError('') }}><X size={15} /></button></div> : <button className="file-picker" disabled={loading} onClick={() => input.current?.click()}><Upload size={16} /> Выбрать Excel <span>или перетащить сюда</span></button>}
    <div className={`file-state ${error ? 'state-error' : file ? 'state-ready' : ''}`} role={error ? 'alert' : undefined}>{error ? error : loading && file ? <><LoaderCircle size={12} className="spin" /> Загружается</> : file ? <><Check size={13} /> Готов</> : 'Не выбран'}</div>
  </div>
}
interface Props { loading: boolean; progress: number | null; error: string | null; onImport: (files: ImportFiles | null, supplier: Supplier) => void }
export default function FileImportPanel({ loading, progress, error, onImport }: Props) {
  const [files, setFiles] = useState<ImportFiles>(emptyFiles)
  const [supplier, setSupplier] = useState<Supplier>('SystemElectric')
  const [lastSource, setLastSource] = useState<'local' | 'files'>('local')
  const count = Object.values(files).filter(Boolean).length
  function start(source: 'local' | 'files') { setLastSource(source); onImport(source === 'local' ? null : files, supplier) }
  return <div className="import-layout animate-in">
    <section className="panel import-panel">
      <div className="panel-heading"><div><h2>Источники данных</h2><p>Шесть Excel-файлов одного поставщика</p></div><span className="soft-badge">Выбрано {count} из 6</span></div>
      <label className="field supplier-picker">Поставщик<select aria-label="Поставщик" value={supplier} disabled={loading} onChange={(e) => setSupplier(e.target.value as Supplier)}><option value="SystemElectric">SystemElectric</option><option value="IEK">IEK</option></select></label>
      <div className="local-source"><div><h3>Набор {supplier} на сервере</h3><p>Шесть Excel-файлов из {supplier === 'IEK' ? 'data/demo/iek' : 'data/demo/system_electric (или electric_system)'}. Каждый поставщик анализируется отдельно.</p></div><button className="btn btn-primary" disabled={loading} onClick={() => start('local')}><FolderOpen size={17} /> Загрузить из data/demo</button></div>
      <p className="upload-alternative">Или выберите свои шесть файлов ниже</p>
      <div className="file-grid">{slots.map((slot) => <FileSlot key={slot.key} slot={slot} file={files[slot.key]} loading={loading} onChange={(file) => setFiles((current) => ({ ...current, [slot.key]: file }))} />)}</div>
      <div className="import-foot"><p>.xlsx · до 20 MiB на файл · 6 обязательных файлов</p><button className="btn btn-secondary" disabled={count !== 6 || loading} onClick={() => start('files')}>{loading ? <LoaderCircle size={17} className="spin" /> : <Upload size={17} />} Загрузить и проверить</button></div>
      {loading && <div className="upload-state" role="status">{progress !== null ? <><progress max="100" value={progress} aria-label="Отправка файлов" /><span>{progress === 100 ? 'Файлы переданы. Проверяем данные…' : `Отправлено ${progress}%`}</span></> : <><div className="indeterminate" /><span>Загрузка и проверка данных…</span></>}</div>}
      <ErrorNotice message={error} onRetry={!loading && (lastSource === 'local' || count === 6) ? () => start(lastSource) : undefined} />
    </section>
    <aside className="import-aside"><section className="source-note"><h2>Перед загрузкой</h2><ol><li>Соберите отчёты одного поставщика за один период.</li><li>Сохраните исходные названия колонок и листов.</li><li>Проверьте дату отчёта по товарам в пути.</li></ol><p>После загрузки будут показаны результаты проверки и сопоставления товаров. Пустые значения не заменяются нулями.</p></section></aside>
  </div>
}
