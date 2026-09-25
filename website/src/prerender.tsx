import { StrictMode } from 'react'
import { renderToString } from 'react-dom/server'
import { Home } from './home/Home'
import { Protect } from './protect/Protect'

export const pages: Record<string, () => string> = {
  'index.html': () => renderToString(<StrictMode><Home /></StrictMode>),
  'protect.html': () => renderToString(<StrictMode><Protect /></StrictMode>),
}
