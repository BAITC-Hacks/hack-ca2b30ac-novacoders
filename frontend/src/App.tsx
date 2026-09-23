import { useEffect, useMemo, useRef, useState } from 'react'
import { ArrowLeft, ArrowRight, Check, CheckCircle2, Download, FileCheck2, HelpCircle, PlugZap, RotateCcw, Settings2, X } from 'lucide-react'
import type { Dataset, ExportResult, ImportFiles, ItemPatch, Mode, RunParams, RunResult, Stage } from './types/api'
import { downloadExport, getApi } from './api/supplyLensApi'
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
import { defaultRunParams, prepareDemoStage } from './lib/workflow'

const steps: { id: Stage; label: string }[] = [
  { id: 'IMPORT', label: 'Импорт данных' }, { id: 'CONFIGURE', label: 'Параметры' },
  { id: 'RESULTS', label: 'Рекомендации' }, { id: 'APPROVED', label: 'Готовый заказ' },
]
const stageText: Record<Stage, { title: string; text: string }> = {
  IMPORT: { title: 'Импорт данных', text: 'Добавьте шесть отчётов одного поставщика для расчёта пополнения.' },
  CONFIGURE: { title: 'Параметры пополнения', text: 'Укажите срок поставки, страховой запас и доступные остатки.' },
  RESULTS: { title: 'План закупки', text: 'Проверьте спорные позиции перед подтверждением заказа.' },
  APPROVED: { title: 'Готовый заказ', text: 'Проверьте состав заказа и подтвердите выгрузку CSV.' },
}
const errorMessage = (error: unknown) => error instanceof Error ? error.message : 'Произошла ошибка. Попробуйте ещё раз.'

function ConfirmMode({ onClose, onConfirm }: { onClose: () => void; onConfirm: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null)
  useEffect(() => { const element = dialog.current; element?.showModal(); return () => element?.close() }, [])
  return <dialog ref={dialog} className="confirm-dialog" aria-labelledby="mode-title" onCancel={(event) => { event.preventDefault(); onClose() }}><PlugZap size={27} /><h2 id="mode-title">Сменить режим работы?</h2><p>Текущий набор и решения будут сброшены. Уже скачанные файлы сохранятся.</p><div className="flex justify-end gap-3"><button className="btn btn-secondary" autoFocus onClick={onClose}>Остаться</button><button className="btn btn-primary" onClick={onConfirm}><RotateCcw size={16} /> Сменить режим</button></div></dialog>
}

