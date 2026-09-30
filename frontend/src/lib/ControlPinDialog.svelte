<script>
  import Icon from './Icon.svelte'
  import { t } from './i18n.svelte.js'

  // request: { peerName, pin, url } — shown on the computer being controlled
  let { request, onOpen, onCopy, onClose } = $props()
</script>

<div class="overlay" role="presentation">
  <div class="dialog pair-dialog" role="dialog" aria-label={t('controlPin.label')}>
    <span class="logo big"><Icon name="monitor" size={30} stroke={2.2} /></span>
    <h2>{t('controlPin.title', { name: request.peerName })}</h2>
    <p class="muted">{t('controlPin.text')}</p>
    <div class="pair-code with-copy">
      <bdi>{request.pin}</bdi>
      <button class="icon-btn sm" title={t('controlPin.copy')} onclick={() => onCopy(request.pin)}><Icon name="copy" size={15} /></button>
    </div>
    <p class="pair-warn"><Icon name="shield" size={15} /> {t('controlPin.warn')}</p>
    <p class="muted small">{t('controlPin.tip')}</p>
    <div class="dialog-actions">
      <button class="btn secondary" onclick={onClose}>{t('common.close')}</button>
      <button class="btn primary" onclick={() => onOpen(request.url)}><Icon name="open" size={15} /> {t('controlPin.open')}</button>
    </div>
  </div>
</div>
