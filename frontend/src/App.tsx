import { useEffect, useMemo, useRef, useState } from 'react'
import { ArrowLeft, ArrowRight, Check, CheckCircle2, Download, FileCheck2, HelpCircle, Settings2, X } from 'lucide-react'
import type { ExportResult, ImportFiles, ImportResult, ItemPatch, RunParams, RunResult, Stage, Supplier } from './api/types'
import { importFiles, importLocal } from './api/imports'
import { createRecommendations, downloadExport, exportOrder, getRecommendations, patchItem } from './api/recommendations'
import { formatDate, formatNumber, sortItems } from './lib/format'
import FileImportPanel from './components/FileImportPanel'
import CalculationSettings from './components/CalculationSettings'
import SummaryCards from './components/SummaryCards'
import RecommendationFilters from './components/RecommendationFilters'
import { initialFilters } from './lib/filters'
import type { Filters } from './lib/filters'
import RecommendationTable from './components/RecommendationTable'
import RecommendationDrawer from './components/RecommendationDrawer'
import ExportBar from './components/ExportBar'
import { DataWarnings, ErrorNotice } from './components/DataWarnings'
import ErrorBoundary from './components/ErrorBoundary'
import StagePrerequisite from './components/StagePrerequisite'
import { defaultRunParams, validRunParams } from './lib/workflow'

const steps: { id: Stage; label: string }[] = [
  { id: 'IMPORT', label: 'Импорт данных' }, { id: 'CONFIGURE', label: 'Параметры' },
  { id: 'RESULTS', label: 'Рекомендации' }, { id: 'APPROVED', label: 'Готовый заказ' },
]
const stageText: Record<Stage, { title: string; text: string }> = {
  IMPORT: { title: 'Импорт данных', text: 'Выберите набор поставщика на сервере или загрузите шесть отчётов.' },
  CONFIGURE: { title: 'Проверка данных и параметры', text: 'Проверьте результат импорта и укажите условия поставки.' },
  RESULTS: { title: 'План закупки', text: 'Проверьте позиции и подтвердите окончательное количество.' },
  APPROVED: { title: 'Подтверждённый заказ', text: 'В CSV включаются только явно подтверждённые количества больше нуля.' },
}
const errorMessage = (error: unknown) => error instanceof Error ? error.message : 'Произошла ошибка. Повторите запрос.'

