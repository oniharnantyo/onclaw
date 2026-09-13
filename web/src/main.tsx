import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import '@fontsource/inter/400.css'
import '@fontsource/inter/500.css'
import '@fontsource/inter/600.css'
import '@fontsource/inter/700.css'
import '@fontsource/jetbrains-mono/400.css'
import '@fontsource/jetbrains-mono/500.css'
import './index.css'
import { initTheme } from './lib/theme'
import App from './App.tsx'

// App-lifetime theme bootstrap (change add-dark-theme): applies the stored
// preference and, while it is 'system', tracks OS scheme changes. The cleanup
// return is deliberately ignored — the subscription lives for the page.
initTheme()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
