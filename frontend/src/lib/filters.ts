import type { Decision, Urgency } from '../types/api'
export interface Filters { search: string; decision: Decision | ''; category: string; urgency: Urgency | ''; warningsOnly: boolean }
export const initialFilters: Filters = { search: '', decision: '', category: '', urgency: '', warningsOnly: false }
