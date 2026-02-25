import { useAtom, useAtomValue } from 'jotai'
import { useEffect, useRef, useState } from 'react'
import { apiKeyAtom, currentProjectAtom, filesDialogOpenAtom } from '../atoms'
import {
  fetchFiles,
  fetchFileContent,
  saveFileContent,
  fileDownloadURL,
  type ProjectFile,
} from '../api'

function humanSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function formatAge(iso: string): string {
  const d = new Date(iso)
  const now = new Date()
  const diffMs = now.getTime() - d.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  if (diffMins < 1) return 'just now'
  if (diffMins < 60) return `${diffMins}m ago`
  const diffHours = Math.floor(diffMins / 60)
  if (diffHours < 24) return `${diffHours}h ago`
  return `${Math.floor(diffHours / 24)}d ago`
}

function fileIcon(filename: string): string {
  const ext = filename.split('.').pop()?.toLowerCase() ?? ''
  if (['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg'].includes(ext)) return '🖼'
  if (['pdf'].includes(ext)) return '📕'
  if (['md', 'txt'].includes(ext)) return '📝'
  if (['json', 'yaml', 'yml', 'toml', 'xml'].includes(ext)) return '⚙️'
  if (['csv'].includes(ext)) return '📊'
  if (['py', 'js', 'ts', 'go', 'sh', 'bash'].includes(ext)) return '💻'
  return '📄'
}

