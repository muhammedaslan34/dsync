<script>
  import Icon from './Icon.svelte'
  import Avatar from './Avatar.svelte'
  import { t } from './i18n.svelte.js'

  // state: the latest ControlState for this device ({ step, code, message,
  // detail, pin, hint }), or { step: 'options' } before starting. info:
  // what the device offers.
  let { peer, state, info, options, onStart, onCancel, onRetry, onClose, onCopy } = $props()

  const sizes = [
    ['', 'control.sizeFull'],
    ['1600x900', 'control.sizeBig'],
    ['1280x720', 'control.sizeHuge'],
  ]
  // Only a Windows PC switches its own resolution, which is what makes
  // things bigger; elsewhere a smaller stream just looks softer.
  let canZoom = $derived(info?.os === 'windows' && info?.configurable)
  let screens = $derived(info?.displays ?? [])

  function res(d) {
    return d.width && d.height ? `${d.width}×${d.height}` : ''
  }

  const steps = ['checking', 'starting', 'configuring', 'pairing', 'streaming']
  let order = $derived(steps.indexOf(state.step))
  let finished = $derived(['done', 'error', 'canceled', 'options'].includes(state.step))

  // Progress and errors are shown from their step and code, so they follow
  // the window's language; Go's English message is the fallback for
  // anything unknown.
  const pairingMsg = { 'pair-auto': 'pairAuto', 'pair-manual': 'pairManual', 'pair-retry': 'pairRetry' }
  const errorMsg = {
    'no-moonlight': 'noMoonlight', 'no-sunshine': 'noSunshine', 'sunshine-start': 'sunshineStart',
    'sunshine-blocked': 'sunshineBlocked', ask: 'ask', configure: 'configure',
    'pair-unfinished': 'pairUnfinished', 'pair-timeout': 'pairTimeout',
  }
  let message = $derived.by(() => {
    const vars = { name: peer.name, error: state.detail ?? '' }
    if (state.step === 'error') return errorMsg[state.code] ? t('control.err.' + errorMsg[state.code], vars) : state.message
    if (state.step === 'pairing') return pairingMsg[state.code] ? t('control.msg.' + pairingMsg[state.code], vars) : state.message
    if (['checking', 'starting', 'configuring', 'streaming', 'done'].includes(state.step)) return t('control.msg.' + state.step, vars)
    return state.message
  })
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="overlay" role="presentation" onclick={(e) => { if (e.target === e.currentTarget && finished) onClose() }}>
  <div class="dialog control-dialog" role="dialog" aria-label={t('control.label')}>
    <header class="dialog-head">
      <div class="control-title">
        <Avatar name={peer.name} id={peer.id} size={40} />
        <div>
          <h2>{t('control.title', { name: peer.name })}</h2>
          <p class="muted">{t('control.subtitle')}</p>
        </div>
      </div>
      <button class="icon-btn" title={t('common.close')} onclick={finished ? onClose : onCancel}><Icon name="x" /></button>
    </header>

    {#if state.step === 'options'}
      {#if !info}
        <p class="muted small control-msg"><span class="spinner"></span> {t('control.asking', { name: peer.name })}</p>
      {:else}
        <div class="ctl-options">
          {#if screens.length > 1}
            <div class="opt">
              <span class="opt-label">{t('control.screen')}</span>
              <div class="screen-list">
                {#each screens as d (d.id)}
                  <button class="screen" class:on={options.screen === d.id || (!options.screen && (info.screen ? info.screen === d.id : d.primary))}
                    disabled={!info.configurable} onclick={() => (options.screen = d.id)}>
                    <Icon name="monitor" size={18} />
                    <span class="screen-text"><b><bdi>{d.name}</bdi></b><span class="muted small"><bdi>{res(d)}</bdi>{d.primary ? ' · ' + t('control.main') : ''}</span></span>
                  </button>
                {/each}
              </div>
              {#if !info.configurable}<p class="muted small">{t('control.chooseHint', { name: peer.name })}</p>{/if}
            </div>
          {/if}

          <div class="opt">
            <span class="opt-label">{t('control.size')}</span>
            <div class="segmented wide">
              {#each sizes as [value, label] (value)}
                <button class:on={options.resolution === value} onclick={() => (options.resolution = value)}>{t(label)}</button>
              {/each}
            </div>
            <p class="muted small">
              {#if options.resolution && canZoom}{t('control.zoomHint', { name: peer.name })}
              {:else if options.resolution}{t(info.os === 'windows' ? 'control.smallHintWin' : 'control.smallHint', { name: peer.name })}
              {:else}{t('control.sizeFullHint')}{/if}
            </p>
          </div>

          <div class="opt">
            <span class="opt-label">{t('control.quality')}</span>
            <div class="segmented wide">
              {#each ['standard', 'high', 'best'] as value (value)}
                <button class:on={options.quality === value} onclick={() => (options.quality = value)}>{t('control.' + value)}</button>
              {/each}
            </div>
            <p class="muted small">
              {t(options.quality === 'best' ? 'control.bestHint' : options.quality === 'standard' ? 'control.standardHint' : 'control.highHint')}
            </p>
            <label class="switch-row">
              <span class="switch-text">
                <span>{t('control.sharpText')}</span>
                <span class="muted small">{t('control.sharpTextHint')}</span>
              </span>
              <input type="checkbox" class="switch" role="switch" bind:checked={options.sharpText} />
            </label>
          </div>

          <div class="opt">
            <span class="opt-label">{t('control.mouse')}</span>
            <div class="segmented wide">
              <button class:on={options.mouse !== 'game'} onclick={() => (options.mouse = 'desktop')}>{t('control.desktop')}</button>
              <button class:on={options.mouse === 'game'} onclick={() => (options.mouse = 'game')}>{t('control.game')}</button>
            </div>
            <p class="muted small">
              {t(options.mouse === 'game' ? 'control.gameHint' : 'control.desktopHint')}
            </p>
            {#if info.pointerAdjustable}
              <label class="speed">
                <span class="opt-label">{t('control.pointerSpeed')}</span>
                <input type="range" min="-2" max="2" step="1" bind:value={options.pointerSpeed} />
                <span class="speed-scale muted small"><span>{t('control.slower')}</span><span>{t('control.normal')}</span><span>{t('control.faster')}</span></span>
              </label>
              {#if options.pointerSpeed}<p class="muted small">{t(options.pointerSpeed > 0 ? 'control.pointerFaster' : 'control.pointerSlower')}</p>{/if}
            {/if}
          </div>

          <div class="opt-row">
            <div class="opt">
              <span class="opt-label">{t('control.window')}</span>
              <div class="segmented">
                <button class:on={options.displayMode === 'fullscreen'} onclick={() => (options.displayMode = 'fullscreen')}>{t('control.fullscreen')}</button>
                <button class:on={options.displayMode === 'windowed'} onclick={() => (options.displayMode = 'windowed')}>{t('control.windowed')}</button>
              </div>
            </div>
            <div class="opt">
              <span class="opt-label">{t('control.smoothness')}</span>
              <div class="segmented">
                {#each [30, 60, 120] as fps (fps)}
                  <button class:on={options.fps === fps} onclick={() => (options.fps = fps)}>{t('control.fps', { count: fps })}</button>
                {/each}
              </div>
            </div>
          </div>

          <div class="ctl-tips muted small">
            <div><b>{t('control.tipsControlling')}</b> {t('control.tipsKeys', { stop: 'Ctrl+Alt+Shift+Q', full: 'Ctrl+Alt+Shift+X' })}</div>
            <div><b>{t('control.tipsZoom')}</b> {info.os === 'windows' ? t('control.zoomWin') : t('control.zoomGnome', { keys: 'Super+Alt+8' })}</div>
          </div>
          {#if info.error}<p class="form-error"><Icon name="alert" size={14} /> {info.error}</p>{/if}
        </div>
      {/if}
    {:else if state.step === 'error'}
      <div class="control-error">
        <Icon name="alert" size={18} />
        <div>
          <b>{message}</b>
          {#if state.hint}
            <div class="hint-box">
              <code dir="ltr">{state.hint}</code>
              <button class="icon-btn sm" title={t('common.copy')} onclick={() => onCopy(state.hint)}><Icon name="copy" size={14} /></button>
            </div>
          {/if}
        </div>
      </div>
    {:else if state.step === 'done'}
      <div class="control-done"><Icon name="check" size={18} stroke={3} /> {message}</div>
    {:else}
      <ol class="control-steps">
        {#each steps as key, i (key)}
          <li class:active={i === order} class:past={order > i}>
            <span class="step-dot">
              {#if order > i}<Icon name="check" size={12} stroke={3} />{:else if i === order}<span class="spinner"></span>{/if}
            </span>
            {t('control.steps.' + key)}
          </li>
        {/each}
      </ol>
      {#if message}<p class="muted small control-msg">{message}</p>{/if}
      {#if state.pin}
        <div class="pair-code" dir="ltr">{state.pin}</div>
      {/if}
    {/if}

    <div class="dialog-actions">
      {#if state.step === 'options'}
        <button class="btn secondary" onclick={onClose}>{t('common.cancel')}</button>
        <button class="btn primary" disabled={!info} onclick={() => onStart(options)}><Icon name="monitor" size={15} /> {t('control.start')}</button>
      {:else if state.step === 'error'}
        <button class="btn secondary" onclick={onClose}>{t('common.close')}</button>
        <button class="btn primary" onclick={onRetry}>{t('common.tryAgain')}</button>
      {:else if finished}
        <button class="btn primary" onclick={onClose}>{t('common.close')}</button>
      {:else}
        <button class="btn secondary" onclick={onCancel}>{t('common.cancel')}</button>
      {/if}
    </div>
  </div>
</div>
