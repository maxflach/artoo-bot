import { useAtom, useAtomValue } from 'jotai'
import { useEffect, useState } from 'react'
import { apiKeyAtom, currentProjectAtom, scheduleDialogOpenAtom } from '../atoms'
import {
  fetchSchedules,
  createSchedule,
  deleteSchedule,
  type Schedule,
} from '../api'

function formatLastRun(lastRun: string | null): string {
  if (!lastRun) return 'never'
  const d = new Date(lastRun)
  const now = new Date()
  const diffMs = now.getTime() - d.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  if (diffMins < 1) return 'just now'
  if (diffMins < 60) return `${diffMins}m ago`
  const diffHours = Math.floor(diffMins / 60)
  if (diffHours < 24) return `${diffHours}h ago`
  return `${Math.floor(diffHours / 24)}d ago`
}

function matchesProject(s: Schedule, project: string): boolean {
  const ws = s.workspace ?? ''
  if (project === 'global') return ws === 'global' || ws === ''
  return ws === project
}

const WHEN_EXAMPLES = [
  'every morning',
  'every day 09:00',
  'every weekday 08:30',
  'every monday 10:00',
  'every hour',
  'every 6 hours',
  'tomorrow 18:00',
  'in 2h',
]

export default function ScheduleDialog() {
  const [open, setOpen] = useAtom(scheduleDialogOpenAtom)
  const apiKey = useAtomValue(apiKeyAtom)
  const currentProject = useAtomValue(currentProjectAtom)

  const [schedules, setSchedules] = useState<Schedule[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [tab, setTab] = useState<'list' | 'add'>('list')
  const [showAll, setShowAll] = useState(false)

  const [name, setName] = useState('')
  const [when, setWhen] = useState('')
  const [prompt, setPrompt] = useState('')
  const [oneShot, setOneShot] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [deletingId, setDeletingId] = useState<number | null>(null)

  const load = () => {
    setLoading(true)
    setError(null)
    fetchSchedules(apiKey)
      .then(setSchedules)
      .catch(e => setError(String(e)))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    if (open) {
      load()
      setTab('list')
      setShowAll(false)
    }
  }, [open]) // eslint-disable-line react-hooks/exhaustive-deps

  const projectSchedules = schedules.filter(s => matchesProject(s, currentProject))
  const otherSchedules = schedules.filter(s => !matchesProject(s, currentProject))
  const visibleSchedules = showAll ? schedules : projectSchedules

  async function handleDelete(id: number) {
    setDeletingId(id)
    try {
      await deleteSchedule(apiKey, id)
      setSchedules(prev => prev.filter(s => s.id !== id))
    } catch (e) {
      setError(String(e))
    } finally {
      setDeletingId(null)
    }
  }

  async function handleAdd(e: React.FormEvent) {
    e.preventDefault()
    if (!when.trim() || !prompt.trim()) return
    setSubmitting(true)
    setSubmitError(null)
    try {
      await createSchedule(apiKey, name.trim(), when.trim(), prompt.trim(), oneShot)
      setName('')
      setWhen('')
      setPrompt('')
      setOneShot(false)
      setTab('list')
      load()
    } catch (err) {
      setSubmitError(String(err))
    } finally {
      setSubmitting(false)
    }
  }

  if (!open) return null

  const inputCls = "w-full bg-white dark:bg-zinc-800 border border-gray-200 dark:border-zinc-600 rounded-lg px-3 py-2 text-sm text-gray-900 dark:text-zinc-100 placeholder-gray-400 dark:placeholder-zinc-500 outline-none focus:border-blue-400 dark:focus:border-blue-500 transition-colors"

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 dark:bg-black/60"
      onClick={e => { if (e.target === e.currentTarget) setOpen(false) }}
    >
      <div className="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-xl shadow-2xl w-full max-w-lg mx-4 flex flex-col max-h-[80vh]">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-zinc-700 shrink-0">
          <div>
            <h2 className="text-base font-semibold text-gray-900 dark:text-zinc-100">Schedules</h2>
            <p className="text-xs text-gray-500 dark:text-zinc-500 mt-0.5">Project: <span className="text-gray-700 dark:text-zinc-300">{currentProject}</span></p>
          </div>
          <button
            onClick={() => setOpen(false)}
            className="text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300 text-xl leading-none transition-colors"
          >
            ×
          </button>
        </div>

        {/* Tabs */}
        <div className="flex border-b border-gray-200 dark:border-zinc-700 shrink-0">
          <button
            onClick={() => setTab('list')}
            className={`px-5 py-2 text-sm font-medium transition-colors ${
              tab === 'list'
                ? 'text-blue-600 dark:text-blue-400 border-b-2 border-blue-500 dark:border-blue-400'
                : 'text-gray-500 dark:text-zinc-400 hover:text-gray-700 dark:hover:text-zinc-200'
            }`}
          >
            Scheduled tasks
            {projectSchedules.length > 0 && (
              <span className="ml-1.5 text-xs text-gray-400 dark:text-zinc-500">{projectSchedules.length}</span>
            )}
          </button>
          <button
            onClick={() => setTab('add')}
            className={`px-5 py-2 text-sm font-medium transition-colors ${
              tab === 'add'
                ? 'text-blue-600 dark:text-blue-400 border-b-2 border-blue-500 dark:border-blue-400'
                : 'text-gray-500 dark:text-zinc-400 hover:text-gray-700 dark:hover:text-zinc-200'
            }`}
          >
            + Add new
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto">
          {tab === 'list' && (
            <div className="p-4 flex flex-col gap-3">
              {error && (
                <div className="text-xs text-red-600 dark:text-red-400 px-1">
                  {error}{' '}
                  <button onClick={load} className="underline hover:opacity-70">Retry</button>
                </div>
              )}
              {loading && (
                <p className="text-sm text-gray-400 dark:text-zinc-500 text-center py-6">Loading...</p>
              )}
              {!loading && projectSchedules.length === 0 && !error && (
                <p className="text-sm text-gray-500 dark:text-zinc-500 text-center py-6">
                  No schedules for <span className="text-gray-700 dark:text-zinc-300">{currentProject}</span>.{' '}
                  <button onClick={() => setTab('add')} className="text-blue-600 dark:text-blue-400 hover:opacity-80">
                    Add one
                  </button>
                </p>
              )}
              {visibleSchedules.map(s => (
                <div
                  key={s.id}
                  className="bg-gray-50 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 rounded-lg p-3 flex flex-col gap-1"
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="flex items-center gap-2 min-w-0">
                      <span className="text-sm shrink-0">
                        {s.one_shot ? '⏰' : s.enabled ? '✅' : '⏸'}
                      </span>
                      <span className="text-sm font-medium text-gray-900 dark:text-zinc-100 truncate">{s.name}</span>
                      {showAll && !matchesProject(s, currentProject) && (
                        <span className="text-xs text-gray-500 dark:text-zinc-500 shrink-0 bg-gray-200 dark:bg-zinc-700 px-1.5 py-0.5 rounded">
                          {s.workspace || 'global'}
                        </span>
                      )}
                    </div>
                    <button
                      onClick={() => handleDelete(s.id)}
                      disabled={deletingId === s.id}
                      className="text-gray-400 dark:text-zinc-600 hover:text-red-500 dark:hover:text-red-400 transition-colors text-xs shrink-0 disabled:opacity-40"
                      title="Delete"
                    >
                      {deletingId === s.id ? '…' : '🗑'}
                    </button>
                  </div>
                  <code className="text-xs text-blue-600 dark:text-blue-300 font-mono">{s.schedule}</code>
                  <p className="text-xs text-gray-600 dark:text-zinc-400 line-clamp-2">{s.prompt}</p>
                  <span className="text-xs text-gray-400 dark:text-zinc-600">last run: {formatLastRun(s.last_run)}</span>
                </div>
              ))}

              {!loading && otherSchedules.length > 0 && (
                <button
                  onClick={() => setShowAll(v => !v)}
                  className="text-xs text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300 transition-colors text-center pt-1"
                >
                  {showAll
                    ? `Show only ${currentProject}`
                    : `+ ${otherSchedules.length} schedule${otherSchedules.length === 1 ? '' : 's'} in other projects`}
                </button>
              )}
            </div>
          )}

          {tab === 'add' && (
            <form onSubmit={handleAdd} className="p-4 flex flex-col gap-4">
              <div className="text-xs text-gray-500 dark:text-zinc-500 bg-gray-50 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 rounded-lg px-3 py-2">
                Will run in project: <span className="text-gray-700 dark:text-zinc-300 font-medium">{currentProject}</span>
              </div>

              <div className="flex gap-4">
                <label className="flex items-center gap-2 cursor-pointer text-sm text-gray-700 dark:text-zinc-300">
                  <input type="radio" name="type" checked={!oneShot} onChange={() => setOneShot(false)} className="accent-blue-500" />
                  Recurring
                </label>
                <label className="flex items-center gap-2 cursor-pointer text-sm text-gray-700 dark:text-zinc-300">
                  <input type="radio" name="type" checked={oneShot} onChange={() => setOneShot(true)} className="accent-blue-500" />
                  One-off reminder
                </label>
              </div>

              <div>
                <label className="block text-xs text-gray-500 dark:text-zinc-400 mb-1">
                  Name <span className="text-gray-400 dark:text-zinc-600">(optional)</span>
                </label>
                <input
                  type="text"
                  value={name}
                  onChange={e => setName(e.target.value)}
                  placeholder="e.g. morning-standup"
                  className={inputCls}
                />
              </div>

              <div>
                <label className="block text-xs text-gray-500 dark:text-zinc-400 mb-1">When *</label>
                <input
                  type="text"
                  value={when}
                  onChange={e => setWhen(e.target.value)}
                  placeholder={oneShot ? 'e.g. tomorrow 09:00 or in 2h' : 'e.g. every day 08:00'}
                  required
                  className={inputCls}
                />
                <div className="mt-1.5 flex flex-wrap gap-1">
                  {WHEN_EXAMPLES
                    .filter(ex =>
                      oneShot
                        ? ex.startsWith('tomorrow') || ex.startsWith('in ')
                        : !ex.startsWith('tomorrow') && !ex.startsWith('in ')
                    )
                    .map(ex => (
                      <button
                        key={ex}
                        type="button"
                        onClick={() => setWhen(ex)}
                        className="px-2 py-0.5 text-xs bg-gray-100 dark:bg-zinc-700 hover:bg-gray-200 dark:hover:bg-zinc-600 text-gray-600 dark:text-zinc-300 rounded transition-colors"
                      >
                        {ex}
                      </button>
                    ))}
                </div>
              </div>

              <div>
                <label className="block text-xs text-gray-500 dark:text-zinc-400 mb-1">Task prompt *</label>
                <textarea
                  value={prompt}
                  onChange={e => setPrompt(e.target.value)}
                  placeholder="What should Artoo do?"
                  required
                  rows={3}
                  className={`${inputCls} resize-none`}
                />
              </div>

              {submitError && (
                <p className="text-xs text-red-600 dark:text-red-400">{submitError}</p>
              )}

              <button
                type="submit"
                disabled={submitting || !when.trim() || !prompt.trim()}
                className="px-4 py-2 bg-blue-600 hover:bg-blue-500 disabled:bg-gray-100 dark:disabled:bg-zinc-700 disabled:text-gray-400 dark:disabled:text-zinc-500 text-white rounded-lg text-sm font-medium transition-colors"
              >
                {submitting ? 'Adding...' : 'Add schedule'}
              </button>
            </form>
          )}
        </div>
      </div>
    </div>
  )
}
