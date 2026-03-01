import { useAtomValue, useSetAtom } from 'jotai'
import { useEffect } from 'react'
import { HashRouter, Routes, Route, Navigate, useParams } from 'react-router-dom'
import { apiKeyAtom, currentProjectAtom, themeAtom } from './atoms'
import Login from './components/Login'
import Sidebar from './components/Sidebar'
import ChatPane from './components/ChatPane'
import ScheduleDialog from './components/ScheduleDialog'
import FilesDialog from './components/FilesDialog'
import WishlistDialog from './components/WishlistDialog'

function Layout() {
  const { project: rawProject } = useParams<{ project: string }>()
  const setCurrentProject = useSetAtom(currentProjectAtom)

  useEffect(() => {
    setCurrentProject(decodeURIComponent(rawProject ?? 'global'))
  }, [rawProject, setCurrentProject])

  return (
    <div className="flex h-screen bg-gray-50 dark:bg-zinc-950 text-gray-900 dark:text-zinc-100 overflow-hidden transition-colors">
      <Sidebar />
      <ChatPane />
      <ScheduleDialog />
      <FilesDialog />
      <WishlistDialog />
    </div>
  )
}

export default function App() {
  const apiKey = useAtomValue(apiKeyAtom)
  const theme = useAtomValue(themeAtom)

  useEffect(() => {
    document.documentElement.classList.toggle('dark', theme === 'dark')
  }, [theme])

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
