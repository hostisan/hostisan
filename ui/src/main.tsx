import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { createBrowserRouter } from 'react-router'
import { RouterProvider } from 'react-router/dom'
import { appRoutes, Root } from './App.tsx'
import { retryQuery } from './api/errors'
import { RootError } from './layout/RootError'
import { SessionProvider } from './session/SessionProvider'
import './index.css'

const queryClient = new QueryClient({ defaultOptions: { queries: { retry: retryQuery, refetchOnWindowFocus: false } } })

const router = createBrowserRouter([{ element: <Root />, errorElement: <RootError />, children: appRoutes }])

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <SessionProvider>
        <RouterProvider router={router} />
      </SessionProvider>
    </QueryClientProvider>
  </StrictMode>,
)
