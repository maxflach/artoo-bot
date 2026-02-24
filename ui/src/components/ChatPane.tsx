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

export default function ChatPane() {
  const { project = 'global' } = useParams<{ project: string }>()
  const decodedProject = decodeURIComponent(project)

  const apiKey = useAtomValue(apiKeyAtom)
  const [allMsgs, setAllMsgs] = useAtom(allMsgsAtom)
  const [sessionID, setSessionID] = useAtom(sessionIDAtom)
  const [connected, setConnected] = useAtom(connectedAtom)
  const [working, setWorking] = useAtom(workingAtom)

  const msgs = allMsgs[decodedProject] ?? []
  const [input, setInput] = useState('')

  const bottomRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const projectRef = useRef(decodedProject)

  // Keep projectRef in sync for the SSE message handler
  useEffect(() => {
    projectRef.current = decodedProject
  }, [decodedProject])

  // Append a message to the active project's history
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

  // Open SSE connection once per API key; keeps alive across project switches
  useEffect(() => {
    if (!apiKey) return

    const es = new EventSource(`/chat/sse?key=${encodeURIComponent(apiKey)}`)

    es.onopen = () => setConnected(true)
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

  // Auto-scroll on new messages or working state change
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [msgs, working])

  // Auto-resize textarea
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

  return (
    <div className="flex flex-col flex-1 min-w-0">
      {/* Status bar */}
      <div className="px-4 py-2 text-xs border-b border-zinc-700 bg-zinc-800 flex items-center gap-2 shrink-0">
        <span className={`w-2 h-2 rounded-full shrink-0 ${connected ? 'bg-green-500' : 'bg-red-500'}`} />
        <span className="text-zinc-400">
          {working ? 'Working...' : connected ? 'Connected' : 'Disconnected — reload to reconnect'}
        </span>
        <span className="ml-auto text-zinc-600 truncate">{decodedProject}</span>
      </div>

      {/* Messages */}
      <div className="flex-1 overflow-y-auto p-4 flex flex-col gap-4">
        {msgs.length === 0 && (
          <div className="flex-1 flex items-center justify-center">
            <p className="text-zinc-600 text-sm">No messages yet. Say something!</p>
          </div>
        )}
        {msgs.map(m => (
          <Message key={m.id} message={m} />
        ))}
        {working && (
          <div className="flex items-center gap-2 text-zinc-500 text-sm">
            <div className="w-7 h-7 rounded-full overflow-hidden shrink-0">
              <img src="/chat/avatar.png" alt="bot" className="w-full h-full object-cover" />
            </div>
            <span className="animate-pulse">Thinking...</span>
          </div>
        )}
        <div ref={bottomRef} />
      </div>

      {/* Input */}
      <div className="p-3 border-t border-zinc-700 flex gap-2 items-end shrink-0">
        <textarea
          ref={textareaRef}
          value={input}
          onChange={e => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={`Message ${decodedProject}...`}
          rows={1}
          className="flex-1 bg-zinc-800 border border-zinc-600 rounded-lg px-3 py-2 text-sm resize-none outline-none focus:border-blue-500 text-zinc-100 placeholder-zinc-500 min-h-[40px] max-h-40 transition-colors"
        />
        <button
          onClick={handleSend}
          disabled={!canSend}
          className="px-4 py-2 bg-blue-600 hover:bg-blue-500 disabled:bg-zinc-700 disabled:text-zinc-500 text-white rounded-lg text-sm font-medium transition-colors shrink-0 h-[40px]"
        >
          Send
        </button>
      </div>
    </div>
  )
}
