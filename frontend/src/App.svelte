<script>
  import { onMount, tick } from 'svelte'
  import {
    Self, SetName, Peers, History, Status, Scan, AddPeer, ForgetPeer, SendText,
    PickFiles, SendPaths, CancelTransfer, ReceiveDir, ChooseReceiveDir, OpenPath, RevealPath,
  } from '../wailsjs/go/main/App'
  import { EventsOn, ClipboardSetText, OnFileDrop } from '../wailsjs/runtime/runtime'

  let self = $state({ id: '', name: '', os: '', port: 0 })
  let peers = $state([])
  let messages = $state([])
  let selectedId = $state(null)
  let unread = $state({})
  let serviceError = $state('')
  let toast = $state(null)
  let draft = $state('')
  let sending = $state(false)
  let scanning = $state(false)
  let progress = $state({}) // message id -> { done, rate }
  let receiveDir = $state('')

  let editingName = $state(false)
  let nameDraft = $state('')
  let showAdd = $state(false)
  let addAddr = $state('')
  let adding = $state(false)
  let addError = $state('')

  let messagesEl = $state()
  let composerEl = $state()

  const osLabel = { windows: 'Windows', linux: 'Linux', darwin: 'macOS' }

  // Devices we have talked to stay in the list even after they disappear.
  let devices = $derived.by(() => {
    const byId = new Map(peers.map((p) => [p.id, p]))
    for (const m of messages) {
      if (!byId.has(m.peerId)) {
        byId.set(m.peerId, { id: m.peerId, name: m.peerName, os: '', addr: '', online: false, manual: false })
      }
    }
    return [...byId.values()].sort((a, b) => (b.online - a.online) || a.name.localeCompare(b.name))
  })
  let selected = $derived(devices.find((d) => d.id === selectedId) ?? null)
  let thread = $derived(messages.filter((m) => m.peerId === selectedId))

  onMount(async () => {
    ;[self, peers, messages, serviceError, receiveDir] = await Promise.all([
      Self(), Peers(), History(), Status(), ReceiveDir(),
    ])
    peers ??= []
    messages ??= []

    EventsOn('peers', (list) => { peers = list ?? []; scanning = false })
    EventsOn('message', (m) => {
      messages = [...messages, m]
      if (m.incoming && m.peerId !== selectedId) unread[m.peerId] = (unread[m.peerId] ?? 0) + 1
      if (m.peerId === selectedId) scrollToBottom()
    })
    EventsOn('message:update', (m) => {
      messages = messages.map((x) => (x.id === m.id ? m : x))
      if (m.file?.status !== 'active') delete progress[m.id]
    })
    EventsOn('progress', (p) => { progress[p.id] = p })
    EventsOn('error', (e) => { serviceError = e })

    // Only elements with --wails-drop-target: drop accept files.
    OnFileDrop((_x, _y, paths) => {
      if (selectedId && paths?.length) sendPaths(paths)
    }, true)

    if (!selectedId && devices.length) select(devices[0].id)
  })

  async function scrollToBottom() {
    await tick()
    if (messagesEl) messagesEl.scrollTop = messagesEl.scrollHeight
  }

  function select(id) {
    selectedId = id
    unread[id] = 0
    scrollToBottom()
    tick().then(() => composerEl?.focus())
  }

  function showToast(text, kind = 'info') {
    toast = { text, kind }
    clearTimeout(showToast.t)
    showToast.t = setTimeout(() => (toast = null), 3500)
  }

  async function send() {
    const text = draft
    if (!text.trim() || !selected || sending) return
    sending = true
    try {
      await SendText(selected.id, text)
      draft = ''
    } catch (e) {
      showToast(String(e), 'error')
    } finally {
      sending = false
      composerEl?.focus()
    }
  }

  function onComposerKey(e) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      send()
    }
  }

  async function attach() {
    try {
      await PickFiles(selected.id)
    } catch (e) {
      showToast(String(e), 'error')
    }
  }

  async function sendPaths(paths) {
    try {
      await SendPaths(selectedId, paths)
    } catch (e) {
      showToast(String(e), 'error')
    }
  }

  async function changeReceiveDir() {
    try {
      receiveDir = await ChooseReceiveDir()
    } catch (e) {
      showToast(String(e), 'error')
    }
  }

  async function run(fn, path) {
    try {
      await fn(path)
    } catch (e) {
      showToast(String(e), 'error')
    }
  }

  function fmtSize(n) {
    if (n < 1024) return `${n} B`
    const units = ['KB', 'MB', 'GB', 'TB']
    let i = -1
    do { n /= 1024; i++ } while (n >= 1024 && i < units.length - 1)
    return `${n.toFixed(n < 10 ? 1 : 0)} ${units[i]}`
  }

  function pct(done, size) {
    return size ? Math.min(100, (done / size) * 100) : 0
  }

  function extOf(name) {
    const i = name.lastIndexOf('.')
    return i > 0 && name.length - i <= 5 ? name.slice(i + 1).toUpperCase() : 'FILE'
  }

  const statusText = { done: 'done', failed: 'failed', canceled: 'canceled' }

  function refresh() {
    scanning = true
    Scan()
  }

  async function saveName() {
    if (!editingName) return
    editingName = false
    if (nameDraft.trim() === self.name) return
    try {
      await SetName(nameDraft)
      self = await Self()
    } catch (e) {
      showToast(String(e), 'error')
    }
  }

  async function addDevice(e) {
    e.preventDefault()
    adding = true
    addError = ''
    try {
      const p = await AddPeer(addAddr)
      showAdd = false
      addAddr = ''
      select(p.id)
    } catch (err) {
      addError = String(err)
    } finally {
      adding = false
    }
  }

  async function forget() {
    if (!selected) return
    await ForgetPeer(selected.id)
    selectedId = null
  }

  async function copy(text) {
    await ClipboardSetText(text)
    showToast('Copied to clipboard')
  }

  function fmtTime(ms) {
    const d = new Date(ms)
    const today = new Date().toDateString() === d.toDateString()
    return today
      ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
      : d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  }
