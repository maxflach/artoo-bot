import { useSetAtom } from 'jotai'
import { useState } from 'react'
import { apiKeyAtom } from '../atoms'
import { validateKey } from '../api'

export default function Login() {
  const setApiKey = useSetAtom(apiKeyAtom)
  const [key, setKey] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const candidate = key.trim()
    if (!candidate) return
    setError('')
    setLoading(true)
    try {
      const ok = await validateKey(candidate)
      if (!ok) {
        setError('Invalid API key')
      } else {
        setApiKey(candidate)
      }
    } catch {
      setError('Could not connect to server')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-gray-50 dark:bg-zinc-950 flex items-center justify-center p-4 transition-colors">
      <div className="fixed inset-0 pointer-events-none">
        <div className="absolute top-1/3 left-1/2 -translate-x-1/2 -translate-y-1/2 w-96 h-96 bg-blue-500/5 dark:bg-blue-600/5 rounded-full blur-3xl" />
      </div>

      <div className="relative w-full max-w-xs">
        <form onSubmit={handleSubmit} className="flex flex-col items-center gap-7">
          <div className="relative">
            <div className="absolute inset-0 rounded-full bg-blue-500/15 blur-2xl scale-[2]" />
            <img
              src="/chat/avatar.png"
              alt="Artoo"
              className="relative w-16 h-16 rounded-full object-cover ring-1 ring-gray-300 dark:ring-white/15 shadow-xl"
            />
          </div>

          <div className="text-center">
            <h1 className="text-2xl font-bold text-gray-900 dark:text-zinc-100 tracking-tight">Artoo</h1>
            <p className="text-sm text-gray-500 dark:text-zinc-500 mt-1">Your personal AI assistant</p>
          </div>

          <div className="w-full flex flex-col gap-3">
            <input
              type="password"
              value={key}
              onChange={e => setKey(e.target.value)}
              placeholder="Enter your API key"
              autoComplete="current-password"
              autoFocus
              className="w-full bg-white dark:bg-zinc-900 border border-gray-200 dark:border-white/10 rounded-xl px-4 py-3 text-gray-900 dark:text-zinc-100 placeholder-gray-400 dark:placeholder-zinc-600 text-sm outline-none focus:border-blue-400 dark:focus:border-blue-500/60 focus:ring-2 focus:ring-blue-500/10 transition-all"
            />
            {error && (
              <div className="flex items-center gap-2 px-3 py-2 bg-red-50 dark:bg-red-950/50 border border-red-200 dark:border-red-900/50 rounded-lg">
                <span className="text-red-600 dark:text-red-400 text-xs">{error}</span>
              </div>
            )}
            <button
              type="submit"
              disabled={loading || !key.trim()}
              className="w-full bg-blue-600 hover:bg-blue-500 active:bg-blue-700 disabled:bg-gray-100 dark:disabled:bg-zinc-800 disabled:text-gray-400 dark:disabled:text-zinc-600 text-white py-3 rounded-xl text-sm font-semibold transition-all shadow-lg shadow-blue-900/10 dark:shadow-blue-950/50"
            >
              {loading ? 'Connecting…' : 'Connect'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
