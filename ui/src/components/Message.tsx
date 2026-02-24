import { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import type { ChatMessage } from '../atoms'

interface Props {
  message: ChatMessage
}

export default function Message({ message }: Props) {
  const [showTime, setShowTime] = useState(false)
  const time = new Date(message.ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })

  if (message.role === 'user') {
    return (
      <div
        className="flex justify-end"
        onMouseEnter={() => setShowTime(true)}
        onMouseLeave={() => setShowTime(false)}
      >
        <div className="relative max-w-[80%]">
          <div className="bg-blue-700 text-zinc-100 px-3 py-2 rounded-2xl rounded-tr-sm text-sm whitespace-pre-wrap">
            {message.text}
          </div>
          {showTime && (
            <div className="absolute right-0 -bottom-5 text-xs text-zinc-600 whitespace-nowrap">
              {time}
            </div>
          )}
        </div>
      </div>
    )
  }

  return (
    <div
      className="flex items-start gap-2"
      onMouseEnter={() => setShowTime(true)}
      onMouseLeave={() => setShowTime(false)}
    >
      <div className="w-7 h-7 rounded-full overflow-hidden shrink-0 mt-0.5">
        <img src="/chat/avatar.png" alt="bot" className="w-full h-full object-cover" />
      </div>
      <div className="relative max-w-[80%] bg-zinc-800 border border-zinc-700 px-3 py-2 rounded-2xl rounded-tl-sm text-sm">
        <div className="prose prose-sm prose-invert max-w-none">
          <ReactMarkdown>{message.text}</ReactMarkdown>
        </div>
        {showTime && (
          <div className="absolute left-0 -bottom-5 text-xs text-zinc-600 whitespace-nowrap">
            {time}
          </div>
        )}
      </div>
    </div>
  )
}
