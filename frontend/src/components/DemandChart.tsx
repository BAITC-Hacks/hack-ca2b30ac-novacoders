import { ChartNoAxesCombined } from 'lucide-react'
import { Bar, CartesianGrid, ComposedChart, Line, ReferenceArea, ReferenceDot, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import type { DemandPoint } from '../types/api'
import { formatNumber } from '../lib/format'

export default function DemandChart({ data }: { data: DemandPoint[] }) {
  if (!data.length) return <div className="chart-empty"><ChartNoAxesCombined size={26} /><strong>Недостаточно данных для графика</strong><p>API не передал историю спроса для этой позиции.</p></div>
  return <section className="drawer-section"><div className="section-heading"><h3>Динамика спроса</h3><span className="muted">Продажи и регулярный спрос, шт.</span></div>
    <div className="chart-legend"><span><i className="legend-sales" />Продажи</span><span><i className="legend-clean" />Очищенный спрос</span><span><i className="legend-forecast" />Прогноз</span></div>
    <div className="demand-chart" role="img" aria-label="Месячные продажи, очищенный спрос и прогноз. Периоды отсутствия товара выделены янтарным.">
      <ResponsiveContainer width="100%" height="100%"><ComposedChart data={data} margin={{ top: 27, right: 17, bottom: 0, left: -20 }}>
        <CartesianGrid strokeDasharray="3 4" vertical={false} stroke="#e9edf3" />
        <XAxis dataKey="month" axisLine={false} tickLine={false} tick={{ fill: '#526071', fontSize: 12 }} dy={7} />
        <YAxis axisLine={false} tickLine={false} tick={{ fill: '#526071', fontSize: 12 }} tickFormatter={formatNumber} />
        <Tooltip contentStyle={{ border: '1px solid #e5e9f1', borderRadius: 6, fontSize: 13, boxShadow: 'none' }} formatter={(value, name) => [typeof value === 'number' ? formatNumber(value) : '—', name]} />
        {data.filter((point) => point.stockout).map((point) => <ReferenceArea key={point.month} x1={point.month} x2={point.month} stroke="#f8e9c9" strokeWidth={35} fill="#f8e9c9" label={{ value: 'stockout', position: 'insideTop', fill: '#a27018', fontSize: 10 }} />)}
        <Bar name="Продажи" dataKey="sales" fill="#dce2ed" radius={[3, 3, 0, 0]} barSize={22} isAnimationActive={false} />
        <Line name="Очищенный спрос" type="monotone" dataKey="cleanedDemand" stroke="#2455d6" strokeWidth={2.5} dot={{ r: 3, fill: '#2455d6', stroke: '#fff', strokeWidth: 2 }} activeDot={{ r: 5 }} connectNulls={false} isAnimationActive={false} />
        <Line name="Прогноз" type="monotone" dataKey="forecast" stroke="#344054" strokeWidth={2.5} strokeDasharray="5 4" dot={{ r: 3, fill: '#344054', stroke: '#fff', strokeWidth: 2 }} connectNulls={false} isAnimationActive={false} />
        {data.filter((point) => point.anomaly !== undefined && point.sales !== null).map((point) => <ReferenceDot key={point.month} x={point.month} y={point.sales!} r={5} fill="#d3972c" stroke="#fff" strokeWidth={2} label={{ value: `${formatNumber(point.anomaly!)} · аномалия`, position: 'top', fill: '#91631a', fontSize: 11 }} />)}
      </ComposedChart></ResponsiveContainer>
    </div>
    <div className="chart-footnote">{data.some((point) => point.stockout) && <span><i /> Период отсутствия товара</span>}{data.some((point) => point.incomplete) && <span>* Сентябрь 2026 — неполный месяц</span>}</div>
  </section>
}
