import { Component } from 'react'
import type { ReactNode } from 'react'
export default class ErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false }
  static getDerivedStateFromError() { return { failed: true } }
  render() { return this.state.failed ? <main className="fatal-error"><h1>Не удалось отобразить результат</h1><p>Проверьте формат ответа API. Обновление страницы начнёт новую сессию.</p><button className="btn btn-primary" onClick={() => location.reload()}>Обновить страницу</button></main> : this.props.children }
}