</script>

<div class="app">
  <aside class="sidebar">
    <section class="me">
      <div class="label">This device</div>
      {#if editingName}
        <!-- svelte-ignore a11y_autofocus -->
        <input
          class="name-input"
          bind:value={nameDraft}
          autofocus
          onblur={saveName}
          onkeydown={(e) => { if (e.key === 'Enter') saveName(); if (e.key === 'Escape') editingName = false }}
        />
      {:else}
        <button class="me-name" title="Rename" onclick={() => { nameDraft = self.name; editingName = true }}>
          {self.name}
          <span class="edit">✎</span>
        </button>
      {/if}
      <div class="muted small">{osLabel[self.os] ?? self.os} · port {self.port}</div>
    </section>

    <div class="list-head">
      <span class="label">Devices</span>
      <div class="head-actions">
        <button class="icon" title="Scan again" onclick={refresh} class:spin={scanning}>↻</button>
        <button class="icon" title="Add by address" onclick={() => { showAdd = true; addError = '' }}>+</button>
      </div>
    </div>

    <ul class="devices">
      {#each devices as d (d.id)}
        <li>
          <button class="device" class:active={d.id === selectedId} onclick={() => select(d.id)}>
            <span class="dot" class:online={d.online}></span>
            <span class="device-text">
              <span class="device-name">{d.name}</span>
              <span class="muted small">{d.online ? (osLabel[d.os] ?? d.os) : 'offline'}</span>
            </span>
            {#if unread[d.id]}<span class="badge">{unread[d.id]}</span>{/if}
          </button>
        </li>
      {:else}
        <li class="empty-list">
          <p>Looking for devices…</p>
          <p class="muted small">
            Start dsync on your other computer. If it doesn't show up, open UDP 47100 and TCP 47101
            in the firewall, or add it by address with <b>+</b>.
          </p>
        </li>
      {/each}
    </ul>

    <section class="receive">
      <div class="label">Received files</div>
      <button class="path" title={receiveDir} onclick={() => run(OpenPath, receiveDir)}>{'\u200e' + receiveDir + '\u200e'}</button>
      <button class="link small" onclick={changeReceiveDir}>Change folder</button>
    </section>
  </aside>

  <main class="main" class:drop-target={!!selected}>
    {#if serviceError}
      <div class="banner">Background service stopped: {serviceError}</div>
    {/if}

    {#if selected}
      <header class="thread-head">
        <div>
          <div class="thread-name">{selected.name}</div>
          <div class="muted small">
            <span class="dot inline" class:online={selected.online}></span>
            {selected.online ? 'online' : 'offline'}{selected.addr ? ` · ${selected.addr}` : ''}
          </div>
        </div>
        {#if selected.manual}
          <button class="ghost" onclick={forget}>Forget</button>
        {/if}
      </header>

      <div class="messages" bind:this={messagesEl}>
        {#each thread as m (m.id)}
          <div class="msg" class:out={!m.incoming}>
            <div class="bubble" class:file-bubble={m.file}>
              {#if m.file}
                {@const f = m.file}
                {@const p = progress[m.id]}
                <div class="file">
                  <div class="file-icon">{extOf(f.name)}</div>
                  <div class="file-body">
                    <div class="file-name" title={f.name}>{f.name}</div>
                    {#if f.status === 'active'}
                      <div class="bar"><div class="fill" style="width: {pct(p?.done ?? 0, f.size)}%"></div></div>
                      <div class="muted small">
                        {#if p}
                          {fmtSize(p.done)} of {fmtSize(f.size)} · {fmtSize(p.rate)}/s
                        {:else}
                          {fmtSize(f.size)} · {m.incoming ? 'starting…' : 'waiting…'}
                        {/if}
                      </div>
                    {:else}
                      <div class="muted small" class:error={f.status === 'failed'} title={f.error}>
                        {fmtSize(f.size)} · {statusText[f.status]}{f.error ? `: ${f.error}` : ''}
                      </div>
                    {/if}
                  </div>
                </div>
              {:else}
                <pre>{m.text}</pre>
              {/if}
              <div class="meta">
                <span>{fmtTime(m.time)}</span>
                {#if m.file?.status === 'active'}
                  <button class="action" onclick={() => CancelTransfer(m.id)}>Cancel</button>
                {:else if m.file?.status === 'done' && m.file.path}
                  <button class="action" onclick={() => run(OpenPath, m.file.path)}>Open</button>
                  <button class="action" onclick={() => run(RevealPath, m.file.path)}>Show in folder</button>
                {:else if !m.file}
                  <button class="copy" onclick={() => copy(m.text)}>Copy</button>
                {/if}
              </div>
            </div>
          </div>
        {:else}
          <div class="empty">No messages with {selected.name} yet.<br/>Type below, or drag files here.</div>
        {/each}
      </div>

      <form class="composer" onsubmit={(e) => { e.preventDefault(); send() }}>
        <button type="button" class="icon attach" title="Send files (or drag them here)" onclick={attach}>📎</button>
        <textarea
          bind:this={composerEl}
          bind:value={draft}
          onkeydown={onComposerKey}
          rows="3"
          placeholder={selected.online ? `Message ${selected.name} — Enter to send, Shift+Enter for a new line` : `${selected.name} is offline`}
        ></textarea>
        <button class="primary" type="submit" disabled={sending || !draft.trim()}>
          {sending ? 'Sending…' : 'Send'}
        </button>
      </form>
    {:else}
      <div class="welcome">
        <h1>dsync</h1>
        <p class="muted">Pick a device on the left to send it text.</p>
      </div>
    {/if}
  </main>
</div>

{#if selected}
  <div class="drop-hint">Drop to send to {selected.name}</div>
{/if}

{#if showAdd}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div class="overlay" onclick={(e) => { if (e.target === e.currentTarget) showAdd = false }}>
    <form class="dialog" onsubmit={addDevice}>
      <h2>Add device by address</h2>
      <p class="muted small">Use this when the device is on another network, e.g. its Tailscale IP.</p>
      <!-- svelte-ignore a11y_autofocus -->
      <input bind:value={addAddr} placeholder="192.168.1.20 or 100.64.0.2:47101" autofocus />
      {#if addError}<p class="error small">{addError}</p>{/if}
      <div class="dialog-actions">
        <button type="button" class="ghost" onclick={() => (showAdd = false)}>Cancel</button>
        <button type="submit" class="primary" disabled={adding || !addAddr.trim()}>
          {adding ? 'Connecting…' : 'Add'}
        </button>
      </div>
    </form>
  </div>
{/if}

{#if toast}
  <div class="toast" class:error-toast={toast.kind === 'error'}>{toast.text}</div>
{/if}
