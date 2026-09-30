<script>
  import { onMount } from 'svelte'
  import Icon from './Icon.svelte'
  import { fmtSize, fileKind } from './format.js'

  // items: [{ name, size, url? (image preview), file? (File), path? (local path) }]
  let { items, peerName, onSend, onCancel } = $props()
  let sending = $state(false)
  let sendBtn = $state()

  onMount(() => sendBtn?.focus())

  async function send() {
    sending = true
    try {
      await onSend()
    } finally {
      sending = false
    }
  }

  function onKey(e) {
    if (e.key === 'Enter' && !sending) {
      e.preventDefault()
      send()
    }
  }

  let total = $derived(items.reduce((n, it) => n + (it.size ?? 0), 0))
</script>

<svelte:window onkeydown={onKey} />

<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="overlay" role="presentation" onclick={(e) => { if (e.target === e.currentTarget) onCancel() }}>
  <div class="dialog paste-dialog" role="dialog" aria-label="Send pasted items">
    <header class="dialog-head">
      <h2>Send to {peerName}?</h2>
      <button class="icon-btn" title="Cancel" onclick={onCancel}><Icon name="x" /></button>
    </header>

    {#if items.length === 1 && items[0].url}
      <div class="paste-preview"><img src={items[0].url} alt={items[0].name} /></div>
    {/if}

    <ul class="paste-list">
      {#each items as it, i (i)}
        <li>
          {#if it.url && items.length > 1}
            <img class="paste-thumb" src={it.url} alt="" />
          {:else}
            <span class="file-card" data-kind={fileKind(it.name)}><span class="file-icon sm"><Icon name={fileKind(it.name)} size={16} /></span></span>
          {/if}
          <span class="paste-name" title={it.name}>{it.name}</span>
          {#if it.size != null}<span class="muted small">{fmtSize(it.size)}</span>{/if}
        </li>
      {/each}
    </ul>

    <div class="dialog-actions">
      {#if items.length > 1 && total}<span class="muted small paste-total">{items.length} items · {fmtSize(total)}</span>{/if}
      <button class="btn secondary" onclick={onCancel}>Cancel</button>
      <button class="btn primary" bind:this={sendBtn} disabled={sending} onclick={send}>
        <Icon name="send" size={15} /> {sending ? 'Sending…' : 'Send'}
      </button>
    </div>
  </div>
</div>
