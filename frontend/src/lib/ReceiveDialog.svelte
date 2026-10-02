<script>
  import Icon from './Icon.svelte'
  import { fmtSize } from './format.js'
  import { t } from './i18n.svelte.js'

  let { request, onAnswer } = $props()
</script>

<div class="overlay" role="presentation">
  <div class="dialog pair-dialog" role="dialog" aria-label={t('incoming.label')}>
    <span class="logo big"><Icon name={request.folder ? 'folder' : 'file'} size={30} stroke={2.2} /></span>
    <h2>{t(request.folder ? 'incoming.folderTitle' : 'incoming.fileTitle', { name: request.peerName })}</h2>
    <p class="muted small">
      <bdi>{request.peerName}</bdi>
      {#if request.fingerprint} · <bdi class="mono" dir="ltr">{request.fingerprint}</bdi>{/if}
    </p>
    <div class="incoming-name" dir="auto"><bdi>{request.name}</bdi></div>
    <p class="muted">
      {#if request.folder}
        {t('incoming.folderDetails', { count: request.files ?? 0, size: fmtSize(request.size) })}
      {:else}
        {fmtSize(request.size)}
      {/if}
    </p>
    <p class="pair-warn"><Icon name="shield" size={15} /> {t('incoming.warn')}</p>
    <div class="dialog-actions">
      <button class="btn secondary" onclick={() => onAnswer(false)}>{t('incoming.decline')}</button>
      <button class="btn primary" onclick={() => onAnswer(true)}>{t('incoming.accept')}</button>
    </div>
  </div>
</div>

<style>
  .incoming-name {
    margin: 16px 0 6px;
    padding: 12px;
    overflow-wrap: anywhere;
    border: 1px solid var(--border);
    border-radius: 10px;
    background: var(--sidebar);
    font-weight: 650;
  }
</style>
