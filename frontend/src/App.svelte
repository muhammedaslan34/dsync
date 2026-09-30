<script>
  import { onMount, tick } from 'svelte'
  import {
    Self, SetName, Peers, History, Status, Scan, ForgetPeer, SendText,
    PickFiles, SendPaths, CancelTransfer, RetryTransfer, ReceiveDir, ChooseReceiveDir, OpenPath, RevealPath,
    LocalAddrs, Fingerprint, StartPair, CancelPair, AnswerPair, PendingPairs, Unpair,
  } from '../wailsjs/go/main/App'
  import { EventsOn, ClipboardSetText, OnFileDrop } from '../wailsjs/runtime/runtime'
  import Icon from './lib/Icon.svelte'
  import Avatar from './lib/Avatar.svelte'
  import FileCard from './lib/FileCard.svelte'
  import ConnectDialog from './lib/ConnectDialog.svelte'
  import SettingsDialog from './lib/SettingsDialog.svelte'
  import PairDialog from './lib/PairDialog.svelte'
  import { osLabel, fmtTime, fmtShort, dayLabel } from './lib/format.js'

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
  let localAddrs = $state([])
  let dialog = $state(null) // 'connect' | 'settings' | 'unpair' | null
  let fingerprint = $state('')
  let pairOut = $state(null) // pairing we started: { kind: 'out', peerId, name, code, error }
  let pairIn = $state([]) // incoming requests waiting for an answer
  let theme = $state(loadTheme())

  let messagesEl = $state()
  let composerEl = $state()

  // Devices we have talked to stay in the list even after they disappear.
  let devices = $derived.by(() => {
    const byId = new Map(peers.map((p) => [p.id, p]))
    for (const m of messages) {
      if (!byId.has(m.peerId)) {
        byId.set(m.peerId, { id: m.peerId, name: m.peerName, os: '', addr: '', online: false, manual: false })
      }
    }
    return [...byId.values()].sort(
      (a, b) => (b.online - a.online) || (lastByPeer[b.id]?.time ?? 0) - (lastByPeer[a.id]?.time ?? 0) || a.name.localeCompare(b.name),
    )
  })
  let lastByPeer = $derived.by(() => {
    const last = {}
    for (const m of messages) last[m.peerId] = m
    return last
  })
  let selected = $derived(devices.find((d) => d.id === selectedId) ?? null)
  let primaryAddr = $derived(localAddrs[0]?.ip ?? '')

  // The thread with day separators, and a flag for the first message of
  // each run from the same side (for spacing).
  let items = $derived.by(() => {
    const out = []
    let prev = null
    for (const m of messages) {
      if (m.peerId !== selectedId) continue
      const day = dayLabel(m.time)
      if (!prev || dayLabel(prev.time) !== day) out.push({ key: `d${m.id}`, day })
      const first = !prev || prev.incoming !== m.incoming || m.time - prev.time > 5 * 60000 || out.at(-1).day
      out.push({ key: m.id, m, first })
      prev = m
    }
    return out
  })

  onMount(async () => {
    ;[self, peers, messages, serviceError, receiveDir, localAddrs, fingerprint, pairIn] = await Promise.all([
      Self(), Peers(), History(), Status(), ReceiveDir(), LocalAddrs(), Fingerprint(), PendingPairs(),
    ])
    localAddrs ??= []
    pairIn = (pairIn ?? []).map((r) => ({ ...r, kind: 'in' }))
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
    EventsOn('pair:request', (r) => { pairIn = [...pairIn, { ...r, kind: 'in' }] })
    EventsOn('pair:closed', (id) => { pairIn = pairIn.filter((r) => r.id !== id) })
    EventsOn('pair:result', (r) => {
      if (pairOut?.peerId !== r.peerId) return
      if (r.ok) {
        showToast(`Paired with ${pairOut.name}`)
        pairOut = null
        tick().then(() => composerEl?.focus())
      } else if (r.error === 'canceled') {
        pairOut = null
      } else {
        pairOut = { ...pairOut, error: r.error }
      }
    })

    // Only elements with --wails-drop-target: drop accept files.
    OnFileDrop((_x, _y, paths) => {
      if (selectedId && paths?.length) run(() => SendPaths(selectedId, paths))
    }, true)

    window.addEventListener('keydown', (e) => { if (e.key === 'Escape') dialog = null })

    if (!selectedId && devices.length) select(devices[0].id)
  })

  function loadTheme() {
    let t = 'system'
    try { t = localStorage.getItem('theme') || 'system' } catch {}
    applyTheme(t)
    return t
  }

  function applyTheme(t) {
    if (t === 'system') document.documentElement.removeAttribute('data-theme')
    else document.documentElement.dataset.theme = t
  }

  function setTheme(t) {
    theme = t
    applyTheme(t)
    try { localStorage.setItem('theme', t) } catch {}
  }

  async function scrollToBottom() {
    await tick()
    messagesEl?.scrollTo({ top: messagesEl.scrollHeight })
  }

  function select(id) {
    selectedId = id
    unread[id] = 0
    scrollToBottom()
    tick().then(() => composerEl?.focus())
  }

  function showToast(text, kind = 'info') {
    toast = { text, kind, id: Date.now() }
    clearTimeout(showToast.t)
    showToast.t = setTimeout(() => (toast = null), kind === 'error' ? 5000 : 2200)
  }

  // run calls fn and shows any error as a toast.
  async function run(fn) {
    try {
      return await fn()
    } catch (e) {
      showToast(String(e), 'error')
    }
  }

  async function send() {
    const text = draft
    if (!text.trim() || !selected || sending) return
    sending = true
    try {
      await SendText(selected.id, text)
      draft = ''
      await tick()
      autosize()
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

  function autosize() {
    if (!composerEl) return
    composerEl.style.height = 'auto'
    composerEl.style.height = `${Math.min(composerEl.scrollHeight, 200)}px`
  }

  function refresh() {
    scanning = true
    Scan()
  }

  async function rename(name) {
    await run(async () => {
      await SetName(name)
      self = await Self()
      showToast('Device renamed')
    })
  }

  async function forget() {
    if (!selected) return
    await run(() => ForgetPeer(selected.id))
    selectedId = null
  }

  async function copy(text) {
    await ClipboardSetText(text)
    showToast('Copied to clipboard')
  }

  function openDialog(name) {
    dialog = name
    LocalAddrs().then((a) => (localAddrs = a ?? []))
  }

  async function startPair(d) {
    pairOut = { kind: 'out', peerId: d.id, name: d.name, code: '', error: '' }
    try {
      pairOut.code = await StartPair(d.id)
    } catch (e) {
      pairOut.error = String(e)
    }
  }

  function closePairOut() {
    if (pairOut && !pairOut.error) CancelPair(pairOut.peerId)
    pairOut = null
  }

  function answerPair(accept) {
    const r = pairIn[0]
    AnswerPair(r.id, accept)
    pairIn = pairIn.slice(1)
  }

  async function unpair() {
    dialog = null
    await run(async () => {
      await Unpair(selected.id)
      showToast(`Unpaired ${selected.name}`)
    })
  }

  function preview(m) {
    if (!m) return null
    const text = m.file ? m.file.name : m.text.split('\n')[0]
    return (m.incoming ? '' : 'You: ') + text
  }
</script>

<div class="app">
  <aside class="sidebar">
    <div class="brand">
      <span class="logo"><Icon name="logo" size={18} stroke={2.4} /></span>
      <span class="brand-name">dsync</span>
      <button class="icon-btn" title="Settings" onclick={() => openDialog('settings')}><Icon name="settings" /></button>
    </div>

    <button class="me" title="Settings" onclick={() => openDialog('settings')}>
      <Avatar name={self.name} id={self.id} size={36} online={!serviceError} />
      <span class="device-text">
        <span class="device-name">{self.name}</span>
        <span class="muted small">{osLabel[self.os] ?? self.os}{primaryAddr ? ` · ${primaryAddr}` : ''}</span>
      </span>
      <span class="chip">This PC</span>
    </button>

    <div class="list-head">
      <span class="label">Devices <span class="count">{devices.length}</span></span>
      <button class="icon-btn sm" title="Scan again" onclick={refresh}>
        <span class:spin={scanning}><Icon name="refresh" size={15} /></span>
      </button>
    </div>

    <ul class="devices">
      {#each devices as d (d.id)}
        {@const last = lastByPeer[d.id]}
        <li>
          <button class="device" class:active={d.id === selectedId} onclick={() => select(d.id)}>
            <Avatar name={d.name} id={d.id} size={38} online={d.online} />
            <span class="device-text">
              <span class="device-row">
                <span class="device-name">{d.name}</span>
                {#if last}<span class="time">{fmtShort(last.time)}</span>{/if}
              </span>
              <span class="device-row">
                <span class="preview">
                  {#if !d.paired && d.online}<span class="not-paired"><Icon name="lock" size={11} stroke={2.5} /> Not paired</span>
                  {:else if last}{preview(last)}{:else}{d.online ? `Online · ${osLabel[d.os] ?? d.os}` : 'Offline'}{/if}
                </span>
                {#if unread[d.id]}<span class="badge">{unread[d.id]}</span>{/if}
              </span>
            </span>
          </button>
        </li>
      {:else}
        <li class="empty-list">
          <span class="pulse"><Icon name="wifi" size={20} /></span>
          <p>Looking for devices…</p>
          <p class="muted small">Open dsync on your other computer and it will show up here.</p>
        </li>
      {/each}
    </ul>

    <div class="sidebar-foot">
      <button class="btn primary block" onclick={() => openDialog('connect')}>
        <Icon name="plus" size={16} /> Connect a device
      </button>
    </div>
  </aside>

  <main class="main" class:drop-target={!!selected?.paired}>
    {#if serviceError}
      <div class="banner"><Icon name="alert" size={16} /> Background service stopped: {serviceError}</div>
    {/if}

    {#if selected}
      <header class="thread-head">
        <Avatar name={selected.name} id={selected.id} size={40} online={selected.online} />
        <div class="device-text">
          <div class="thread-name">{selected.name}</div>
          <div class="muted small">
            {#if selected.online}
              <span class="online-text">Online</span> · {osLabel[selected.os] ?? selected.os}{selected.addr ? ` · ${selected.addr}` : ''}
              {#if selected.paired}<span class="secure" title="Paired · end-to-end encrypted (TLS 1.3)"> · <Icon name="lock" size={11} stroke={2.5} /> Encrypted</span>{/if}
            {:else}
              Offline
            {/if}
          </div>
        </div>
        <div class="head-actions">
          {#if selected.paired}
            <button class="icon-btn" title="Send files" onclick={() => run(() => PickFiles(selected.id))}><Icon name="paperclip" /></button>
          {/if}
          <button class="icon-btn" title="Open received files folder" onclick={() => run(() => OpenPath(receiveDir))}><Icon name="folder" /></button>
          {#if selected.paired}
            <button class="icon-btn danger" title="Unpair" onclick={() => (dialog = 'unpair')}><Icon name="unlink" /></button>
          {/if}
          {#if selected.manual}
            <button class="icon-btn danger" title="Forget this device" onclick={forget}><Icon name="trash" /></button>
          {/if}
        </div>
      </header>

      <div class="messages" bind:this={messagesEl}>
        {#each items as it (it.key)}
          {#if it.day}
            <div class="day"><span>{it.day}</span></div>
          {:else}
            {@const m = it.m}
            <div class="msg" class:out={!m.incoming} class:first={it.first}>
              {#if m.file}
                <div class="bubble file-bubble">
                  <FileCard
                    {m}
                    progress={progress[m.id]}
                    onCancel={() => CancelTransfer(m.id)}
                    onOpen={() => run(() => OpenPath(m.file.path))}
                    onReveal={() => run(() => RevealPath(m.file.path))}
                    onRetry={() => run(() => RetryTransfer(m.id))}
                  />
                  <span class="stamp">{fmtTime(m.time)}</span>
                </div>
              {:else}
                <div class="bubble">
                  <pre>{m.text}</pre>
                  <span class="stamp">{fmtTime(m.time)}</span>
                </div>
                <button class="copy-btn" title="Copy" onclick={() => copy(m.text)}><Icon name="copy" size={14} /></button>
              {/if}
            </div>
          {/if}
        {:else}
          <div class="thread-empty">
            <Avatar name={selected.name} id={selected.id} size={64} />
            {#if selected.paired}
              <h3>Start sending to {selected.name}</h3>
              <p class="muted">Type a message or command below, or drag files anywhere in this window.</p>
            {:else}
              <h3>{selected.name} isn't paired yet</h3>
              <p class="muted">Pair once to send messages and files. Everything between paired devices is encrypted.</p>
            {/if}
          </div>
        {/each}
      </div>

      {#if !selected.paired}
        <div class="pair-bar">
          <span class="pair-bar-icon"><Icon name="lock" size={18} /></span>
          <div class="device-text">
            <b>Pair with {selected.name}</b>
            <span class="muted small">You'll confirm a 6-digit code on both computers.</span>
          </div>
          <button class="btn primary" disabled={!selected.online} onclick={() => startPair(selected)}>
            {selected.online ? 'Pair' : 'Offline'}
          </button>
        </div>
      {:else}
      {#if !selected.online}
        <div class="offline-note"><Icon name="alert" size={14} /> {selected.name} is offline. Messages will fail until it's back.</div>
      {/if}

      <form class="composer" onsubmit={(e) => { e.preventDefault(); send() }}>
        <div class="composer-box">
          <button type="button" class="icon-btn" title="Send files" onclick={() => run(() => PickFiles(selected.id))}>
            <Icon name="paperclip" />
          </button>
          <textarea
            bind:this={composerEl}
            bind:value={draft}
            oninput={autosize}
            onkeydown={onComposerKey}
            rows="1"
            placeholder="Message {selected.name}"
          ></textarea>
          <button class="send-btn" type="submit" title="Send (Enter)" disabled={sending || !draft.trim()}>
            <Icon name="send" size={17} />
          </button>
        </div>
        <div class="composer-hint">Enter to send · Shift+Enter for a new line · Drop files to send them</div>
      </form>
      {/if}
    {:else}
      <div class="welcome">
        <span class="logo big"><Icon name="logo" size={34} stroke={2.2} /></span>
        <h1>Welcome to dsync</h1>
        <p class="muted">Send text, commands and files between your computers.</p>
        <ol class="steps">
          <li><span class="step-n">1</span><div><b>Open dsync on your other computer</b><span class="muted">Both need to be on the same network, or on Tailscale.</span></div></li>
          <li><span class="step-n">2</span><div><b>Allow it through the firewall</b><span class="muted">UDP 47100 and TCP 47101, on private networks.</span></div></li>
          <li><span class="step-n">3</span><div><b>Pick it on the left</b><span class="muted">It appears automatically, or use Connect a device.</span></div></li>
        </ol>
        <button class="btn primary" onclick={() => openDialog('connect')}><Icon name="plus" size={16} /> Connect a device</button>
        {#if primaryAddr}
          <p class="muted small">This computer's address: <button class="text-btn mono" onclick={() => copy(primaryAddr)}>{primaryAddr}</button></p>
        {/if}
      </div>
    {/if}

    {#if selected}
      <div class="drop-hint">
        <div class="drop-card">
          <Icon name="upload" size={36} />
          <b>Drop to send to {selected.name}</b>
        </div>
      </div>
    {/if}
  </main>
</div>

{#if dialog === 'connect'}
  <ConnectDialog
    {peers}
    {localAddrs}
    onClose={() => (dialog = null)}
    onConnected={(id) => {
      dialog = null
      select(id)
      const d = devices.find((x) => x.id === id)
      if (d && !d.paired) startPair(d)
    }}
    onCopy={copy}
  />
{:else if dialog === 'settings'}
  <SettingsDialog
    {self}
    {receiveDir}
    {localAddrs}
    {theme}
    {fingerprint}
    pairedCount={peers.filter((p) => p.paired).length}
    onClose={() => (dialog = null)}
    onRename={rename}
    onChangeDir={() => run(async () => (receiveDir = await ChooseReceiveDir()))}
    onOpenDir={() => run(() => OpenPath(receiveDir))}
    onCopy={copy}
    onTheme={setTheme}
  />
{/if}

{#if dialog === 'unpair' && selected}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <div class="overlay" role="presentation" onclick={(e) => { if (e.target === e.currentTarget) dialog = null }}>
    <div class="dialog confirm" role="dialog" aria-label="Unpair">
      <h2>Unpair {selected.name}?</h2>
      <p class="muted">Neither computer will accept messages or files from the other until you pair again.</p>
      <div class="dialog-actions">
        <button class="btn secondary" onclick={() => (dialog = null)}>Cancel</button>
        <button class="btn danger" onclick={unpair}>Unpair</button>
      </div>
    </div>
  </div>
{/if}

{#if pairIn.length}
  <PairDialog pair={pairIn[0]} onAnswer={answerPair} />
{:else if pairOut}
  <PairDialog pair={pairOut} onCancel={closePairOut} onRetry={() => startPair({ id: pairOut.peerId, name: pairOut.name })} />
{/if}

{#if toast}
  {#key toast.id}
    <div class="toast" class:error={toast.kind === 'error'} role="status">
      <Icon name={toast.kind === 'error' ? 'alert' : 'check'} size={16} />
      <span>{toast.text}</span>
    </div>
  {/key}
{/if}
