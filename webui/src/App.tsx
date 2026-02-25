import { useEffect, useMemo, useState } from 'react'

type ChatItem = { role: 'user' | 'assistant'; text: string }
type Session = { key: string; title: string }
type CronJob = { id: string; name: string; enabled: boolean; schedule?: { kind?: string } }

const defaultSessions: Session[] = [{ key: 'webui:default', title: 'Default' }]

export function App() {
  const [token, setToken] = useState('')
  const [cfgText, setCfgText] = useState('{}')
  const [sessions, setSessions] = useState<Session[]>(defaultSessions)
  const [active, setActive] = useState('webui:default')
  const [chat, setChat] = useState<Record<string, ChatItem[]>>({ 'webui:default': [] })
  const [msg, setMsg] = useState('')
  const [nodes, setNodes] = useState<string>('[]')
  const [cron, setCron] = useState<CronJob[]>([])
  const activeChat = useMemo(() => chat[active] || [], [chat, active])

  const q = token ? `?token=${encodeURIComponent(token)}` : ''

  async function loadConfig() {
    const r = await fetch(`/webui/api/config${q}`)
    setCfgText(await r.text())
  }

  async function saveConfig() {
    const parsed = JSON.parse(cfgText)
    const r = await fetch(`/webui/api/config${q}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(parsed),
    })
    alert(await r.text())
  }

  async function refreshNodes() {
    const r = await fetch(`/webui/api/nodes${q}`)
    const j = await r.json()
    setNodes(JSON.stringify(j.nodes || [], null, 2))
  }

  async function refreshCron() {
    const r = await fetch(`/webui/api/cron${q}`)
    const j = await r.json()
    setCron(j.jobs || [])
  }

  async function cronAction(action: 'delete' | 'enable' | 'disable', id: string) {
    await fetch(`/webui/api/cron${q}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action, id }),
    })
    await refreshCron()
  }

  async function send() {
    let media = ''
    const input = document.getElementById('file') as HTMLInputElement | null
    const f = input?.files?.[0]
    if (f) {
      const fd = new FormData()
      fd.append('file', f)
      const ur = await fetch(`/webui/api/upload${q}`, { method: 'POST', body: fd })
      const uj = await ur.json()
      media = uj.path || ''
    }

    const userText = msg + (media ? ` [file:${media}]` : '')
    setChat((prev) => ({ ...prev, [active]: [...(prev[active] || []), { role: 'user', text: userText }] }))
    const payload = { session: active, message: msg, media }
    setMsg('')

    const r = await fetch(`/webui/api/chat${q}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    })
    const t = await r.text()
    setChat((prev) => ({ ...prev, [active]: [...(prev[active] || []), { role: 'assistant', text: t }] }))
    if (input) input.value = ''
  }

  function addSession() {
    const n = `webui:${Date.now()}`
    const s = { key: n, title: `Session-${sessions.length + 1}` }
    setSessions((v) => [...v, s])
    setActive(n)
    setChat((prev) => ({ ...prev, [n]: [] }))
  }

  useEffect(() => {
    loadConfig().catch(() => {})
    refreshNodes().catch(() => {})
    refreshCron().catch(() => {})
  }, [])

  return (
    <div className="app">
      <header className="topbar">
        <strong>ClawGo WebUI (React/Vite)</strong>
        <input value={token} onChange={(e) => setToken(e.target.value)} placeholder="gateway token" />
      </header>
      <div className="layout">
        <aside className="panel sessions">
          <div className="panel-title">Sessions <button onClick={addSession}>+</button></div>
          {sessions.map((s) => (
            <button key={s.key} className={s.key === active ? 'active' : ''} onClick={() => setActive(s.key)}>
              {s.title}
            </button>
          ))}
        </aside>

        <main className="panel chat">
          <div className="panel-title">Chat</div>
          <div className="chatlog">
            {activeChat.map((m, i) => (
              <div key={i} className={`bubble ${m.role}`}>
                {m.text}
              </div>
            ))}
          </div>
          <div className="composer">
            <input value={msg} onChange={(e) => setMsg(e.target.value)} placeholder="Type message..." />
            <input id="file" type="file" />
            <button onClick={send}>Send</button>
          </div>
        </main>

        <section className="panel right">
          <div className="panel-title">Config</div>
          <div className="row"><button onClick={loadConfig}>Load</button><button onClick={saveConfig}>Save+Reload</button></div>
          <textarea value={cfgText} onChange={(e) => setCfgText(e.target.value)} />
          <div className="panel-title">Cron</div>
          <div className="row"><button onClick={refreshCron}>Refresh</button></div>
          <div className="cron-list">
            {cron.map((j) => (
              <div key={j.id} className="cron-item">
                <div><strong>{j.name || j.id}</strong><div className="muted">{j.id}</div></div>
                <div className="row">
                  <button onClick={() => cronAction(j.enabled ? 'disable' : 'enable', j.id)}>{j.enabled ? 'Disable' : 'Enable'}</button>
                  <button onClick={() => cronAction('delete', j.id)}>Delete</button>
                </div>
              </div>
            ))}
          </div>
          <div className="panel-title">Nodes</div>
          <div className="row"><button onClick={refreshNodes}>Refresh</button></div>
          <pre>{nodes}</pre>
        </section>
      </div>
    </div>
  )
}
