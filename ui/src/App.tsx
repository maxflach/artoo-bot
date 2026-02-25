import { useAtomValue } from 'jotai'
import { HashRouter, Routes, Route, Navigate } from 'react-router-dom'
import { apiKeyAtom } from './atoms'
import Login from './components/Login'
import Sidebar from './components/Sidebar'
import ChatPane from './components/ChatPane'
import ScheduleDialog from './components/ScheduleDialog'

function Layout() {
  return (
    <div className="flex h-screen bg-zinc-900 text-zinc-100 overflow-hidden">
      <Sidebar />
      <ChatPane />
      <ScheduleDialog />
    </div>
  )
}

export default function App() {
  const apiKey = useAtomValue(apiKeyAtom)

  if (!apiKey) return <Login />

  return (
    <HashRouter>
      <Routes>
        <Route path="/" element={<Navigate to="/p/global" replace />} />
        <Route path="/p/:project" element={<Layout />} />
        <Route path="*" element={<Navigate to="/p/global" replace />} />
      </Routes>
    </HashRouter>
  )
}
