import { useAtomValue, useSetAtom } from 'jotai'
import { useEffect } from 'react'
import { HashRouter, Routes, Route, Navigate, useParams } from 'react-router-dom'
import { apiKeyAtom, currentProjectAtom } from './atoms'
import Login from './components/Login'
import Sidebar from './components/Sidebar'
import ChatPane from './components/ChatPane'
import ScheduleDialog from './components/ScheduleDialog'

function Layout() {
  const { project: rawProject } = useParams<{ project: string }>()
  const setCurrentProject = useSetAtom(currentProjectAtom)

  useEffect(() => {
    setCurrentProject(decodeURIComponent(rawProject ?? 'global'))
  }, [rawProject, setCurrentProject])

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
