import { useAtom, useAtomValue, useSetAtom } from 'jotai'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import {
  allMsgsAtom,
  apiKeyAtom,
  connectedAtom,
  sessionIDAtom,
  workingAtom,
  type ChatMessage,
} from '../atoms'
import { sendMessage } from '../api'
import Message from './Message'

function genID() {
  return Math.random().toString(36).slice(2) + Date.now().toString(36)
}

function ThinkingDots() {
  return (
    <div className="flex items-start gap-2.5">
      <div className="w-7 h-7 rounded-full overflow-hidden shrink-0 ring-1 ring-gray-200 dark:ring-white/10">
        <img src="/chat/avatar.png" alt="bot" className="w-full h-full object-cover" />
      </div>
      <div className="flex items-center gap-1.5 px-4 py-3 bg-white dark:bg-zinc-900 border border-gray-200 dark:border-white/8 rounded-2xl rounded-tl-sm shadow-sm">
        <span className="thinking-dot w-1.5 h-1.5 bg-gray-300 dark:bg-zinc-500 rounded-full" style={{ animationDelay: '0ms' }} />
        <span className="thinking-dot w-1.5 h-1.5 bg-gray-300 dark:bg-zinc-500 rounded-full" style={{ animationDelay: '200ms' }} />
        <span className="thinking-dot w-1.5 h-1.5 bg-gray-300 dark:bg-zinc-500 rounded-full" style={{ animationDelay: '400ms' }} />
      </div>
    </div>
  )
}

export default function ChatPane() {
  const { project = 'global' } = useParams<{ project: string }>()
  const decodedProject = decodeURIComponent(project)

  const apiKey = useAtomValue(apiKeyAtom)
  const [allMsgs, setAllMsgs] = useAtom(allMsgsAtom)
  const [sessionID, setSessionID] = useAtom(sessionIDAtom)
  const [connected, setConnected] = useAtom(connectedAtom)
  const [working, setWorking] = useAtom(workingAtom)
  const [everConnected, setEverConnected] = useState(false)

  const msgs = allMsgs[decodedProject] ?? []
  const [input, setInput] = useState('')

  const bottomRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const projectRef = useRef(decodedProject)

  useEffect(() => {
    projectRef.current = decodedProject
  }, [decodedProject])

  const appendMsg = useCallback(
    (msg: ChatMessage) => {
      setAllMsgs(prev => {
        const proj = projectRef.current
        const existing = prev[proj] ?? []
        return { ...prev, [proj]: [...existing, msg] }
      })
    },
    [setAllMsgs],
  )

  useEffect(() => {
    if (!apiKey) return

    const es = new EventSource(`/chat/sse?key=${encodeURIComponent(apiKey)}`)

    es.onopen = () => {
      setConnected(true)
      setEverConnected(true)
    }
    es.onerror = () => setConnected(false)

    es.addEventListener('session', (e: MessageEvent) => {
      setSessionID(e.data)
    })

    es.addEventListener('message', (e: MessageEvent) => {
      const text = (e.data as string).replace(/\r/g, '\n')
      appendMsg({ id: genID(), role: 'bot', text, ts: Date.now() })
      setWorking(false)
    })

    return () => {
      es.close()
      setConnected(false)
    }
  }, [apiKey]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [msgs, working])

  useEffect(() => {
    const ta = textareaRef.current
    if (!ta) return
    ta.style.height = 'auto'
    ta.style.height = `${Math.min(ta.scrollHeight, 160)}px`
  }, [input])

  async function handleSend() {
    const text = input.trim()
    if (!text || !sessionID || working) return

    setInput('')
    appendMsg({ id: genID(), role: 'user', text, ts: Date.now() })
    setWorking(true)

    try {
      await sendMessage(apiKey, text, sessionID)
    } catch (e) {
      appendMsg({ id: genID(), role: 'bot', text: `Error: ${String(e)}`, ts: Date.now() })
      setWorking(false)
    }
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  const canSend = Boolean(sessionID && input.trim() && !working)

  const statusText = working
    ? 'Working…'
    : connected
    ? ''
    : everConnected
    ? 'Disconnected — reload to reconnect'
    : 'Connecting…'

  const statusColor = working
    ? 'text-blue-500 dark:text-blue-400'
    : everConnected && !connected
    ? 'text-red-500 dark:text-red-400'
    : 'text-gray-400 dark:text-zinc-500'

  return (
    <div className="flex flex-col flex-1 min-w-0 bg-gray-50 dark:bg-zinc-950">
      {/* Header */}
      <div className="px-5 py-3 border-b border-gray-200 dark:border-white/5 bg-white/80 dark:bg-zinc-900/50 flex items-center gap-3 shrink-0">
        <div className="flex-1 min-w-0">
          <h2 className="text-sm font-semibold text-gray-900 dark:text-zinc-200 truncate">{decodedProject}</h2>
          {statusText && (
            <p className={`text-xs mt-0.5 ${statusColor}`}>{statusText}</p>
          )}
        </div>
        <div className={`w-2 h-2 rounded-full shrink-0 transition-colors ${connected ? 'bg-green-400' : 'bg-gray-300 dark:bg-zinc-700'}`} />
      </div>

      {/* Messages */}
      <div className="flex-1 overflow-y-auto px-5 py-6 flex flex-col gap-5">
        {msgs.length === 0 && (
          <div className="flex-1 flex flex-col items-center justify-center gap-4 text-center py-16">
            <div className="relative">
              <div className="absolute inset-0 rounded-full bg-blue-500/10 blur-2xl scale-150" />
              <img src="/chat/avatar.png" alt="Artoo" className="relative w-12 h-12 rounded-full object-cover opacity-70" />
            </div>
            <div>
              <p className="text-gray-500 dark:text-zinc-400 text-sm font-medium">How can I help?</p>
              <p className="text-gray-400 dark:text-zinc-600 text-xs mt-1">{decodedProject}</p>
            </div>
          </div>
        )}
        {msgs.map(m => (
          <Message key={m.id} message={m} />
        ))}
        {working && <ThinkingDots />}
        <div ref={bottomRef} />
      </div>

      {/* Input */}
      <div className="px-4 py-4 border-t border-gray-200 dark:border-white/5 bg-white/80 dark:bg-zinc-900/50 shrink-0">
        <div className="flex gap-2 items-end bg-white dark:bg-zinc-900 border border-gray-200 dark:border-white/8 rounded-2xl px-3 py-2 shadow-sm focus-within:border-blue-400 dark:focus-within:border-blue-500/40 focus-within:ring-2 focus-within:ring-blue-500/10 transition-all">
          <textarea
            ref={textareaRef}
            value={input}
            onChange={e => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={`Message ${decodedProject}…`}
            rows={1}
            className="flex-1 bg-transparent text-sm resize-none outline-none text-gray-900 dark:text-zinc-100 placeholder-gray-400 dark:placeholder-zinc-600 min-h-[28px] max-h-40 py-1"
          />
          <button
            onClick={handleSend}
            disabled={!canSend}
            className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 disabled:bg-gray-100 dark:disabled:bg-zinc-800 disabled:text-gray-400 dark:disabled:text-zinc-600 text-white rounded-xl text-xs font-semibold transition-all shrink-0 self-end"
          >
            Send
          </button>
        </div>
        <p className="text-center text-[10px] text-gray-300 dark:text-zinc-700 mt-2">Enter to send · Shift+Enter for newline</p>
      </div>
    </div>
  )
}