export default function FilesDialog() {
  const [open, setOpen] = useAtom(filesDialogOpenAtom)
  const apiKey = useAtomValue(apiKeyAtom)
  const currentProject = useAtomValue(currentProjectAtom)

  const [files, setFiles] = useState<ProjectFile[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [selected, setSelected] = useState<ProjectFile | null>(null)
  const [content, setContent] = useState<string>('')
  const [contentLoading, setContentLoading] = useState(false)
  const [contentError, setContentError] = useState<string | null>(null)
  const [edited, setEdited] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [savedOk, setSavedOk] = useState(false)

  const textareaRef = useRef<HTMLTextAreaElement>(null)

  const load = () => {
    setLoading(true)
    setError(null)
    fetchFiles(apiKey)
      .then(setFiles)
      .catch(e => setError(String(e)))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    if (open) {
      load()
      setSelected(null)
      setContent('')
      setEdited(false)
    }
  }, [open]) // eslint-disable-line react-hooks/exhaustive-deps

  async function selectFile(f: ProjectFile) {
    setSelected(f)
    setEdited(false)
    setSaveError(null)
    setSavedOk(false)
    if (!f.is_text) {
      setContent('')
      return
    }
    setContentLoading(true)
    setContentError(null)
    try {
      const text = await fetchFileContent(apiKey, f.id)
      setContent(text)
    } catch (e) {
      setContentError(String(e))
    } finally {
      setContentLoading(false)
    }
  }

  async function handleSave() {
    if (!selected) return
    setSaving(true)
    setSaveError(null)
    setSavedOk(false)
    try {
      await saveFileContent(apiKey, selected.id, content)
      setEdited(false)
      setSavedOk(true)
      load()
    } catch (e) {
      setSaveError(String(e))
    } finally {
      setSaving(false)
    }
  }

  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 dark:bg-black/60"
      onClick={e => { if (e.target === e.currentTarget) setOpen(false) }}
    >
      <div className="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-700 rounded-xl shadow-2xl w-full max-w-3xl mx-4 flex flex-col"
           style={{ height: '80vh' }}>
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-zinc-700 shrink-0">
          <div>
            <h2 className="text-base font-semibold text-gray-900 dark:text-zinc-100">Files</h2>
            <p className="text-xs text-gray-500 dark:text-zinc-500 mt-0.5">
              Project: <span className="text-gray-700 dark:text-zinc-300">{currentProject}</span>
            </p>
          </div>
          <button
            onClick={() => setOpen(false)}
            className="text-gray-400 dark:text-zinc-500 hover:text-gray-600 dark:hover:text-zinc-300 text-xl leading-none transition-colors"
          >
            ×
          </button>
        </div>

        {/* Body: two-pane layout */}
        <div className="flex flex-1 min-h-0">
          {/* File list */}
          <div className="w-64 shrink-0 border-r border-gray-200 dark:border-zinc-700 flex flex-col">
            <div className="flex-1 overflow-y-auto py-2">
              {error && (
                <div className="mx-3 my-2 px-3 py-2 rounded-lg bg-red-50 dark:bg-red-950/50 border border-red-200 dark:border-red-900/50 text-xs text-red-600 dark:text-red-400">
                  {error}{' '}
                  <button onClick={load} className="underline hover:opacity-70">Retry</button>
                </div>
              )}
              {loading && (
                <p className="px-4 py-4 text-sm text-gray-400 dark:text-zinc-500">Loading...</p>
              )}
              {!loading && files.length === 0 && !error && (
                <p className="px-4 py-4 text-sm text-gray-400 dark:text-zinc-500">No files yet.</p>
              )}
              {files.map(f => (
                <button
                  key={f.id}
                  onClick={() => selectFile(f)}
                  className={`w-full text-left px-3 py-2 flex items-start gap-2 transition-colors ${
                    selected?.id === f.id
                      ? 'bg-blue-50 dark:bg-zinc-800'
                      : 'hover:bg-gray-50 dark:hover:bg-zinc-800/50'
                  }`}
                >
                  <span className="text-base shrink-0 mt-0.5">{fileIcon(f.filename)}</span>
                  <div className="min-w-0">
                    <p className={`text-sm truncate ${selected?.id === f.id ? 'text-gray-900 dark:text-zinc-100' : 'text-gray-700 dark:text-zinc-300'}`}>
                      {f.filename}
                    </p>
                    <p className="text-xs text-gray-400 dark:text-zinc-600">{humanSize(f.size)} · {formatAge(f.created_at)}</p>
                  </div>
                </button>
              ))}
            </div>
          </div>

          {/* Content pane */}
          <div className="flex-1 flex flex-col min-w-0">
            {!selected && (
              <div className="flex-1 flex items-center justify-center">
                <p className="text-sm text-gray-400 dark:text-zinc-600">Select a file to view</p>
              </div>
            )}

            {selected && (
              <>
                {/* File toolbar */}
                <div className="flex items-center justify-between px-4 py-2 border-b border-gray-200 dark:border-zinc-700 shrink-0 gap-3">
                  <span className="text-sm text-gray-700 dark:text-zinc-300 truncate">{selected.filename}</span>
                  <div className="flex items-center gap-2 shrink-0">
                    {edited && (
                      <>
                        {saveError && <span className="text-xs text-red-600 dark:text-red-400">{saveError}</span>}
                        {savedOk && <span className="text-xs text-green-600 dark:text-green-400">Saved ✓</span>}
                        <button
                          onClick={handleSave}
                          disabled={saving}
                          className="px-3 py-1 text-xs bg-blue-600 hover:bg-blue-500 disabled:bg-gray-200 dark:disabled:bg-zinc-700 text-white rounded transition-colors"
                        >
                          {saving ? 'Saving…' : 'Save'}
                        </button>
                      </>
                    )}
                    <a
                      href={fileDownloadURL(selected.id)}
                      download={selected.filename}
                      className="px-3 py-1 text-xs bg-gray-100 dark:bg-zinc-700 hover:bg-gray-200 dark:hover:bg-zinc-600 text-gray-700 dark:text-zinc-200 rounded transition-colors"
                    >
                      Download
                    </a>
                  </div>
                </div>

                {/* Content area */}
                <div className="flex-1 min-h-0 overflow-auto">
                  {contentLoading && (
                    <p className="p-4 text-sm text-gray-400 dark:text-zinc-500">Loading…</p>
                  )}
                  {contentError && (
                    <p className="p-4 text-sm text-red-600 dark:text-red-400">{contentError}</p>
                  )}
                  {!contentLoading && !contentError && selected.is_text && (
                    <textarea
                      ref={textareaRef}
                      value={content}
                      onChange={e => { setContent(e.target.value); setEdited(true); setSavedOk(false) }}
                      spellCheck={false}
                      className="w-full h-full min-h-full bg-gray-50 dark:bg-zinc-950 text-gray-800 dark:text-zinc-200 text-xs font-mono p-4 resize-none outline-none border-none"
                    />
                  )}
                  {!contentLoading && !contentError && !selected.is_text && (
                    <div className="p-6 flex flex-col items-center justify-center gap-3 text-center">
                      <span className="text-4xl">{fileIcon(selected.filename)}</span>
                      <p className="text-sm text-gray-500 dark:text-zinc-400">Binary file — not viewable as text.</p>
                      <a
                        href={fileDownloadURL(selected.id)}
                        download={selected.filename}
                        className="px-4 py-2 text-sm bg-gray-100 dark:bg-zinc-700 hover:bg-gray-200 dark:hover:bg-zinc-600 text-gray-700 dark:text-zinc-200 rounded-lg transition-colors"
                      >
                        Download {selected.filename}
                      </a>
                    </div>
                  )}
                </div>
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
