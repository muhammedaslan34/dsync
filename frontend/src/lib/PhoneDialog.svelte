<script>
  import { onMount } from 'svelte'
  import QRCode from 'qrcode'
  import Icon from './Icon.svelte'
  import { t } from './i18n.svelte.js'

  // pairing: { url, expires } from StartPhonePairing.
  let { pairing, onRefresh, onClose, onCopy } = $props()

  let svg = $state('')
  let now = $state(Date.now())
  let left = $derived(Math.max(0, Math.round((pairing.expires - now) / 1000)))

  $effect(() => {
    QRCode.toString(pairing.url, { type: 'svg', margin: 1, errorCorrectionLevel: 'M' }).then((s) => (svg = s))
  })
  onMount(() => {
    const t = setInterval(() => (now = Date.now()), 1000)
    return () => clearInterval(t)
  })
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="overlay" role="presentation" onclick={(e) => { if (e.target === e.currentTarget) onClose() }}>
  <div class="dialog phone-dialog" role="dialog" aria-label={t('phone.title')}>
    <header class="dialog-head">
      <div>
        <h2>{t('phone.title')}</h2>
        <p class="muted">{t('phone.text')}</p>
      </div>
      <button class="icon-btn" title={t('common.close')} onclick={onClose}><Icon name="x" /></button>
    </header>

    <div class="qr" class:expired={left === 0}>
      {#if svg}{@html svg}{/if}
      {#if left === 0}
        <div class="qr-expired">
          <b>{t('phone.expired')}</b>
          <button class="btn primary sm" onclick={onRefresh}>{t('phone.newCode')}</button>
        </div>
      {/if}
    </div>
    <p class="muted small qr-note">
      {#if left > 0}{t('phone.worksFor', { time: `${Math.floor(left / 60)}:${String(left % 60).padStart(2, '0')}` })}{/if}
      {t('phone.encrypted')}
    </p>
    <div class="dialog-actions">
      <button class="btn secondary" onclick={() => onCopy(pairing.url)}><Icon name="copy" size={14} /> {t('phone.copyLink')}</button>
      <button class="btn primary" onclick={onClose}>{t('common.done')}</button>
    </div>
  </div>
</div>
