import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { createQueryClient } from './queryClient'
import { createAppRouter } from './router'
import './styles.css'

const root = document.getElementById('root')
if (!root) {
  throw new Error('missing #root element')
}
const queryClient = createQueryClient()
const router = createAppRouter(queryClient)
createRoot(root).render(
  <StrictMode>
    <App queryClient={queryClient} router={router} />
  </StrictMode>,
)
