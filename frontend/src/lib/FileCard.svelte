<script>
  import Icon from './Icon.svelte'
  import { fmtSize, fmtRate, fmtDuration, fileKind } from './format.js'
  import { t } from './i18n.svelte.js'

  let { m, progress, onCancel, onOpen, onReveal, onRetry, showHiddenHint = false } = $props()

  let f = $derived(m.file)
  let kind = $derived(f.folder ? 'folder' : fileKind(f.name))
  let pct = $derived(f.size && progress ? Math.min(100, (progress.done / f.size) * 100) : 0)
  // Pictures show inline once there is a local copy: always for files we
  // sent, and for received ones when they have finished.
  let showImage = $derived(!f.folder && kind === 'image' && f.path && (!m.incoming || f.status === 'done'))

  // "3 of 120 files · " for folders that stopped partway.
  let filesPrefix = $derived(f.folder && f.files ? t('file.filesOf', { done: f.doneFiles ?? 0, count: f.files }) + ' · ' : '')
  let filesLabel = $derived(f.folder && f.files ? t('file.files', { count: f.files }) + ' · ' : '')
  let imageFailed = $state(false)

  function eta(p) {
    if (!p?.rate) return ''
    const s = Math.max(0, Math.round((f.size - p.done) / p.rate))
    return t('time.left', { time: fmtDuration(s) })
  }
</script>

{#if showImage && !imageFailed}
  <button class="image-preview" title={t('common.open')} onclick={onOpen}>
    <img src="/image/{m.id}" alt={f.name} loading="lazy" onerror={() => (imageFailed = true)} />
  </button>
{/if}

<div class="file-card" data-kind={kind} class:failed={f.status === 'failed'}>
  <div class="file-icon"><Icon name={kind} size={20} /></div>
  <div class="file-body">
    <div class="file-name" title={f.name}><bdi>{f.name}</bdi></div>
    {#if f.status === 'active'}
      <div class="bar"><div class="fill" class:indeterminate={!progress} style="width: {progress ? pct : 100}%"></div></div>
      <div class="file-sub">
        {#if f.error}
          <span>{progress ? t('units.progress', { done: fmtSize(progress.done), total: fmtSize(f.size) }) : fmtSize(f.size)}</span>
          <span>{t('file.paused')}</span>
        {:else if progress}
          <span>{t('units.progress', { done: fmtSize(progress.done), total: fmtSize(f.size) })}</span>
          <span>{progress.rate ? `${fmtRate(progress.rate)} · ${eta(progress)}` : t('file.resuming')}</span>
        {:else if f.folder && !f.files}
          <span>{t('file.folder')}</span>
          <span>{t(m.incoming ? 'file.starting' : 'file.waiting')}</span>
        {:else}
          <span>{fmtSize(f.size)}</span>
          <span>{t(m.incoming ? 'file.starting' : 'file.waiting')}</span>
        {/if}
      </div>
      {#if f.folder && f.files}<div class="file-count">{t('file.filesOf', { done: f.doneFiles ?? 0, count: f.files })}</div>{/if}
      {#if f.error}<div class="retrying"><Icon name="refresh" size={12} /> {f.error}</div>{/if}
    {:else}
      <div class="file-sub">
        <span>{f.status === 'done' ? filesLabel : filesPrefix}{fmtSize(f.size)}</span>
        {#if f.status === 'done'}
          <span class="chip ok"><Icon name="check" size={12} stroke={3} /> {t(m.incoming ? 'file.received' : 'file.sent')}</span>
        {:else if f.status === 'canceled'}
          <span class="chip">{t('file.canceled')}</span>
        {:else}
          <span class="chip bad" title={f.error}>{t('file.failed')}</span>
        {/if}
      </div>
      {#if f.error}<div class="file-error" title={f.error}>{f.error}</div>{/if}
      {#if showHiddenHint && m.incoming && f.status === 'done' && f.name.startsWith('.')}
        <div class="file-hint">{t('file.hiddenHint')}</div>
      {/if}
    {/if}
  </div>
</div>

<div class="file-actions">
  {#if f.status === 'active'}
    <button class="pill" onclick={onCancel}><Icon name="x" size={13} /> {t('common.cancel')}</button>
  {:else if f.status === 'done' && f.path}
    <button class="pill" onclick={onOpen}><Icon name="open" size={13} /> {t('common.open')}</button>
    <button class="pill" onclick={onReveal}><Icon name="folder" size={13} /> {t('file.reveal')}</button>
  {:else if !m.incoming && (f.status === 'failed' || f.status === 'canceled')}
    <button class="pill" onclick={onRetry}><Icon name="refresh" size={13} /> {t(f.status === 'failed' ? 'file.retry' : 'file.sendAgain')}</button>
  {/if}
</div>
