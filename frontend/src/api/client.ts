const baseUrl = (import.meta.env.VITE_API_BASE_URL ?? 'http://127.0.0.1:8080').replace(/\/+$/, '')
const timeoutMs = 120_000
const connectionError = 'Не удалось связаться с backend. Проверьте, что он запущен, и повторите запрос.'
const endpoint = (path: string) => `${baseUrl}${path}`
const object = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null

export function responseError(status: number, body: unknown): Error {
  const messages: Record<number, string> = {
    404: 'Импорт или расчёт не найден. После перезапуска backend загрузите файлы заново.',
    413: 'Превышен допустимый размер файлов или данных для анализа.',
    422: 'Не удалось обработать данные. Проверьте файлы и параметры.',
    502: 'Сервис анализа вернул некорректный ответ. Повторите расчёт позже.',
    503: 'Сервис анализа недоступен. Загруженные данные сохранены; повторите расчёт позже.',
    504: 'Время расчёта истекло. Загруженные данные сохранены; повторите попытку.',
  }
  const error = object(body) && object(body.error) ? body.error : null
  let message = error && typeof error.message === 'string' ? error.message : messages[status] || `Ошибка сервера (${status}). Повторите запрос.`
  if (error && object(error.details)) {
    const details = error.details
    const fields = Array.isArray(details.fields) ? details.fields : Array.isArray(details.errors) ? details.errors : []
    const descriptions = fields.slice(0, 3).filter(object).map((field) => `${typeof field.field === 'string' ? `${field.field}: ` : typeof field.file === 'string' ? `${field.file}: ` : ''}${typeof field.message === 'string' ? field.message : ''}`).filter(Boolean)
    if (descriptions.length) message += ` ${descriptions.join('; ')}`
  }
  return new Error(message)
}
export async function request(path: string, options: RequestInit = {}): Promise<Response> {
  let response: Response
  try {
    response = await fetch(endpoint(path), { ...options, signal: options.signal ?? AbortSignal.timeout(timeoutMs) })
  } catch (error) {
    if (error instanceof Error && ['TimeoutError', 'AbortError'].includes(error.name)) throw new Error('Сервер не ответил вовремя. Повторите запрос; предыдущий результат сохранён.')
    throw new Error(connectionError)
  }
  if (!response.ok) throw responseError(response.status, await response.json().catch(() => null))
  return response
}
export async function requestJSON(path: string, options: RequestInit = {}): Promise<unknown> {
  const response = await request(path, { ...options, headers: { 'Accept': 'application/json', ...(options.body ? { 'Content-Type': 'application/json' } : {}), ...options.headers } })
  if (!response.headers.get('Content-Type')?.includes('application/json')) throw new Error('Backend вернул ответ вместо ожидаемого JSON. Проверьте адрес API.')
  try { return await response.json() } catch { throw new Error('Backend вернул повреждённый JSON.') }
}
export function upload(path: string, form: FormData, onProgress: (value: number | null) => void): Promise<unknown> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('POST', endpoint(path)); xhr.timeout = timeoutMs
    xhr.upload.onprogress = (event) => onProgress(event.lengthComputable ? Math.round(event.loaded / event.total * 100) : null)
    xhr.onerror = () => reject(new Error(connectionError))
    xhr.ontimeout = () => reject(new Error('Загрузка и проверка заняли больше двух минут. Повторите запрос.'))
    xhr.onabort = () => reject(new Error('Загрузка отменена.'))
    xhr.onload = () => {
      let body: unknown
      try { body = JSON.parse(xhr.responseText) } catch { reject(responseError(xhr.status, null)); return }
      if (xhr.status < 200 || xhr.status >= 300) { reject(responseError(xhr.status, body)); return }
      resolve(body)
    }
    // The browser supplies the multipart boundary; do not set Content-Type.
    xhr.send(form)
  })
}
