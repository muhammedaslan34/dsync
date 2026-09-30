<script>
  import Icon from './Icon.svelte'
  import Avatar from './Avatar.svelte'

  // state: the latest ControlState for this device ({ step, message, pin, hint })
  let { peer, state, onCancel, onRetry, onClose, onCopy } = $props()

  const steps = [
    ['checking', 'Check Moonlight and Sunshine'],
    ['starting', 'Start Sunshine'],
    ['pairing', 'Pair (only the first time)'],
    ['streaming', 'Open Moonlight'],
  ]
  let order = $derived(steps.findIndex(([s]) => s === state.step))
  let finished = $derived(['done', 'error', 'canceled'].includes(state.step))
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="overlay" role="presentation" onclick={(e) => { if (e.target === e.currentTarget && finished) onClose() }}>
  <div class="dialog control-dialog" role="dialog" aria-label="Remote control">
    <header class="dialog-head">
      <div class="control-title">
        <Avatar name={peer.name} id={peer.id} size={40} />
        <div>
          <h2>Control {peer.name}</h2>
          <p class="muted">Its screen opens in Moonlight; your mouse and keyboard control it.</p>
        </div>
      </div>
      <button class="icon-btn" title="Close" onclick={finished ? onClose : onCancel}><Icon name="x" /></button>
    </header>

    {#if state.step === 'error'}
      <div class="control-error">
        <Icon name="alert" size={18} />
        <div>
          <b>{state.message}</b>
          {#if state.hint}
            <div class="hint-box">
              <code>{state.hint}</code>
              <button class="icon-btn sm" title="Copy" onclick={() => onCopy(state.hint)}><Icon name="copy" size={14} /></button>
            </div>
          {/if}
        </div>
      </div>
    {:else if state.step === 'done'}
      <div class="control-done"><Icon name="check" size={18} stroke={3} /> {state.message}</div>
    {:else}
      <ol class="control-steps">
        {#each steps as [key, label], i (key)}
          <li class:active={i === order} class:past={order > i}>
            <span class="step-dot">
              {#if order > i}<Icon name="check" size={12} stroke={3} />{:else if i === order}<span class="spinner"></span>{/if}
            </span>
            {label}
          </li>
        {/each}
      </ol>
      {#if state.message}<p class="muted small control-msg">{state.message}</p>{/if}
      {#if state.pin}
        <div class="pair-code">{state.pin}</div>
      {/if}
    {/if}

    <div class="dialog-actions">
      {#if state.step === 'error'}
        <button class="btn secondary" onclick={onClose}>Close</button>
        <button class="btn primary" onclick={onRetry}>Try again</button>
      {:else if finished}
        <button class="btn primary" onclick={onClose}>Close</button>
      {:else}
        <button class="btn secondary" onclick={onCancel}>Cancel</button>
      {/if}
    </div>
  </div>
</div>
