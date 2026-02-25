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
        <div className="relative max-w-[78%]">
          <div className="bg-blue-600 text-white px-4 py-2.5 rounded-2xl rounded-tr-sm text-sm whitespace-pre-wrap shadow-md shadow-blue-900/20 dark:shadow-blue-950/40">
            {message.text}
          </div>
          {showTime && (
            <div className="absolute right-0 -bottom-5 text-[10px] text-gray-400 dark:text-zinc-600 whitespace-nowrap">
              {time}
            </div>
          )}
        </div>
      </div>
    )
  }

  return (
    <div
      className="flex items-start gap-2.5"
      onMouseEnter={() => setShowTime(true)}
      onMouseLeave={() => setShowTime(false)}
    >
      <div className="w-7 h-7 rounded-full overflow-hidden shrink-0 mt-0.5 ring-1 ring-gray-200 dark:ring-white/10">
        <img src="/chat/avatar.png" alt="bot" className="w-full h-full object-cover" />
      </div>
      <div className="relative max-w-[78%] bg-white dark:bg-zinc-900 border border-gray-200 dark:border-white/8 px-4 py-2.5 rounded-2xl rounded-tl-sm text-sm shadow-sm">
        <div className="prose prose-sm dark:prose-invert max-w-none">
          <ReactMarkdown>{message.text}</ReactMarkdown>
        </div>
        {showTime && (
          <div className="absolute left-0 -bottom-5 text-[10px] text-gray-400 dark:text-zinc-600 whitespace-nowrap">
            {time}
          </div>
        )}
      </div>
    </div>
  )
}
