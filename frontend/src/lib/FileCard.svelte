<script>
  import Icon from './Icon.svelte'
  import { fmtSize, fileKind } from './format.js'

  let { m, progress, onCancel, onOpen, onReveal, onRetry } = $props()

  let f = $derived(m.file)
  let kind = $derived(f.folder ? 'folder' : fileKind(f.name))
  let pct = $derived(f.size && progress ? Math.min(100, (progress.done / f.size) * 100) : 0)
  // Pictures show inline once there is a local copy: always for files we
  // sent, and for received ones when they have finished.
  let showImage = $derived(!f.folder && kind === 'image' && f.path && (!m.incoming || f.status === 'done'))

  // "3 of 120 files · " for folders that stopped partway.
  let filesPrefix = $derived(f.folder && f.files ? `${f.doneFiles ?? 0} of ${f.files} files · ` : '')
  let filesLabel = $derived(f.folder && f.files ? `${f.files} ${f.files === 1 ? 'file' : 'files'} · ` : '')
  let imageFailed = $state(false)

  function eta(p) {
    if (!p?.rate) return ''
    const s = Math.max(0, Math.round((f.size - p.done) / p.rate))
    if (s < 60) return `${s}s left`
    if (s < 3600) return `${Math.round(s / 60)} min left`
    return `${(s / 3600).toFixed(1)} h left`
  }
</script>

{#if showImage && !imageFailed}
  <button class="image-preview" title="Open" onclick={onOpen}>
    <img src="/image/{m.id}" alt={f.name} loading="lazy" onerror={() => (imageFailed = true)} />
  </button>
{/if}

<div class="file-card" data-kind={kind} class:failed={f.status === 'failed'}>
  <div class="file-icon"><Icon name={kind} size={20} /></div>
  <div class="file-body">
    <div class="file-name" title={f.name}>{f.name}</div>
    {#if f.status === 'active'}
      <div class="bar"><div class="fill" class:indeterminate={!progress} style="width: {progress ? pct : 100}%"></div></div>
      <div class="file-sub">
        {#if f.error}
          <span>{progress ? `${fmtSize(progress.done)} / ${fmtSize(f.size)}` : fmtSize(f.size)}</span>
          <span>Paused</span>
        {:else if progress}
          <span>{fmtSize(progress.done)} / {fmtSize(f.size)}</span>
          <span>{progress.rate ? `${fmtSize(progress.rate)}/s · ${eta(progress)}` : 'Resuming…'}</span>
        {:else if f.folder && !f.files}
          <span>Folder</span>
          <span>{m.incoming ? 'Starting…' : 'Waiting…'}</span>
        {:else}
          <span>{fmtSize(f.size)}</span>
          <span>{m.incoming ? 'Starting…' : 'Waiting…'}</span>
        {/if}
      </div>
      {#if f.folder && f.files}<div class="file-count">{f.doneFiles ?? 0} of {f.files} files</div>{/if}
      {#if f.error}<div class="retrying"><Icon name="refresh" size={12} /> {f.error}</div>{/if}
    {:else}
      <div class="file-sub">
        <span>{f.status === 'done' ? filesLabel : filesPrefix}{fmtSize(f.size)}</span>
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
  {:else if !m.incoming && (f.status === 'failed' || f.status === 'canceled')}
    <button class="pill" onclick={onRetry}><Icon name="refresh" size={13} /> {f.status === 'failed' ? 'Retry' : 'Send again'}</button>
  {/if}
</div>
