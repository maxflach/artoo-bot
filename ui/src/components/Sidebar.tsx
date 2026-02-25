import { useAtom, useAtomValue, useSetAtom } from 'jotai'
import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { apiKeyAtom, projectsAtom, scheduleDialogOpenAtom } from '../atoms'
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

  if (collapsed) {
    return (
      <div className="w-10 flex flex-col items-center py-3 border-r border-zinc-700 bg-zinc-800 shrink-0">
        <button
          onClick={() => setCollapsed(false)}
          className="text-zinc-400 hover:text-zinc-100 text-lg leading-none"
          title="Expand sidebar"
        >
          ›
        </button>
      </div>
    )
  }

  return (
    <div className="w-64 flex flex-col border-r border-zinc-700 bg-zinc-800 shrink-0">
      <div className="flex items-center justify-between px-4 py-3 border-b border-zinc-700">
        <span className="text-sm font-semibold text-zinc-300">Projects</span>
        <button
          onClick={() => setCollapsed(true)}
          className="text-zinc-500 hover:text-zinc-300 text-lg leading-none"
          title="Collapse sidebar"
        >
          ‹
        </button>
      </div>

      <div className="flex-1 overflow-y-auto py-2">
        {fetchError && (
          <div className="px-4 py-2 text-xs text-red-400">
            Failed to load projects.{' '}
            <button onClick={loadProjects} className="underline hover:text-red-300">
              Retry
            </button>
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
              className={`w-full text-left px-4 py-2 text-sm flex items-center gap-2 hover:bg-zinc-700 transition-colors ${
                isActive ? 'bg-zinc-700 text-zinc-100' : 'text-zinc-400'
              }`}
            >
              <span className={`text-xs shrink-0 ${isActive ? 'text-blue-400' : 'text-zinc-600'}`}>
                {typeIcon(p.type)}
              </span>
              <span className="truncate flex-1">{p.title}</span>
              {isLoading && <span className="text-xs text-zinc-500 shrink-0">...</span>}
            </button>
          )
        })}
      </div>

      <div className="border-t border-zinc-700 p-3 flex flex-col gap-1">
        <button
          onClick={() => setScheduleDialogOpen(true)}
          className="w-full text-sm text-zinc-400 hover:text-zinc-100 transition-colors py-1 flex items-center gap-2"
        >
          <span>⏰</span> Schedules
        </button>
        <button
          onClick={() => setApiKey('')}
          className="w-full text-sm text-zinc-500 hover:text-zinc-300 transition-colors py-1"
        >
          Logout
        </button>
      </div>
    </div>
  )
}