function Workspace() {
  const [mode, setMode] = useState<Mode>('demo')
  const [stage, setStage] = useState<Stage>('IMPORT')
  const [dataset, setDataset] = useState<Dataset | null>(null)
  const [params, setParams] = useState<RunParams | null>(null)
  const [run, setRun] = useState<RunResult | null>(null)
  const [exported, setExported] = useState<ExportResult | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [progress, setProgress] = useState<number | null>(null)
  const [filters, setFilters] = useState<Filters>(initialFilters)
  const [selectedCode, setSelectedCode] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)
  const [help, setHelp] = useState(false)
  const [confirmMode, setConfirmMode] = useState<Mode | null>(null)
  useEffect(() => { window.scrollTo({ top: 0, left: 0, behavior: 'instant' }) }, [stage])
  const api = getApi(mode)
  const capabilities = dataset?.capabilities || api.capabilities
  const selected = run?.items.find((item) => item.code1C === selectedCode)
  const filtered = useMemo(() => sortItems(run?.items || []).filter((item) => {
    const query = filters.search.trim().toLocaleLowerCase('ru')
    return (!query || `${item.name} ${item.article} ${item.code1C}`.toLocaleLowerCase('ru').includes(query)) && (!filters.decision || item.decision === filters.decision) && (!filters.category || item.category === filters.category) && (!filters.urgency || item.urgency === filters.urgency) && (!filters.warningsOnly || item.warnings.length > 0)
  }), [run, filters])
  const completedSteps: Record<Stage, boolean> = { IMPORT: Boolean(dataset), CONFIGURE: Boolean(run), RESULTS: Boolean(exported), APPROVED: Boolean(exported) }
  function switchMode(next: Mode) {
    setMode(next); setStage('IMPORT'); setDataset(null); setParams(null); setRun(null); setExported(null); setError(null); setSelectedCode(null); setFilters(initialFilters); setConfirmMode(null)
  }
  function requestMode(next: Mode) {
    if (mode === next) return
    if (dataset) setConfirmMode(next)
    else switchMode(next)
  }
  async function importData(files?: ImportFiles) {
    if (busy) return
    setBusy(true); setError(null); setProgress(null)
    try {
      const result = files ? await api.importFiles(files, setProgress) : await api.loadDemoDataset()
      setDataset(result)
      setParams(defaultRunParams(result, api.capabilities))
      setStage('CONFIGURE'); setRun(null); setExported(null)
    } catch (caught) { setError(errorMessage(caught)) } finally { setBusy(false) }
  }
  async function calculate() {
    if (busy || !params) return
    setBusy(true); setError(null)
    try { const result = await api.createRun(params); setRun(result); setExported(null); setStage('RESULTS'); setFilters(initialFilters) }
    catch (caught) { setError(errorMessage(caught)) } finally { setBusy(false) }
  }
  async function patchItem(patch: ItemPatch) {
    if (!run || !selectedCode || busy) return
    setBusy(true); setSaveError(null); setSaved(false)
    try { const result = await api.patchItem(run.runId, selectedCode, patch); setRun(result); setExported(null); setSaved(true) }
    catch (caught) { setSaveError(errorMessage(caught)) } finally { setBusy(false) }
  }
  async function exportOrder() {
    if (!run || busy) return
    setBusy(true); setError(null)
    try { const result = await api.approveExport(run.runId); setExported(result); downloadExport(result); setStage('APPROVED') }
    catch (caught) { setError(errorMessage(caught)) } finally { setBusy(false) }
  }
  async function navigate(next: Stage) {
    if (busy) return
    setStage(next); setError(null); setSelectedCode(null)
    if (mode !== 'demo' || next === 'IMPORT') return
    if (dataset && params && (next === 'CONFIGURE' || run)) return
    setBusy(true)
    try {
      const prepared = await prepareDemoStage(api, next, { dataset, params, run })
      setDataset(prepared.dataset); setParams(prepared.params); setRun(prepared.run)
      if (prepared.run !== run) { setExported(null); setFilters(initialFilters) }
    } catch (caught) { setError(errorMessage(caught)) } finally { setBusy(false) }
  }
  return <div className="app-shell">
    <header className="topbar">
      <div className="brand-group"><a className="brand" href="#" onClick={(event) => event.preventDefault()} aria-label="SupplyLens"><span className="brand-mark" aria-hidden="true"><i /><i /><i /></span>SupplyLens</a><span className="workspace-name">Закупки <span>/</span> Электрокомплект</span></div>
      <div className="topbar-actions"><div className="mode-switch" aria-label="Режим работы"><button disabled={busy} aria-pressed={mode === 'demo'} onClick={() => requestMode('demo')}>Демо</button><button disabled={busy} aria-pressed={mode === 'api'} onClick={() => requestMode('api')}>Go API</button></div><button className="icon-button help-button" aria-label="Как работает SupplyLens" aria-expanded={help} onClick={() => setHelp((value) => !value)}><HelpCircle size={20} /></button></div>
    </header>
    <nav className="workflow-nav" aria-label="Этапы заказа">{steps.map(({ id, label }, index) => <button key={id} className={`workflow-step ${stage === id ? 'active' : ''}`} disabled={busy} onClick={() => navigate(id)} aria-current={stage === id ? 'step' : undefined}><span className="step-number">{completedSteps[id] && stage !== id ? <Check size={14} /> : `0${index + 1}`}</span><span>{label}</span></button>)}</nav>
    <main className={`main-content stage-${stage.toLowerCase()}`}>
      <div className="page-heading"><div><h1>{stageText[stage].title}</h1><p>{stage === 'RESULTS' && run ? <>{run.supplier}<span className="text-divider">/</span>На {formatDate(run.asOf)}</> : stageText[stage].text}</p></div>{stage === 'RESULTS' && run && <button className="btn btn-secondary" disabled={busy} onClick={() => navigate('CONFIGURE')}><Settings2 size={16} /> Параметры расчёта</button>}</div>
      {mode === 'demo' && <div className="demo-notice"><span className="demo-pill">Демо</span><span>Синтетические данные. {stage === 'IMPORT' ? 'Можно сразу открыть любой раздел — пример загрузится автоматически.' : 'Расчёт и AI-анализ имитируются; параметры не меняют прогноз.'}</span></div>}
      {stage === 'IMPORT' && <FileImportPanel mode={mode} canDemo={api.capabilities.demoDataset} loading={busy} error={error} progress={progress} onImport={(files) => void importData(files)} onDemo={() => void importData()} />}
      {stage !== 'IMPORT' && ((stage === 'CONFIGURE' && (!dataset || !params)) || (stage !== 'CONFIGURE' && !run)) && <StagePrerequisite stage={stage} hasDataset={Boolean(dataset && params)} loading={busy} error={error} onImport={() => void navigate('IMPORT')} onConfigure={() => void navigate('CONFIGURE')} onRetry={mode === 'demo' ? () => void navigate(stage) : undefined} />}
      {stage === 'CONFIGURE' && dataset && params && <><CalculationSettings dataset={dataset} params={params} capabilities={capabilities} mode={mode} loading={busy} error={error} onChange={setParams} onRun={() => void calculate()} onBack={() => navigate('IMPORT')} />{run && <button className="text-button previous-result" disabled={busy} onClick={() => navigate('RESULTS')}><ArrowLeft size={15} /> Вернуться к предыдущему результату</button>}</>}
      {stage === 'RESULTS' && run && <div className="results"><SummaryCards summary={run.summary} /><section className="panel results-panel" aria-label="Закупочные решения"><div className="results-title"><h2>Закупочные решения <span>{run.items.length}</span></h2><p>Сначала проверка, затем риск дефицита</p></div><RecommendationFilters value={filters} onChange={setFilters} categories={[...new Set(run.items.map((item) => item.category))].sort()} /><RecommendationTable items={filtered} total={run.items.length} selectedCode={selectedCode} onOpen={(item) => { setSelectedCode(item.code1C); setSaveError(null); setSaved(false) }} onReset={() => setFilters(initialFilters)} /><div className="table-footer"><span>Показано {filtered.length} из {run.items.length} позиций</span><span>Нажмите на товар, чтобы проверить расчёт</span></div></section><DataWarnings warnings={run.warnings} compact /><ErrorNotice message={error} onRetry={busy ? undefined : () => void exportOrder()} /><ExportBar run={run} busy={busy} onExport={() => void exportOrder()} /></div>}
      {stage === 'APPROVED' && run && !exported && <div className="results order-preview"><div className="order-preview-heading"><div><h2>{run.exportReady && run.summary.buy > 0 ? 'Заказ готов к подтверждению' : 'Заказ требует проверки'}</h2><p>{run.supplier} · В CSV войдут только позиции с решением «Заказать».{run.summary.review > 0 && ` На проверке: ${run.summary.review}.`}</p></div><button className="btn btn-secondary" disabled={busy} onClick={() => void navigate('RESULTS')}><ArrowLeft size={16} /> Проверить рекомендации</button></div><SummaryCards summary={run.summary} />{run.summary.buy > 0 && <section className="panel results-panel"><div className="results-title"><h2>Состав заказа</h2></div><RecommendationTable items={sortItems(run.items.filter((item) => item.decision === 'BUY'))} total={run.summary.buy} selectedCode={selectedCode} onOpen={(item) => { setSelectedCode(item.code1C); setSaveError(null); setSaved(false) }} onReset={() => undefined} /></section>}<DataWarnings warnings={run.warnings} compact /><ErrorNotice message={error} onRetry={busy ? undefined : () => void exportOrder()} /><ExportBar run={run} busy={busy} onExport={() => void exportOrder()} /></div>}
      {stage === 'APPROVED' && run && exported && <section className="panel approved-panel"><div className="approved-heading"><CheckCircle2 size={26} /><div><h2>Заказ {run.supplier} сформирован</h2><p>{mode === 'demo' ? 'Демонстрационный CSV' : 'Экспорт завершён'}</p></div></div><dl className="approved-totals"><div><dt>Позиций в заказе</dt><dd>{formatNumber(run.summary.buy)}</dd></div><div><dt>Всего единиц</dt><dd>{formatNumber(run.summary.totalUnits)}</dd></div><div><dt>Осталось на проверке</dt><dd>{formatNumber(run.summary.review)}</dd></div></dl><div className="exported-file"><FileCheck2 size={25} /><div><strong>{exported.filename}</strong><span>CSV · готов к скачиванию</span></div><button className="icon-button" aria-label="Скачать CSV ещё раз" onClick={() => downloadExport(exported)}><Download size={19} /></button></div><p className="approved-note">Заказ не отправлялся поставщику.{mode === 'demo' && ' Демо-файл не предназначен для закупки.'}{run.summary.review > 0 && ` Позиции на проверке (${run.summary.review}) не включены.`}</p><div className="approved-actions"><button className="btn btn-secondary" onClick={() => navigate('RESULTS')}><ArrowLeft size={16} /> К рекомендациям</button><button className="btn btn-primary" onClick={() => { setDataset(null); setRun(null); setParams(null); setExported(null); navigate('IMPORT') }}>Новый расчёт <ArrowRight size={16} /></button></div></section>}
      <footer className="page-footer"><span>Электрокомплект · SupplyLens</span><span>{mode === 'demo' ? 'Демонстрационный режим' : 'Go API / v1'}</span></footer>
    </main>
    {selected && (stage === 'RESULTS' || (stage === 'APPROVED' && !exported)) && <RecommendationDrawer item={selected} mode={mode} busy={busy} error={saveError} saved={saved} onClose={() => setSelectedCode(null)} onSave={(patch) => void patchItem(patch)} />}
    {help && <div className="help-popover" role="region" aria-label="Как работает SupplyLens"><button className="icon-button" onClick={() => setHelp(false)} aria-label="Закрыть справку"><X size={18} /></button><h3>Отчёты → решение → заказ</h3><ol><li>Загрузите шесть файлов одного поставщика.</li><li>Укажите срок поставки и страховой запас.</li><li>Проверьте рекомендации и спорные операции.</li><li>Подтвердите готовые позиции и скачайте CSV.</li></ol><p>SupplyLens не отправляет заказ поставщику. В режиме Go API все расчёты выполняет сервер.</p></div>}
    {confirmMode && <ConfirmMode onClose={() => setConfirmMode(null)} onConfirm={() => switchMode(confirmMode)} />}
  </div>
}
export default function App() { return <ErrorBoundary><Workspace /></ErrorBoundary> }
