import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import './index.css'
import App from './App.tsx'
import { runGroupBlocksTests } from './components/chat/groupBlocks.test.ts'
import { Agentation } from 'agentation'

// Run unit tests on startup
runGroupBlocksTests()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <App />
    </BrowserRouter>
    {import.meta.env.MODE !== 'production' && <Agentation />}
  </StrictMode>,
)
