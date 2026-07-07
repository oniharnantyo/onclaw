import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import { runGroupBlocksTests } from './components/chat/groupBlocks.test.ts'
import { Agentation } from 'agentation'

// Run unit tests on startup
runGroupBlocksTests()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
    {import.meta.env.MODE !== 'production' && <Agentation />}
  </StrictMode>,
)
