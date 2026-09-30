<script>
  import { onMount } from 'svelte'
  import { AddPeer, FindOnNetwork } from '../../wailsjs/go/main/App'
  import Icon from './Icon.svelte'
  import Avatar from './Avatar.svelte'
  import { osLabel } from './format.js'

  let { peers, localAddrs, onClose, onConnected, onCopy } = $props()

  let searching = $state(false)
  let results = $state(null) // null until the first search finishes
  let addr = $state('')
  let busy = $state(false)
  let error = $state('')

  onMount(search)

  async function search() {
    searching = true
    try {
      results = (await FindOnNetwork()) ?? []
    } finally {
      searching = false
    }
  }

  async function connect(a) {
    busy = true
    error = ''
    try {
      const p = await AddPeer(a)
      onConnected(p.id)
    } catch (e) {
      error = String(e)
    } finally {
      busy = false
    }
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
<div class="overlay" onclick={(e) => { if (e.target === e.currentTarget) onClose() }}>
  <div class="dialog" role="dialog" aria-label="Connect a device">
    <header class="dialog-head">
      <div>
        <h2>Connect a device</h2>
        <p class="muted">Open dsync on the other computer, then pick it below.</p>
      </div>
      <button class="icon-btn" title="Close" onclick={onClose}><Icon name="x" /></button>
    </header>

    <div class="section-head">
      <span class="label">On your network</span>
      <button class="text-btn" onclick={search} disabled={searching}>
        <span class:spin={searching}><Icon name="refresh" size={14} /></span>
        {searching ? 'Searching…' : 'Search again'}
      </button>
    </div>

    <div class="results">
      {#if searching && !results?.length}
        {#each [0, 1] as i (i)}
          <div class="result skeleton"><span class="sk-avatar"></span><span class="sk-lines"><span></span><span></span></span></div>
        {/each}
      {:else if results?.length}
        {#each results as r (r.id)}
          {@const known = peers.some((p) => p.id === r.id)}
          <div class="result">
            <Avatar name={r.name} id={r.id} size={34} online={true} />
            <span class="device-text">
              <span class="device-name">{r.name}</span>
              <span class="muted small">{osLabel[r.os] ?? r.os} · {r.addr}</span>
            </span>
            <button class="btn {known ? 'secondary' : 'primary'} sm" disabled={busy} onclick={() => connect(r.addr)}>
              {known ? 'Open' : 'Connect'}
            </button>
          </div>
        {/each}
      {:else if results}
        <div class="results-empty">
          <Icon name="search" size={22} />
          <div>
            <b>No devices found</b>
            <p class="muted small">
              Check that dsync is open on the other computer and TCP port 47101 is allowed in its
              firewall. You can also enter its address below.
            </p>
          </div>
        </div>
      {/if}
    </div>

    <form class="addr-form" onsubmit={(e) => { e.preventDefault(); connect(addr) }}>
      <label class="label" for="addr-input">Or connect by address</label>
      <div class="input-row">
        <input id="addr-input" bind:value={addr} placeholder="192.168.1.20 or a Tailscale IP" />
        <button type="submit" class="btn primary" disabled={busy || !addr.trim()}>
          {busy ? 'Connecting…' : 'Connect'}
        </button>
      </div>
      {#if error}<p class="form-error"><Icon name="alert" size={14} /> {error}</p>{/if}
    </form>

    {#if localAddrs.length}
      <footer class="dialog-foot">
        <span class="muted small">This computer:</span>
        {#each localAddrs as a (a.ip)}
          <button class="addr-chip" title="Copy" onclick={() => onCopy(a.ip)}>
            <span class="mono">{a.ip}</span><span class="muted">{a.kind}</span><Icon name="copy" size={12} />
          </button>
        {/each}
      </footer>
    {/if}
  </div>
</div>
