<script>
  import Icon from './Icon.svelte'
  import { fmtSize, fileKind } from './format.js'

  let { m, progress, onCancel, onOpen, onReveal } = $props()

  let f = $derived(m.file)
  let kind = $derived(fileKind(f.name))
  let pct = $derived(f.size && progress ? Math.min(100, (progress.done / f.size) * 100) : 0)

  function eta(p) {
    if (!p?.rate) return ''
    const s = Math.max(0, Math.round((f.size - p.done) / p.rate))
    if (s < 60) return `${s}s left`
    if (s < 3600) return `${Math.round(s / 60)} min left`
    return `${(s / 3600).toFixed(1)} h left`
  }
</script>

<div class="file-card" data-kind={kind} class:failed={f.status === 'failed'}>
  <div class="file-icon"><Icon name={kind} size={20} /></div>
  <div class="file-body">
    <div class="file-name" title={f.name}>{f.name}</div>
    {#if f.status === 'active'}
      <div class="bar"><div class="fill" class:indeterminate={!progress} style="width: {progress ? pct : 100}%"></div></div>
      <div class="file-sub">
        {#if progress}
          <span>{fmtSize(progress.done)} / {fmtSize(f.size)}</span>
          <span>{fmtSize(progress.rate)}/s · {eta(progress)}</span>
        {:else}
          <span>{fmtSize(f.size)}</span>
          <span>{m.incoming ? 'Starting…' : 'Waiting…'}</span>
        {/if}
      </div>
    {:else}
      <div class="file-sub">
        <span>{fmtSize(f.size)}</span>
        {#if f.status === 'done'}
          <span class="chip ok"><Icon name="check" size={12} stroke={3} /> {m.incoming ? 'Received' : 'Sent'}</span>
        {:else if f.status === 'canceled'}
          <span class="chip">Canceled</span>
        {:else}
          <span class="chip bad" title={f.error}>Failed</span>
        {/if}
      </div>
      {#if f.error}<div class="file-error" title={f.error}>{f.error}</div>{/if}
    {/if}
  </div>
</div>

<div class="file-actions">
  {#if f.status === 'active'}
    <button class="pill" onclick={onCancel}><Icon name="x" size={13} /> Cancel</button>
  {:else if f.status === 'done' && f.path}
    <button class="pill" onclick={onOpen}><Icon name="open" size={13} /> Open</button>
    <button class="pill" onclick={onReveal}><Icon name="folder" size={13} /> Show in folder</button>
  {/if}
</div>
