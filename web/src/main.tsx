import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { LanguageProvider } from './i18n'

const root = document.getElementById('root')
if (!root) throw new Error('The console root element is missing.')

createRoot(root).render(<StrictMode><LanguageProvider><App /></LanguageProvider></StrictMode>)
