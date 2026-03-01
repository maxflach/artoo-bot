import { useAtom, useAtomValue, useSetAtom } from 'jotai'
import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { apiKeyAtom, filesDialogOpenAtom, projectsAtom, scheduleDialogOpenAtom, themeAtom, wishlistDialogOpenAtom } from '../atoms'
import { fetchProjects, switchProject, type Project } from '../api'

function typeIcon(type: string) {
  if (type === 'external') return '⬡'
  if (type === 'shared') return '◈'
  return '◇'
}

export default function Sidebar() {
  const apiKey = useAtomValue(apiKeyAtom)
  const [projects, setProjects] = useAtom(projectsAtom)
  const setApiKey = useSetAtom(apiKeyAtom)
  const setScheduleDialogOpen = useSetAtom(scheduleDialogOpenAtom)
  const setFilesDialogOpen = useSetAtom(filesDialogOpenAtom)
  const [theme, setTheme] = useAtom(themeAtom)
  const setWishlistDialogOpen = useSetAtom(wishlistDialogOpenAtom)
  const navigate = useNavigate()
  const { project: rawProject } = useParams<{ project: string }>()
  const currentProject = decodeURIComponent(rawProject ?? 'global')
  const [collapsed, setCollapsed] = useState(false)
  const [switching, setSwitching] = useState<string | null>(null)
  const [fetchError, setFetchError] = useState<string | null>(null)

  const loadProjects = () => {
    setFetchError(null)
    fetchProjects(apiKey)
      .then(r => setProjects(r.projects))
      .catch(e => setFetchError(String(e)))
  }

  useEffect(() => {
    loadProjects()
  }, [apiKey]) // eslint-disable-line react-hooks/exhaustive-deps

  async function handleSwitch(p: Project) {
    if (switching) return
    setSwitching(p.name)
    try {
      await switchProject(apiKey, p.name)
      navigate(`/p/${encodeURIComponent(p.name)}`)
    } catch (e) {
      console.error('switch failed', e)
    } finally {
      setSwitching(null)
    }
  }

  const toggleTheme = () => setTheme(t => t === 'dark' ? 'light' : 'dark')

  if (collapsed) {
    return (
      <div className="w-10 flex flex-col items-center py-3 border-r border-gray-200 dark:border-white/5 bg-white dark:bg-zinc-900 shrink-0">
        <button
          onClick={() => setCollapsed(false)}
          className="text-gray-400 dark:text-zinc-500 hover:text-gray-700 dark:hover:text-zinc-200 text-lg leading-none transition-colors"
          title="Expand sidebar"
        >
          ›
        </button>
      </div>
    )
  }

  return (
    <div className="w-60 flex flex-col border-r border-gray-200 dark:border-white/5 bg-white dark:bg-zinc-900 shrink-0">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3.5 border-b border-gray-200 dark:border-white/5">
        <div className="flex items-center gap-2">
          <img src="/chat/avatar.png" alt="Artoo" className="w-6 h-6 rounded-full object-cover opacity-90" />
          <span className="text-sm font-semibold text-gray-800 dark:text-zinc-200">Artoo</span>
        </div>
        <div className="flex items-center gap-1">
          <button
            onClick={toggleTheme}
            className="text-gray-400 dark:text-zinc-600 hover:text-gray-700 dark:hover:text-zinc-300 transition-colors px-1 text-sm"
            title={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
          >
            {theme === 'dark' ? '☀' : '☾'}
          </button>
          <button
            onClick={() => setCollapsed(true)}
            className="text-gray-400 dark:text-zinc-600 hover:text-gray-700 dark:hover:text-zinc-300 text-lg leading-none transition-colors"
            title="Collapse sidebar"
          >
            ‹
          </button>
        </div>
      </div>

      {/* Project label */}
      <div className="px-4 pt-4 pb-1">
        <span className="text-[10px] font-semibold text-gray-400 dark:text-zinc-600 uppercase tracking-widest">Projects</span>
      </div>

      <div className="flex-1 overflow-y-auto py-1">
        {fetchError && (
          <div className="mx-3 my-2 px-3 py-2 rounded-lg bg-red-50 dark:bg-red-950/50 border border-red-200 dark:border-red-900/50 text-xs text-red-600 dark:text-red-400">
            Failed to load.{' '}
            <button onClick={loadProjects} className="underline hover:opacity-70">Retry</button>
          </div>
        )}
        {projects.map(p => {
          const isActive = currentProject === p.name
          const isLoading = switching === p.name
          return (
            <button
              key={p.name}
              onClick={() => handleSwitch(p)}
              disabled={isLoading}
              className={`w-full text-left px-3 mx-1 py-2 text-sm flex items-center gap-2 rounded-lg transition-all my-0.5 ${
                isActive
                  ? 'bg-blue-50 dark:bg-white/8 text-gray-900 dark:text-zinc-100'
                  : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-100 dark:hover:bg-white/5 hover:text-gray-800 dark:hover:text-zinc-200'
              }`}
              style={{ width: 'calc(100% - 8px)' }}
            >
              <span className={`text-xs shrink-0 transition-colors ${isActive ? 'text-blue-500 dark:text-blue-400' : 'text-gray-300 dark:text-zinc-600'}`}>
                {typeIcon(p.type)}
              </span>
              <span className="truncate flex-1">{p.title}</span>
              {isActive && <span className="w-1.5 h-1.5 rounded-full bg-blue-500 dark:bg-blue-400 shrink-0" />}
              {isLoading && <span className="text-xs text-gray-400 dark:text-zinc-600 shrink-0">…</span>}
            </button>
          )
        })}
      </div>

      {/* Footer */}
      <div className="border-t border-gray-200 dark:border-white/5 p-3 flex flex-col gap-0.5">
        <button
          onClick={() => setFilesDialogOpen(true)}
          className="w-full text-sm text-gray-500 dark:text-zinc-500 hover:text-gray-800 dark:hover:text-zinc-200 hover:bg-gray-100 dark:hover:bg-white/5 transition-all py-1.5 px-3 rounded-lg flex items-center gap-2.5"
        >
          <span className="text-base">📁</span>
          <span>Files</span>
        </button>
        <button
          onClick={() => setScheduleDialogOpen(true)}
          className="w-full text-sm text-gray-500 dark:text-zinc-500 hover:text-gray-800 dark:hover:text-zinc-200 hover:bg-gray-100 dark:hover:bg-white/5 transition-all py-1.5 px-3 rounded-lg flex items-center gap-2.5"
        >
          <span className="text-base">⏰</span>
          <span>Schedules</span>
        </button>
        <button
          onClick={() => setWishlistDialogOpen(true)}
          className="w-full text-sm text-gray-500 dark:text-zinc-500 hover:text-gray-800 dark:hover:text-zinc-200 hover:bg-gray-100 dark:hover:bg-white/5 transition-all py-1.5 px-3 rounded-lg flex items-center gap-2.5"
        >
          <span className="text-base">✨</span>
          <span>Wishlist</span>
        </button>
        <div className="my-1 border-t border-gray-200 dark:border-white/5" />
        <button
          onClick={() => setApiKey('')}
          className="w-full text-xs text-gray-400 dark:text-zinc-600 hover:text-gray-600 dark:hover:text-zinc-400 hover:bg-gray-100 dark:hover:bg-white/5 transition-all py-1.5 px-3 rounded-lg text-left"
        >
          Sign out
        </button>
      </div>
    </div>
  )
}
