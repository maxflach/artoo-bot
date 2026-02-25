export interface Project {
  name: string
  title: string
  type: 'local' | 'external' | 'shared'
}

export interface ProjectsResponse {
  active: string
  projects: Project[]
}

export async function fetchProjects(key: string): Promise<ProjectsResponse> {
  const r = await fetch('/chat/projects', {
    headers: { Authorization: `Bearer ${key}` },
  })
  if (!r.ok) throw new Error(`${r.status}`)
  return r.json()
}

export async function switchProject(
  key: string,
  project: string,
): Promise<{ ok: boolean; name: string; title: string }> {
  const r = await fetch('/chat/switch', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${key}` },
    body: JSON.stringify({ project }),
  })
  if (!r.ok) throw new Error(`${r.status}`)
  return r.json()
}

export async function validateKey(key: string): Promise<boolean> {
  const r = await fetch('/v1/health', {
    headers: { Authorization: `Bearer ${key}` },
  })
  return r.status !== 401
}

export async function sendMessage(key: string, text: string, sessionID: string): Promise<void> {
  await fetch('/chat/message', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${key}` },
    body: JSON.stringify({ text, session_id: sessionID }),
  })
}

export interface Schedule {
  id: number
  name: string
  schedule: string
  prompt: string
  workspace: string
  one_shot: boolean
  enabled: boolean
  last_run: string | null
}

export async function fetchSchedules(key: string): Promise<Schedule[]> {
  const r = await fetch('/chat/schedules', {
    headers: { Authorization: `Bearer ${key}` },
  })
  if (!r.ok) throw new Error(`${r.status}`)
  const data = await r.json()
  return data.schedules ?? []
}

export async function createSchedule(
  key: string,
  name: string,
  when: string,
  prompt: string,
  oneShot: boolean,
): Promise<{ desc: string }> {
  const r = await fetch('/chat/schedules', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${key}` },
    body: JSON.stringify({ name, when, prompt, one_shot: oneShot }),
  })
  if (!r.ok) {
    const err = await r.json().catch(() => ({ error: r.statusText }))
    throw new Error(err.error ?? r.statusText)
  }
  return r.json()
}

export async function deleteSchedule(key: string, id: number): Promise<void> {
  const r = await fetch(`/chat/schedules/${id}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${key}` },
  })
  if (!r.ok) throw new Error(`${r.status}`)
}

export interface ProjectFile {
  id: number
  filename: string
  size: number
  created_at: string
  workspace: string
  is_text: boolean
}

export async function fetchFiles(key: string): Promise<ProjectFile[]> {
  const r = await fetch('/chat/files', {
    headers: { Authorization: `Bearer ${key}` },
  })
  if (!r.ok) throw new Error(`${r.status}`)
  const data = await r.json()
  return data.files ?? []
}

export async function fetchFileContent(key: string, id: number): Promise<string> {
  const r = await fetch(`/chat/files/${id}/content`, {
    headers: { Authorization: `Bearer ${key}` },
  })
  if (!r.ok) throw new Error(`${r.status}`)
  return r.text()
}

export async function saveFileContent(key: string, id: number, content: string): Promise<void> {
  const r = await fetch(`/chat/files/${id}/content`, {
    method: 'PUT',
    headers: { Authorization: `Bearer ${key}` },
    body: content,
  })
  if (!r.ok) throw new Error(`${r.status}`)
}

export async function deleteFile(key: string, id: number): Promise<void> {
  const r = await fetch(`/chat/files/${id}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${key}` },
  })
  if (!r.ok) throw new Error(`${r.status}`)
}

export async function downloadFile(key: string, id: number, filename: string): Promise<void> {
  const r = await fetch(`/chat/files/${id}`, {
    headers: { Authorization: `Bearer ${key}` },
  })
  if (!r.ok) throw new Error(`${r.status}`)
  const blob = await r.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}