function Workspace() {
  const [stage, setStage] = useState<Stage>('IMPORT')
  const [dataset, setDataset] = useState<ImportResult | null>(null)
  const [params, setParams] = useState<RunParams>(defaultRunParams)
  const [run, setRun] = useState<RunResult | null>(null)
  const [exported, setExported] = useState<ExportResult | null>(null)
  const [busy, setBusy] = useState(false)
  const requestLock = useRef(false)
  const [error, setError] = useState<string | null>(null)
  const [progress, setProgress] = useState<number | null>(null)
  const [filters, setFilters] = useState<Filters>(initialFilters)
  const [selectedCode, setSelectedCode] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)
  const [refreshRequired, setRefreshRequired] = useState(false)
  const [help, setHelp] = useState(false)
  useEffect(() => { window.scrollTo({ top: 0, left: 0, behavior: 'instant' }) }, [stage])
  const selected = run?.items.find((item) => item.code1C === selectedCode)
  const filtered = useMemo(() => sortItems(run?.items || []).filter((item) => {
    const query = filters.search.trim().toLocaleLowerCase('ru')
    return (!query || `${item.name} ${item.article} ${item.code1C}`.toLocaleLowerCase('ru').includes(query)) && (!filters.decision || item.decision === filters.decision) && (!filters.category || item.category === filters.category) && (!filters.urgency || item.urgency === filters.urgency) && (!filters.warningsOnly || item.warnings.length > 0)
  }), [run, filters])
  const completedSteps: Record<Stage, boolean> = { IMPORT: Boolean(dataset), CONFIGURE: Boolean(run), RESULTS: Boolean(run?.summary.approvedItems), APPROVED: Boolean(exported) }
  function begin() { if (requestLock.current) return false; requestLock.current = true; setBusy(true); return true }
  function finish() { requestLock.current = false; setBusy(false) }
  function navigate(next: Stage) { if (requestLock.current) return; setStage(next); setError(null); setSelectedCode(null) }
  async function upload(files: ImportFiles | null, supplier: Supplier) {
    if (!begin()) return
    setError(null); setProgress(null)
    try {
      const result = files ? await importFiles(files, supplier, setProgress) : await importLocal(supplier)
      setDataset(result); setParams(defaultRunParams()); setStage('CONFIGURE'); setRun(null); setExported(null); setSelectedCode(null); setRefreshRequired(false)
    } catch (caught) { setError(errorMessage(caught)) } finally { finish() }
  }
  async function calculate() {
    if (!dataset || !validRunParams(params) || !begin()) return
    setError(null)
    try { const result = await createRecommendations(dataset.importId, params); setRun(result); setExported(null); setStage('RESULTS'); setFilters(initialFilters); setSelectedCode(null); setRefreshRequired(false) }
    catch (caught) { setError(errorMessage(caught)) } finally { finish() }
  }
  async function refresh() {
    if (!run || !begin()) return
    try { setRun(await getRecommendations(run.runId)); setRefreshRequired(false); setSaveError(null); setError(null) }
    catch (caught) { setSaveError(errorMessage(caught)); setError(errorMessage(caught)) } finally { finish() }
  }
  async function save(patch: ItemPatch) {
    if (!run || !selectedCode || !begin()) return
    setSaveError(null); setSaved(false); setExported(null); setRefreshRequired(true)
    let persisted = false
    try {
      await patchItem(run.runId, selectedCode, patch); persisted = true
      setRun(await getRecommendations(run.runId)); setRefreshRequired(false); setSaved(true)
    } catch (caught) { setSaveError(`${persisted ? 'Решение сохранено, но список не обновился. ' : ''}${errorMessage(caught)} Обновите расчёт перед экспортом.`) } finally { finish() }
  }
  async function download() {
    if (!run || refreshRequired || run.orderSummary.positions === 0 || !begin()) return
    setError(null)
    try { const result = await exportOrder(run.runId); downloadExport(result); setExported(result); setStage('APPROVED') }
    catch (caught) { setError(errorMessage(caught)) } finally { finish() }
  }
  function reset() { setDataset(null); setRun(null); setParams(defaultRunParams()); setExported(null); setRefreshRequired(false); navigate('IMPORT') }
  const open = (code: string) => { setSelectedCode(code); setSaveError(null); setSaved(false) }
  return <div className="app-shell">
    <header className="topbar"><div className="brand-group"><span className="brand"><span className="brand-mark" aria-hidden="true"><i /><i /><i /></span>SupplyLens</span><span className="workspace-name">Закупки <span>/</span> Электрокомплект</span></div><button className="icon-button help-button" aria-label="Как работает SupplyLens" aria-expanded={help} onClick={() => setHelp((value) => !value)}><HelpCircle size={20} /></button></header>
    <nav className="workflow-nav" aria-label="Этапы заказа">{steps.map(({ id, label }, index) => <button key={id} className={`workflow-step ${stage === id ? 'active' : ''}`} disabled={busy} onClick={() => navigate(id)} aria-current={stage === id ? 'step' : undefined}><span className="step-number">{completedSteps[id] && stage !== id ? <Check size={14} /> : `0${index + 1}`}</span><span>{label}</span></button>)}</nav>
    <main className={`main-content stage-${stage.toLowerCase()}`}>
      <div className="page-heading"><div><h1>{stageText[stage].title}</h1><p>{stage === 'RESULTS' && run ? <>{run.supplier}<span className="text-divider">/</span>На {formatDate(run.asOf)}</> : stageText[stage].text}</p></div>{stage === 'RESULTS' && run && <button className="btn btn-secondary" disabled={busy} onClick={() => navigate('CONFIGURE')}><Settings2 size={16} /> Параметры расчёта</button>}</div>
      {stage === 'IMPORT' && <FileImportPanel loading={busy} error={error} progress={progress} onImport={(files, supplier) => void upload(files, supplier)} />}
      {stage !== 'IMPORT' && ((stage === 'CONFIGURE' && !dataset) || (stage !== 'CONFIGURE' && !run)) && <StagePrerequisite stage={stage} hasDataset={Boolean(dataset)} loading={busy} error={error} onImport={() => navigate('IMPORT')} onConfigure={() => navigate('CONFIGURE')} />}
      {stage === 'CONFIGURE' && dataset && <><CalculationSettings dataset={dataset} params={params} loading={busy} error={error} onChange={setParams} onRun={() => void calculate()} onBack={() => navigate('IMPORT')} />{run && <button className="text-button previous-result" disabled={busy} onClick={() => navigate('RESULTS')}><ArrowLeft size={15} /> Вернуться к предыдущему результату</button>}</>}
      {(stage === 'RESULTS' || stage === 'APPROVED') && run && refreshRequired && <div className="notice notice-warning">Результат мог измениться на сервере. Обновите данные перед экспортом.<button className="text-button" disabled={busy} onClick={() => void refresh()}>Обновить расчёт</button></div>}
      {stage === 'RESULTS' && run && <div className="results"><SummaryCards summary={run.summary} /><section className="panel results-panel" aria-label="Закупочные решения"><div className="results-title"><h2>Закупочные решения <span>{run.items.length}</span></h2><p>Сначала проверка, затем риск дефицита</p></div><RecommendationFilters value={filters} onChange={setFilters} categories={[...new Set(run.items.map((item) => item.category).filter(Boolean))].sort()} /><RecommendationTable items={filtered} total={run.items.length} selectedCode={selectedCode} onOpen={(item) => open(item.code1C)} onReset={() => setFilters(initialFilters)} /><div className="table-footer"><span>Показано {filtered.length} из {run.items.length} позиций</span><span>Нажмите на товар, чтобы проверить расчёт</span></div></section><DataWarnings warnings={run.warnings} compact /><ErrorNotice message={error} onRetry={busy ? undefined : () => void (refreshRequired ? refresh() : download())} /><ExportBar run={run} busy={busy || refreshRequired} onExport={() => void download()} /></div>}
      {stage === 'APPROVED' && run && !exported && <div className="results order-preview"><div className="order-preview-heading"><div><h2>{run.orderSummary.positions ? 'Подтверждённые позиции' : 'Подтверждённых позиций пока нет'}</h2><p>Рекомендация «Заказать» сама по себе не подтверждает количество.</p></div><button className="btn btn-secondary" disabled={busy} onClick={() => navigate('RESULTS')}><ArrowLeft size={16} /> К рекомендациям</button></div>{run.orderSummary.positions > 0 && <section className="panel results-panel"><RecommendationTable items={sortItems(run.items.filter((item) => item.approved && item.approvedQuantity !== null && item.approvedQuantity > 0))} total={run.orderSummary.positions} selectedCode={selectedCode} onOpen={(item) => open(item.code1C)} onReset={() => navigate('RESULTS')} /></section>}<ErrorNotice message={error} onRetry={busy ? undefined : () => void download()} /><ExportBar run={run} busy={busy || refreshRequired} onExport={() => void download()} /></div>}
      {stage === 'APPROVED' && run && exported && <section className="panel approved-panel"><div className="approved-heading"><CheckCircle2 size={26} /><div><h2>Заказ {run.supplier} сформирован</h2><p>Экспорт завершён</p></div></div><dl className="approved-totals"><div><dt>Позиций в заказе</dt><dd>{formatNumber(run.orderSummary.positions)}</dd></div><div><dt>Всего единиц</dt><dd>{formatNumber(run.orderSummary.totalUnits)}</dd></div></dl><div className="exported-file"><FileCheck2 size={25} /><div><strong>{exported.filename}</strong><span>CSV · готов к скачиванию</span></div><button className="icon-button" aria-label="Скачать CSV ещё раз" onClick={() => downloadExport(exported)}><Download size={19} /></button></div><p className="approved-note">Заказ не отправлялся поставщику. Неподтверждённые позиции не включены.</p><div className="approved-actions"><button className="btn btn-secondary" onClick={() => navigate('RESULTS')}><ArrowLeft size={16} /> К рекомендациям</button><button className="btn btn-primary" onClick={reset}>Новый расчёт <ArrowRight size={16} /></button></div></section>}
      <footer className="page-footer"><span>Электрокомплект · SupplyLens</span><span>Решение о заказе принимает менеджер</span></footer>
    </main>
    {selected && (stage === 'RESULTS' || (stage === 'APPROVED' && !exported)) && <RecommendationDrawer key={`${selected.code1C}-${selected.updatedAt}`} item={selected} busy={busy} error={saveError} saved={saved} onClose={() => setSelectedCode(null)} onSave={(patch) => void save(patch)} onRetry={() => void refresh()} />}
    {help && <div className="help-popover" role="region" aria-label="Как работает SupplyLens"><button className="icon-button" onClick={() => setHelp(false)} aria-label="Закрыть справку"><X size={18} /></button><h3>Отчёты → решение → заказ</h3><ol><li>Загрузите шесть файлов одного поставщика.</li><li>Проверьте данные и укажите условия поставки.</li><li>Проверьте рекомендации и подтвердите количества.</li><li>Скачайте CSV подтверждённого заказа.</li></ol><p>Данные и подтверждения хранятся в памяти. После перезапуска backend потребуется повторная загрузка.</p></div>}
  </div>
}
export default function App() { return <ErrorBoundary><Workspace /></ErrorBoundary> }
