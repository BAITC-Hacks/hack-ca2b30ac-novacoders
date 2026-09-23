import { useEffect, useRef, useState } from 'react'
import { ArrowRight, Check, CheckCircle2, LoaderCircle, PencilLine, X } from 'lucide-react'
import type { ItemPatch, Mode, RecommendationItem } from '../types/api'
import { decisionLabels, formatMoney, formatNumber, quantityError, quantityWarning } from '../lib/format'
import DemandChart from './DemandChart'
import AnomalyReview from './AnomalyReview'
import { DataWarnings, ErrorNotice } from './DataWarnings'

interface Props { item: RecommendationItem; mode: Mode; busy: boolean; error: string | null; saved: boolean; onClose: () => void; onSave: (patch: ItemPatch) => void }
export default function RecommendationDrawer({ item, mode, busy, error, saved, onClose, onSave }: Props) {
  const dialog = useRef<HTMLDialogElement>(null)
  const content = useRef<HTMLDivElement>(null)
  const [section, setSection] = useState<'overview' | 'calculation' | 'decision'>('overview')
  const [quantity, setQuantity] = useState(String(item.approvedQuantity ?? item.recommendedQuantity))
  const [comment, setComment] = useState(item.managerComment || '')
  const [lastPatch, setLastPatch] = useState<ItemPatch | null>(null)
  useEffect(() => { const element = dialog.current; element?.showModal(); const original = document.body.style.overflow; document.body.style.overflow = 'hidden'; return () => { element?.close(); document.body.style.overflow = original } }, [])
  useEffect(() => { setQuantity(String(item.approvedQuantity ?? item.recommendedQuantity)); setComment(item.managerComment || '') }, [item])
  useEffect(() => { content.current?.scrollTo({ top: 0, behavior: 'instant' }) }, [section])
  const invalid = quantityError(quantity)
  const warning = quantityWarning(quantity, item.breakdown.moq, item.breakdown.orderMultiple)
  const b = item.breakdown
  const dirty = quantity !== String(item.approvedQuantity ?? item.recommendedQuantity) || comment !== (item.managerComment || '')
  const lines: [string, string][] = [
    ['Базовый спрос', formatNumber(b.baseDemand)],
    ['Сезонность', `× ${formatNumber(b.seasonalityFactor)}`],
    ['Рост', `× ${formatNumber(b.growthFactor)}`],
    ['Компенсация stockout', `+ ${formatNumber(b.stockoutCompensation)}`],
    ['Страховой запас', `+ ${formatNumber(b.safetyStock)}`],
    ['Свободный остаток', `− ${formatNumber(b.freeStock)}`],
    ['Товар в пути', `− ${formatNumber(b.inTransit)}`],
  ]
  function save(patch: ItemPatch) { setLastPatch(patch); onSave(patch) }
  return <dialog ref={dialog} className="detail-drawer" aria-labelledby="drawer-title" onCancel={(event) => { event.preventDefault(); if (!busy) onClose() }} onClick={(event) => { if (event.target === event.currentTarget && !busy) onClose() }}><div className="drawer-surface">
    <header className="drawer-header"><div><span className="eyebrow">{item.supplier}{mode === 'demo' && ' / Демо'}</span><h2 id="drawer-title">{item.name}</h2><p className="product-identifiers"><span>1С: {item.code1C}</span><span>{item.article}</span></p></div><button className="icon-button" disabled={busy} onClick={onClose} aria-label="Закрыть карточку"><X size={21} /></button></header>
    <nav className="drawer-tabs" aria-label="Разделы карточки"><button aria-current={section === 'overview' ? 'page' : undefined} onClick={() => setSection('overview')}>Обзор спроса</button><button aria-current={section === 'calculation' ? 'page' : undefined} onClick={() => setSection('calculation')}>Расчёт</button><button aria-current={section === 'decision' ? 'page' : undefined} onClick={() => setSection('decision')}>Решение менеджера</button></nav>
    <div ref={content} className="drawer-content"><section className="drawer-product"><div className="product-decision"><span className={`decision decision-${item.decision.toLowerCase()}`}>{decisionLabels[item.decision]}</span>{item.decision === 'REVIEW' && <span>Не включено в заказ</span>}</div><div className="drawer-product-values"><div><span>Рекомендация</span><strong>{formatNumber(item.recommendedQuantity)} <small>{item.unit}</small></strong></div><div><span>Ориентировочная стоимость</span><strong>{formatMoney(item.estimatedCost)}</strong></div></div></section>
    <DataWarnings warnings={item.warnings} compact />
    {section === 'calculation' && <>
    <section className="drawer-section"><div className="section-heading"><h3>Расчёт потребности</h3><span className="tiny-badge">{mode === 'demo' ? 'Демо-ответ' : 'Данные API'}</span></div><dl className="breakdown">{lines.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}<div className="breakdown-subtotal"><dt>Расчётная потребность</dt><dd>{formatNumber(b.netRequirement)}</dd></div><div><dt>Минимальная партия / кратность</dt><dd>{b.moq === null ? '—' : formatNumber(b.moq)} / {b.orderMultiple === null ? '—' : formatNumber(b.orderMultiple)}</dd></div><div className="breakdown-total"><dt>Рекомендуемый заказ</dt><dd>{formatNumber(b.recommendedOrder)} {item.unit}</dd></div></dl></section>
    </>}
    {section === 'overview' && <>
    <DemandChart data={item.demand} />
    <AnomalyReview anomalies={item.anomalies} mode={mode} busy={busy} onDecision={(id, decision) => save({ anomalyId: id, anomalyDecision: decision })} />
    <section className="explanation-card"><div className="section-heading"><h3>Обоснование решения</h3><span>{item.explanation.source === 'AI' ? 'AI-объяснение' : 'Системное объяснение'}</span></div><p>{item.explanation.summary}</p><ul>{item.explanation.reasons.map((reason, index) => <li key={index}><Check size={14} />{reason}</li>)}</ul></section>
    </>}
    {section === 'decision' && <section className="drawer-section edit-section"><div className="section-heading"><h3>Решение менеджера</h3>{item.approvedQuantity !== undefined && <span className="manual-tag"><PencilLine size={12} /> Изменено вручную</span>}</div><label className="field">Утверждённое количество<div className="number-field"><input inputMode="numeric" aria-label="Утверждённое количество" value={quantity} onChange={(event) => setQuantity(event.target.value)} disabled={busy} aria-invalid={Boolean(invalid)} /><span>{item.unit}</span></div></label><p className="input-hint">Рекомендация системы: {formatNumber(item.recommendedQuantity)} {item.unit}</p>{invalid && <p className="field-error" role="alert">{invalid}</p>}{warning && <div className="notice notice-warning">{warning}</div>}<label className="field mt-4">Комментарий менеджера<textarea rows={3} maxLength={2000} value={comment} disabled={busy} onChange={(event) => setComment(event.target.value)} placeholder="Например, увеличена партия под подтверждённый спрос" /></label>{item.decision === 'REVIEW' && item.anomalies.some((a) => a.reviewable && a.decision === 'REVIEW') && <p className="input-hint">Сначала подтвердите аномалию, чтобы позиция могла войти в заказ. Вернитесь в раздел «Обзор спроса» для проверки.</p>}</section>}
    </div><footer className="drawer-footer"><ErrorNotice message={error} onRetry={busy || !lastPatch ? undefined : () => onSave(lastPatch)} /><div className="drawer-footer-actions"><span role="status">{busy ? 'Сохраняем решение…' : dirty ? 'Есть несохранённые изменения' : saved ? <><CheckCircle2 size={16} /> Решение сохранено</> : 'Изменения сохраняются по кнопке'}</span>{section === 'decision' ? <button className="btn btn-primary" disabled={busy || Boolean(invalid)} onClick={() => save({ approvedQuantity: Number(quantity), managerComment: comment })}>{busy ? <LoaderCircle size={16} className="spin" /> : <Check size={16} />} Сохранить решение</button> : <button className="btn btn-primary" onClick={() => setSection('decision')}>К решению менеджера <ArrowRight size={16} /></button>}</div></footer>
  </div></dialog>
}
