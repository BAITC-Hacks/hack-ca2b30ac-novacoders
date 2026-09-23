import { ArrowRight, LoaderCircle } from 'lucide-react'
import type { Stage } from '../api/types'
import { ErrorNotice } from './DataWarnings'

interface Props {
  stage: Exclude<Stage, 'IMPORT'>; hasDataset: boolean; loading: boolean; error: string | null
  onImport: () => void; onConfigure: () => void; onRetry?: () => void
}
export default function StagePrerequisite({ stage, hasDataset, loading, error, onImport, onConfigure, onRetry }: Props) {
  const title = loading ? 'Ожидаем ответ сервера' : !hasDataset ? 'Сначала добавьте данные поставщика' : stage === 'APPROVED' ? 'Сначала рассчитайте рекомендации' : 'Рекомендации ещё не рассчитаны'
  return <section className="panel stage-prerequisite" aria-busy={loading}>
    <div className="prerequisite-heading">{loading && <LoaderCircle className="spin" size={22} />}<h2>{title}</h2></div>
    <p>{loading ? 'Дождитесь завершения текущего запроса.' : !hasDataset ? 'Загрузите шесть Excel-файлов в разделе «Импорт данных». После проверки можно настроить параметры, рассчитать потребность и сформировать заказ.' : 'Откройте параметры, проверьте условия поставки и нажмите «Рассчитать заказ». Готовые рекомендации появятся здесь.'}</p>
    {!loading && <div className="prerequisite-actions"><button className="btn btn-primary" onClick={hasDataset ? onConfigure : onImport}>{hasDataset ? 'К параметрам расчёта' : 'Перейти к импорту'}<ArrowRight size={16} /></button>{onRetry && <button className="btn btn-secondary" onClick={onRetry}>Повторить</button>}</div>}
    <ErrorNotice message={error} />
  </section>
}
