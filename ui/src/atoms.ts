import { atom } from 'jotai'
import { atomWithStorage } from 'jotai/utils'
import type { Project } from './api'

export interface ChatMessage {
  id: string
  role: 'user' | 'bot'
  text: string
  ts: number
}

// Persisted to localStorage.
// getOnInit: true makes Jotai read from storage immediately on atom init (first render)
// rather than starting with the default value and patching in onMount (after paint).
export const apiKeyAtom = atomWithStorage<string>('webchat_key', '', undefined, { getOnInit: true })
// Messages keyed by project name so each project has its own history
export const allMsgsAtom = atomWithStorage<Record<string, ChatMessage[]>>(
  'webchat_msgs',
  {},
  undefined,
  { getOnInit: true },
)

// In-memory only (reset on reload)
export const sessionIDAtom = atom<string>('')
export const connectedAtom = atom<boolean>(false)
export const projectsAtom = atom<Project[]>([])
export const workingAtom = atom<boolean>(false)
export const scheduleDialogOpenAtom = atom<boolean>(false)
export const filesDialogOpenAtom = atom<boolean>(false)
export const currentProjectAtom = atom<string>('global')
