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
