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
    <div className="min-h-screen bg-zinc-900 flex items-center justify-center p-4">
      <div className="w-full max-w-sm">
        <form onSubmit={handleSubmit} className="flex flex-col items-center gap-5">
          <img
            src="/chat/avatar.png"
            alt="Artoo"
            className="w-20 h-20 rounded-full object-cover ring-2 ring-zinc-700"
          />
          <h1 className="text-xl font-semibold text-zinc-200">Artoo</h1>
          <div className="w-full flex flex-col gap-3">
            <input
              type="password"
              value={key}
              onChange={e => setKey(e.target.value)}
              placeholder="API key"
              autoComplete="current-password"
              autoFocus
              className="w-full bg-zinc-800 border border-zinc-600 rounded-lg px-4 py-3 text-zinc-100 placeholder-zinc-500 text-sm outline-none focus:border-blue-500"
            />
            {error && <p className="text-red-400 text-sm text-center">{error}</p>}
            <button
              type="submit"
              disabled={loading || !key.trim()}
              className="w-full bg-blue-600 hover:bg-blue-500 disabled:bg-zinc-700 disabled:text-zinc-500 text-white py-3 rounded-lg text-sm font-medium transition-colors"
            >
              {loading ? 'Connecting...' : 'Connect'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
