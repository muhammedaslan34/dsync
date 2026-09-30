<script>
  import Icon from './Icon.svelte'
  import Avatar from './Avatar.svelte'

  // state: the latest ControlState for this device ({ step, message, pin, hint }),
  // or { step: 'options' } before starting. info: what the device offers.
  let { peer, state, info, options, onStart, onCancel, onRetry, onClose, onCopy } = $props()

  const sizes = [
    ['', 'Full size', 'Sharpest; everything at its normal size'],
    ['1600x900', 'Bigger', 'Text and windows look larger'],
    ['1280x720', 'Much bigger', 'Easiest to read on a small screen'],
  ]
  // Only a Windows PC switches its own resolution, which is what makes
  // things bigger; elsewhere a smaller stream just looks softer.
  let canZoom = $derived(info?.os === 'windows' && info?.configurable)
  let screens = $derived(info?.displays ?? [])

  function res(d) {
    return d.width && d.height ? `${d.width}×${d.height}` : ''
  }

  const steps = [
    ['checking', 'Check Moonlight and Sunshine'],
    ['starting', 'Start Sunshine'],
    ['configuring', 'Set up the screen'],
    ['pairing', 'Pair (only the first time)'],
    ['streaming', 'Open Moonlight'],
  ]
  let order = $derived(steps.findIndex(([s]) => s === state.step))
  let finished = $derived(['done', 'error', 'canceled', 'options'].includes(state.step))
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

    {#if state.step === 'options'}
      {#if !info}
        <p class="muted small control-msg"><span class="spinner"></span> Asking {peer.name} which screens it has…</p>
      {:else}
        <div class="ctl-options">
          {#if screens.length > 1}
            <div class="opt">
              <span class="opt-label">Screen</span>
              <div class="screen-list">
                {#each screens as d (d.id)}
                  <button class="screen" class:on={options.screen === d.id || (!options.screen && (info.screen ? info.screen === d.id : d.primary))}
                    disabled={!info.configurable} onclick={() => (options.screen = d.id)}>
                    <Icon name="monitor" size={18} />
                    <span class="screen-text"><b>{d.name}</b><span class="muted small">{res(d)}{d.primary ? ' · main' : ''}</span></span>
                  </button>
                {/each}
              </div>
              {#if !info.configurable}<p class="muted small">To choose, save the Sunshine login in dsync on {peer.name} (Settings → Remote control).</p>{/if}
            </div>
          {/if}

          <div class="opt">
            <span class="opt-label">Size</span>
            <div class="segmented wide">
              {#each sizes as [value, label] (value)}
                <button class:on={options.resolution === value} onclick={() => (options.resolution = value)}>{label}</button>
              {/each}
            </div>
            <p class="muted small">
              {#if options.resolution && canZoom}{peer.name}'s screen switches to this size while you control it, so everything looks bigger. It switches back afterwards.
              {:else if options.resolution}A smaller stream is lighter on the network, but doesn't enlarge {peer.name}'s screen{info.os === 'windows' ? ' unless its Sunshine login is saved in dsync' : ''}.
              {:else}{sizes[0][2]}.{/if}
            </p>
          </div>

          <div class="opt">
            <span class="opt-label">Quality</span>
            <div class="segmented wide">
              {#each [['standard', 'Standard'], ['high', 'High'], ['best', 'Best']] as [value, label] (value)}
                <button class:on={options.quality === value} onclick={() => (options.quality = value)}>{label}</button>
              {/each}
            </div>
            <p class="muted small">
              {options.quality === 'best' ? 'Sharpest picture; needs a fast network (wired or strong Wi-Fi).' : options.quality === 'standard' ? 'Lightest on the network; text can look soft.' : 'Sharp text on a home network.'}
            </p>
            <label class="switch-row">
              <span class="switch-text">
                <span>Sharpest text</span>
                <span class="muted small">Keeps full color detail so small text stays crisp. Needs recent graphics on both computers; turn it off if the picture stutters or stays black.</span>
              </span>
              <input type="checkbox" class="switch" role="switch" bind:checked={options.sharpText} />
            </label>
          </div>

          <div class="opt-row">
            <div class="opt">
              <span class="opt-label">Window</span>
              <div class="segmented">
                <button class:on={options.displayMode === 'fullscreen'} onclick={() => (options.displayMode = 'fullscreen')}>Fullscreen</button>
                <button class:on={options.displayMode === 'windowed'} onclick={() => (options.displayMode = 'windowed')}>Window</button>
              </div>
            </div>
            <div class="opt">
              <span class="opt-label">Smoothness</span>
              <div class="segmented">
                {#each [30, 60, 120] as fps (fps)}
                  <button class:on={options.fps === fps} onclick={() => (options.fps = fps)}>{fps} fps</button>
                {/each}
              </div>
            </div>
          </div>

          <div class="ctl-tips muted small">
            <div><b>While controlling:</b> Ctrl+Alt+Shift+Q stops, Ctrl+Alt+Shift+X switches fullscreen.</div>
            <div><b>Zoom into one spot:</b> {info.os === 'windows' ? 'press Win and + on the PC (Windows Magnifier); Win and Esc turns it off.' : 'press Super+Alt+8 on that computer to turn on GNOME zoom.'}</div>
          </div>
          {#if info.error}<p class="form-error"><Icon name="alert" size={14} /> {info.error}</p>{/if}
        </div>
      {/if}
    {:else if state.step === 'error'}
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
      {#if state.step === 'options'}
        <button class="btn secondary" onclick={onClose}>Cancel</button>
        <button class="btn primary" disabled={!info} onclick={() => onStart(options)}><Icon name="monitor" size={15} /> Start</button>
      {:else if state.step === 'error'}
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
