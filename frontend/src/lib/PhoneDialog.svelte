<script>
  import { onMount } from 'svelte'
  import QRCode from 'qrcode'
  import Icon from './Icon.svelte'

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
  <div class="dialog phone-dialog" role="dialog" aria-label="Connect a phone">
    <header class="dialog-head">
      <div>
        <h2>Connect a phone</h2>
        <p class="muted">Open the dsync app on your phone and scan this code. The phone must be on the same Wi-Fi.</p>
      </div>
      <button class="icon-btn" title="Close" onclick={onClose}><Icon name="x" /></button>
    </header>

    <div class="qr" class:expired={left === 0}>
      {#if svg}{@html svg}{/if}
      {#if left === 0}
        <div class="qr-expired">
          <b>This code expired</b>
          <button class="btn primary sm" onclick={onRefresh}>New code</button>
        </div>
      {/if}
    </div>
    <p class="muted small qr-note">
      {#if left > 0}Works once, for {Math.floor(left / 60)}:{String(left % 60).padStart(2, '0')} more.{/if}
      Everything between the phone and this computer is encrypted with a key inside this code, so only scan it with your own phone.
    </p>
    <div class="dialog-actions">
      <button class="btn secondary" onclick={() => onCopy(pairing.url)}><Icon name="copy" size={14} /> Copy link</button>
      <button class="btn primary" onclick={onClose}>Done</button>
    </div>
  </div>
</div>
