import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { AuthProvider, useAuth } from './lib/auth'
import { Login } from './pages/Login'
import { Dashboard } from './pages/Dashboard'
import { Agents } from './pages/Agents'
import { AgentDetail } from './pages/AgentDetail'
import { Groups } from './pages/Groups'
import { Templates } from './pages/Templates'
import { Jobs } from './pages/Jobs'
import { JobDetail } from './pages/JobDetail'
import { Audit } from './pages/Audit'
import { Tokens } from './pages/Tokens'
import { Layout } from './components/Layout'
import { useTranslation } from 'react-i18next'

function Protected({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth()
  const location = useLocation()
  if (loading) return <div className="h-full grid place-items-center text-slate-500">…</div>
  if (!user) return <Navigate to="/login" replace state={{ from: location }} />
  return <>{children}</>
}

function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route
        path="/*"
        element={
          <Protected>
            <Layout>
              <Routes>
                <Route path="/" element={<Dashboard />} />
                <Route path="/agents" element={<Agents />} />
                <Route path="/agents/:id" element={<AgentDetail />} />
                <Route path="/groups" element={<Groups />} />
                <Route path="/templates" element={<Templates />} />
                <Route path="/jobs" element={<Jobs />} />
                <Route path="/jobs/:id" element={<JobDetail />} />
                <Route path="/tokens" element={<Tokens />} />
                <Route path="/audit" element={<Audit />} />
              </Routes>
            </Layout>
          </Protected>
        }
      />
    </Routes>
  )
}

export default function App() {
  // forzar inicialización de i18n
  useTranslation()
  return (
    <AuthProvider>
      <AppRoutes />
    </AuthProvider>
  )
}