import { useEffect, useRef, useState } from 'react'
import { ArrowRight, Check, CheckCircle2, LoaderCircle, X } from 'lucide-react'
import type { ItemPatch, RecommendationItem } from '../api/types'
import { decisionLabels, formatMoney, formatNumber, quantityError, quantityWarning } from '../lib/format'
import AnomalyReview from './AnomalyReview'
import { DataWarnings, ErrorNotice } from './DataWarnings'

interface Props { item: RecommendationItem; busy: boolean; error: string | null; saved: boolean; onClose: () => void; onSave: (patch: ItemPatch) => void; onRetry: () => void }
export default function RecommendationDrawer({ item, busy, error, saved, onClose, onSave, onRetry }: Props) {
  const dialog = useRef<HTMLDialogElement>(null)
  const [section, setSection] = useState<'overview' | 'calculation' | 'decision'>('overview')
  const [quantity, setQuantity] = useState(String(item.approvedQuantity ?? item.recommendedQuantity))
  const [comment, setComment] = useState(item.managerComment || '')
  useEffect(() => {
    const element = dialog.current; element?.showModal()
    const previous = document.body.style.overflow; document.body.style.overflow = 'hidden'
    return () => { element?.close(); document.body.style.overflow = previous }
  }, [])
  const invalid = quantityError(quantity)
  const b = item.calculation
  const warning = quantityWarning(quantity, b.moq ?? null, b.moq ?? null)
  const dirty = quantity !== String(item.approvedQuantity ?? item.recommendedQuantity) || comment !== item.managerComment
  const lines: [string, number | null | undefined][] = [
    ['Базовый месячный спрос', b.baseMonthlyDemand], ['Исключено аномальных продаж', b.anomalyExcludedQuantity],
    ['Компенсация отсутствия товара', b.stockoutCompensation], ['Скорректированный спрос', b.correctedMonthlyDemand],
    ['Коэффициент сезонности', b.seasonalityFactor], ['Коэффициент роста', b.growthFactor],
    ['Горизонт прогноза, месяцев', b.forecastHorizonMonths], ['Прогноз спроса', b.forecastDemand],
    ['Страховой запас', b.safetyStock], ['Свободный остаток', b.freeStock], ['Товар в пути', b.inTransit],
    ['Доступный запас', b.availableStock], ['Исходная потребность', b.rawRequirement], ['Кратность заказа', b.moq],
    ['Округлённая потребность', b.roundedRequirement],
  ]
  return <dialog ref={dialog} className="detail-drawer" aria-labelledby="drawer-title" onCancel={(e) => { e.preventDefault(); if (!busy) onClose() }} onClick={(e) => { if (e.target === e.currentTarget && !busy) onClose() }}><div className="drawer-surface">
    <header className="drawer-header"><div><span className="eyebrow">{item.supplier}</span><h2 id="drawer-title">{item.name || item.code1C}</h2><p className="product-identifiers"><span>1С: {item.code1C}</span><span>{item.article}</span></p></div><button className="icon-button" disabled={busy} onClick={onClose} aria-label="Закрыть карточку"><X size={21} /></button></header>
    <nav className="drawer-tabs" aria-label="Разделы карточки">{([['overview', 'Объяснение'], ['calculation', 'Расчёт'], ['decision', 'Решение менеджера']] as const).map(([id, label]) => <button key={id} aria-current={section === id ? 'page' : undefined} onClick={() => setSection(id)}>{label}</button>)}</nav>
    <div className="drawer-content"><section className="drawer-product"><div className="product-decision"><span className={`decision decision-${item.decision.toLowerCase()}`}>{decisionLabels[item.decision]}</span><span>{item.approved ? item.approvedQuantity === 0 ? 'Подтверждён отказ от заказа' : 'Количество подтверждено' : 'Количество не подтверждено'}</span></div><div className="drawer-product-values"><div><span>Рекомендация</span><strong>{formatNumber(item.recommendedQuantity)} <small>ед.</small></strong></div><div><span>Ориентировочная стоимость</span><strong>{formatMoney(item.estimatedCost)}</strong></div></div></section>
    <DataWarnings warnings={item.warnings} compact />
    {section === 'calculation' && <section className="drawer-section"><h3>Расчёт потребности</h3>{b.historyPeriod && <p>История: {b.historyPeriod.from} — {b.historyPeriod.to}</p>}<dl className="breakdown">{lines.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{formatNumber(value)}</dd></div>)}<div className="breakdown-total"><dt>Рекомендуемый заказ</dt><dd>{formatNumber(item.recommendedQuantity)} ед.</dd></div></dl></section>}
    {section === 'overview' && <><section className="explanation-card"><h3>Обоснование решения</h3>{item.explanation ? <><p>{item.explanation.short}</p><ul>{item.explanation.details?.map((reason, index) => <li key={index}><Check size={14} />{reason}</li>)}</ul><small>{item.explanation.generatedBy === 'OPENAI' ? 'AI-объяснение' : 'Объяснение сервиса'}</small></> : <p>Сервис не вернул объяснение. Числа доступны в разделе «Расчёт».</p>}</section><AnomalyReview anomalies={item.anomalyAnalysis?.candidates ?? []} decision={item.anomalyDecision} busy={busy} onDecision={(anomalyDecision) => onSave({ anomalyDecision })} /></>}
    {section === 'decision' && <section className="drawer-section edit-section"><h3>Решение менеджера</h3><label className="field">Количество для заказа<div className="number-field"><input inputMode="numeric" aria-label="Количество для заказа" value={quantity} onChange={(e) => setQuantity(e.target.value)} disabled={busy} aria-invalid={Boolean(invalid)} /><span>ед.</span></div></label><p className="input-hint">Рекомендация: {formatNumber(item.recommendedQuantity)}. Ноль означает отказ от заказа.</p>{invalid && <p className="field-error" role="alert">{invalid}</p>}{warning && <div className="notice notice-warning">{warning}</div>}<label className="field mt-4">Комментарий менеджера<textarea rows={3} maxLength={1000} value={comment} disabled={busy} onChange={(e) => setComment(e.target.value)} placeholder="Основание для решения" /></label>{item.requiresManualReview && <p className="notice notice-warning">Позиция требует проверки. Подтверждайте количество после проверки замечаний.</p>}<div className="decision-secondary-actions"><button className="btn btn-secondary" disabled={busy || Boolean(invalid)} onClick={() => onSave({ approvedQuantity: Number(quantity), approved: false, comment })}>Сохранить без подтверждения</button>{item.approved && <button className="text-button" disabled={busy} onClick={() => onSave({ approved: false })}>Отменить подтверждение</button>}</div></section>}
    </div><footer className="drawer-footer"><ErrorNotice message={error} onRetry={busy ? undefined : onRetry} /><div className="drawer-footer-actions"><span role="status">{busy ? 'Сохраняем решение…' : dirty ? 'Есть несохранённые изменения' : saved ? <><CheckCircle2 size={16} /> Решение сохранено</> : 'Экспорт требует явного подтверждения'}</span>{section === 'decision' ? <button className="btn btn-primary" disabled={busy || Boolean(invalid)} onClick={() => onSave({ approvedQuantity: Number(quantity), approved: true, comment })}>{busy ? <LoaderCircle size={16} className="spin" /> : <Check size={16} />} Подтвердить количество</button> : <button className="btn btn-primary" onClick={() => setSection('decision')}>К решению менеджера <ArrowRight size={16} /></button>}</div></footer>
  </div></dialog>
}
