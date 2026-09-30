<script>
  import Icon from './Icon.svelte'
  import Avatar from './Avatar.svelte'
  import { osLabel } from './format.js'

  // Outgoing: { kind: 'out', peerId, name, code, error }
  // Incoming: { kind: 'in', id, peerId, name, os, code }
  let { pair, onCancel, onRetry, onAnswer } = $props()
</script>

<div class="overlay">
  <div class="dialog pair-dialog" role="dialog" aria-label="Pair device">
    <div class="pair-avatars">
      <Avatar name={pair.name} id={pair.peerId} size={52} />
      <span class="pair-link"><Icon name="lock" size={16} /></span>
    </div>

    {#if pair.kind === 'in'}
      <h2>{pair.name} wants to pair</h2>
      <p class="muted">{osLabel[pair.os] ?? pair.os} · Paired devices can send you messages and files.</p>
    {:else if pair.error}
      <h2>Pairing didn't finish</h2>
      <p class="muted">{pair.error[0].toUpperCase() + pair.error.slice(1)}.</p>
    {:else}
      <h2>Pairing with {pair.name}</h2>
      <p class="muted">Accept on {pair.name} if it shows this same code.</p>
    {/if}

    {#if !pair.error}
      <div class="pair-code" aria-label="Pairing code">{pair.code}</div>
    {/if}

    {#if pair.kind === 'in'}
      <p class="pair-warn"><Icon name="shield" size={15} /> Only accept if {pair.name} shows exactly this code.</p>
      <div class="dialog-actions">
        <button class="btn secondary" onclick={() => onAnswer(false)}>Decline</button>
        <button class="btn primary" onclick={() => onAnswer(true)}>Accept</button>
      </div>
    {:else if pair.error}
      <div class="dialog-actions">
        <button class="btn secondary" onclick={onCancel}>Close</button>
        <button class="btn primary" onclick={onRetry}>Try again</button>
      </div>
    {:else}
      <p class="pair-wait muted small"><span class="spinner"></span> Waiting for {pair.name}…</p>
      <div class="dialog-actions">
        <button class="btn secondary" onclick={onCancel}>Cancel</button>
      </div>
    {/if}
  </div>
</div>
