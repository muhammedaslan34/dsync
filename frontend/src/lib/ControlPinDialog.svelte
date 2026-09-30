<script>
  import Icon from './Icon.svelte'

  // request: { peerName, pin, url } — shown on the computer being controlled
  let { request, onOpen, onCopy, onClose } = $props()
</script>

<div class="overlay" role="presentation">
  <div class="dialog pair-dialog" role="dialog" aria-label="Remote control PIN">
    <span class="logo big"><Icon name="monitor" size={30} stroke={2.2} /></span>
    <h2>{request.peerName} wants to control this computer</h2>
    <p class="muted">Enter this PIN in Sunshine to allow it. This is only needed once.</p>
    <div class="pair-code with-copy">
      {request.pin}
      <button class="icon-btn sm" title="Copy PIN" onclick={() => onCopy(request.pin)}><Icon name="copy" size={15} /></button>
    </div>
    <p class="pair-warn"><Icon name="shield" size={15} /> If you didn't expect this, just close it.</p>
    <p class="muted small">Tip: save your Sunshine login in dsync's settings and this happens automatically.</p>
    <div class="dialog-actions">
      <button class="btn secondary" onclick={onClose}>Close</button>
      <button class="btn primary" onclick={() => onOpen(request.url)}><Icon name="open" size={15} /> Open Sunshine</button>
    </div>
  </div>
</div>
