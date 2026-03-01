import { useAtom, useAtomValue } from 'jotai'
import { useEffect, useState } from 'react'
import { apiKeyAtom, wishlistDialogOpenAtom } from '../atoms'
import { fetchWishes, addWish, markWishDone, deleteWish, type Wish } from '../api'

function formatAge(iso: string): string {
  const d = new Date(iso)
  const now = new Date()
  const diffMs = now.getTime() - d.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  if (diffMins < 1) return 'just now'
  if (diffMins < 60) return `${diffMins}m ago`
  const diffHours = Math.floor(diffMins / 60)
  if (diffHours < 24) return `${diffHours}h ago`
  const diffDays = Math.floor(diffHours / 24)
  if (diffDays < 30) return `${diffDays}d ago`
  return d.toLocaleDateString()
}

export default function WishlistDialog() {
  const [open, setOpen] = useAtom(wishlistDialogOpenAtom)
  const apiKey = useAtomValue(apiKeyAtom)

  const [wishes, setWishes] = useState<Wish[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [showDone, setShowDone] = useState(false)

  const [newMsg, setNewMsg] = useState('')
  const [adding, setAdding] = useState(false)
  const [addError, setAddError] = useState<string | null>(null)

  const load = () => {
    setLoading(true)
    setError(null)
    fetchWishes(apiKey)
      .then(setWishes)
      .catch(e => setError(String(e)))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    if (open) load()
  }, [open]) // eslint-disable-line react-hooks/exhaustive-deps

  async function handleAdd(e: React.FormEvent) {
    e.preventDefault()
    const msg = newMsg.trim()
    if (!msg) return
    setAdding(true)
    setAddError(null)
    try {
      await addWish(apiKey, msg)
      setNewMsg('')
      load()
    } catch (err) {
      setAddError(String(err))
    } finally {
      setAdding(false)
    }
  }

  async function handleDone(w: Wish) {
    try {
      await markWishDone(apiKey, w.id)
      setWishes(prev => prev.map(x => x.id === w.id ? { ...x, done: true } : x))
    } catch { /* ignore */ }
  }

  async function handleDelete(id: number) {
    try {
      await deleteWish(apiKey, id)
      setWishes(prev => prev.filter(x => x.id !== id))
    } catch { /* ignore */ }
  }

  if (!open) return null

  const open_wishes = wishes.filter(w => !w.done)
  const done_wishes = wishes.filter(w => w.done)
  const visible = showDone ? wishes : open_wishes

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 dark:bg-black/60"
      onClick={e => { if (e.target === e.currentTarget) setOpen(false) }}
    >
      <div className="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-xl shadow-2xl w-full max-w-lg mx-4 flex flex-col max-h-[80vh]">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-zinc-700 shrink-0">
          <div>
            <h2 className="text-base font-semibold text-gray-900 dark:text-zinc-100">Wishlist</h2>
            <p className="text-xs text-gray-500 dark:text-zinc-500 mt-0.5">
              {open_wishes.length} open
              {done_wishes.length > 0 && ` · ${done_wishes.length} done`}
            </p>
          </div>
          <button
            onClick={() => setOpen(false)}
            className="text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300 text-xl leading-none transition-colors"
          >
            ×
          </button>
        </div>

        {/* Add form */}
        <form onSubmit={handleAdd} className="px-5 py-3 border-b border-gray-200 dark:border-zinc-700 shrink-0 flex gap-2">
          <input
            type="text"
            value={newMsg}
            onChange={e => setNewMsg(e.target.value)}
            placeholder="Add a feature request or idea…"
            className="flex-1 bg-gray-50 dark:bg-zinc-800 border border-gray-200 dark:border-zinc-700 rounded-lg px-3 py-2 text-sm text-gray-900 dark:text-zinc-100 placeholder-gray-400 dark:placeholder-zinc-500 outline-none focus:border-blue-400 dark:focus:border-blue-500 transition-colors"
          />
          <button
            type="submit"
            disabled={adding || !newMsg.trim()}
            className="px-4 py-2 text-sm font-medium bg-blue-600 hover:bg-blue-500 disabled:bg-gray-100 dark:disabled:bg-zinc-800 disabled:text-gray-400 dark:disabled:text-zinc-600 text-white rounded-lg transition-colors shrink-0"
          >
            {adding ? '…' : 'Add'}
          </button>
        </form>
        {addError && <p className="px-5 py-1 text-xs text-red-600 dark:text-red-400">{addError}</p>}

        {/* List */}
        <div className="flex-1 overflow-y-auto p-4 flex flex-col gap-2">
          {error && (
            <div className="text-xs text-red-600 dark:text-red-400 px-1">
              {error}{' '}
              <button onClick={load} className="underline hover:opacity-70">Retry</button>
            </div>
          )}
          {loading && <p className="text-sm text-gray-400 dark:text-zinc-500 text-center py-6">Loading…</p>}
          {!loading && open_wishes.length === 0 && !error && (
            <p className="text-sm text-gray-400 dark:text-zinc-500 text-center py-6">No open wishes yet.</p>
          )}

          {visible.map(w => (
            <div
              key={w.id}
              className={`flex items-start gap-3 px-3 py-2.5 rounded-lg border transition-colors ${
                w.done
                  ? 'bg-gray-50 dark:bg-zinc-800/40 border-gray-100 dark:border-zinc-800 opacity-60'
                  : 'bg-white dark:bg-zinc-800/60 border-gray-200 dark:border-zinc-700'
              }`}
            >
              {/* Done toggle */}
              <button
                onClick={() => !w.done && handleDone(w)}
                className={`mt-0.5 w-4 h-4 shrink-0 rounded border flex items-center justify-center transition-colors ${
                  w.done
                    ? 'bg-green-500 border-green-500 text-white'
                    : 'border-gray-300 dark:border-zinc-600 hover:border-green-400 dark:hover:border-green-500'
                }`}
                title={w.done ? 'Done' : 'Mark done'}
              >
                {w.done && <span className="text-[10px] leading-none">✓</span>}
              </button>

              {/* Message */}
              <p className={`flex-1 text-sm leading-relaxed ${w.done ? 'line-through text-gray-400 dark:text-zinc-600' : 'text-gray-800 dark:text-zinc-200'}`}>
                {w.message}
              </p>

              {/* Meta + delete */}
              <div className="flex items-center gap-2 shrink-0">
                <span className="text-xs text-gray-400 dark:text-zinc-600">{formatAge(w.created_at)}</span>
                <button
                  onClick={() => handleDelete(w.id)}
                  className="text-gray-300 dark:text-zinc-700 hover:text-red-400 dark:hover:text-red-500 transition-colors text-xs"
                  title="Delete"
                >
                  ✕
                </button>
              </div>
            </div>
          ))}

          {done_wishes.length > 0 && (
            <button
              onClick={() => setShowDone(v => !v)}
              className="text-xs text-gray-400 dark:text-zinc-600 hover:text-gray-600 dark:hover:text-zinc-400 transition-colors text-center pt-1"
            >
              {showDone ? 'Hide done' : `Show ${done_wishes.length} done`}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
