<script>
  import Icon from './Icon.svelte'
  import Avatar from './Avatar.svelte'
  import { osLabel } from './format.js'

  let {
    self, receiveDir, localAddrs, theme, fingerprint, pairedCount, clipStatus, onClipboard,
    bg, onBackground, setters, hostInfo, onSunshineLogin, onOpenURL, version,
    onClose, onRename, onChangeDir, onOpenDir, onCopy, onTheme,
  } = $props()

  let name = $state('')
  let sunUser = $state('')
  let sunPass = $state('')
  let sunBusy = $state(false)
  let sunError = $state('')

  async function saveSunshine(e) {
    e.preventDefault()
    sunBusy = true
    sunError = ''
    try {
      await onSunshineLogin(sunUser.trim(), sunPass)
      sunPass = ''
    } catch (err) {
      sunError = String(err)
    } finally {
      sunBusy = false
    }
  }
  $effect(() => { name = self.name }) // reset when the saved name changes
  let saving = $state(false)

  async function save(e) {
    e.preventDefault()
    saving = true
    try {
      await onRename(name)
    } finally {
      saving = false
    }
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="overlay" role="presentation" onclick={(e) => { if (e.target === e.currentTarget) onClose() }}>
  <div class="dialog" role="dialog" aria-label="Settings">
    <header class="dialog-head">
      <h2>Settings</h2>
      <button class="icon-btn" title="Close" onclick={onClose}><Icon name="x" /></button>
    </header>

    <section class="setting">
      <div class="setting-title">This device</div>
      <form class="input-row" onsubmit={save}>
        <Avatar name={name} id={self.id} size={38} />
        <input bind:value={name} aria-label="Device name" maxlength="40" />
        <button class="btn primary" disabled={saving || !name.trim() || name.trim() === self.name}>Save</button>
      </form>
      <p class="muted small">Other computers see this name. {osLabel[self.os] ?? self.os} · port {self.port}</p>
    </section>

    <section class="setting">
      <div class="setting-title">Addresses</div>
      <div class="addr-list">
        {#each localAddrs as a (a.ip)}
          <button class="addr-row" onclick={() => onCopy(a.ip)}>
            <Icon name={a.kind === 'Tailscale' || a.kind === 'ZeroTier' ? 'shield' : 'wifi'} size={16} />
            <span class="mono">{a.ip}</span>
            <span class="muted small">{a.kind}</span>
            <span class="copy-hint"><Icon name="copy" size={14} /></span>
          </button>
        {:else}
          <p class="muted small">No network connection.</p>
        {/each}
      </div>
    </section>

    <section class="setting">
      <div class="setting-title">Clipboard</div>
      <label class="switch-row" class:disabled={!clipStatus.available}>
        <span class="switch-text">
          <span>Sync clipboard with paired devices</span>
          <span class="muted small">
            {#if clipStatus.available}
              Copy on one computer, paste on another. Needs this on both. Passwords copied from
              password managers are never sent.
            {:else}
              Not available here: {clipStatus.error}
            {/if}
          </span>
        </span>
        <input type="checkbox" class="switch" role="switch" checked={clipStatus.enabled}
          disabled={!clipStatus.available} onchange={(e) => onClipboard(e.currentTarget.checked)} />
      </label>
    </section>

    <section class="setting">
      <div class="setting-title">Background</div>
      <label class="switch-row" class:disabled={!bg.trayAvailable}>
        <span class="switch-text">
          <span>Keep running in the tray when the window is closed</span>
          <span class="muted small">
            {#if bg.trayAvailable}
              dsync keeps receiving and syncing the clipboard. Quit it from the tray icon.
            {:else}
              This desktop has no tray, so closing the window quits dsync. (On GNOME, the
              AppIndicator extension adds one.)
            {/if}
          </span>
        </span>
        <input type="checkbox" class="switch" role="switch" checked={bg.trayAvailable && bg.keepInTray}
          disabled={!bg.trayAvailable} onchange={(e) => onBackground(setters.SetKeepInTray, e.currentTarget.checked)} />
      </label>
      {#if bg.autostartSupported}
        <label class="switch-row">
          <span class="switch-text">
            <span>Start dsync when I log in</span>
            <span class="muted small">{bg.trayAvailable ? 'It starts in the tray, without opening the window.' : 'It opens its window when you log in.'}</span>
          </span>
          <input type="checkbox" class="switch" role="switch" checked={bg.autostart}
            onchange={(e) => onBackground(setters.SetAutostart, e.currentTarget.checked)} />
        </label>
      {/if}
      {#if bg.appMenuSupported}
        <label class="switch-row">
          <span class="switch-text">
            <span>Show dsync in the app menu</span>
            <span class="muted small">So you can open it like any other app.</span>
          </span>
          <input type="checkbox" class="switch" role="switch" checked={bg.appMenu}
            onchange={(e) => onBackground(setters.SetAppMenu, e.currentTarget.checked)} />
        </label>
      {/if}
    </section>

    <section class="setting">
      <div class="setting-title">Remote control</div>
      <div class="rc-status">
        <div class="rc-row">
          <span class="rc-dot" class:ok={hostInfo.moonlight}></span>
          <span><b>Moonlight</b> {hostInfo.moonlight ? 'is installed: you can control other computers from here.' : "isn't installed: needed to control other computers."}</span>
        </div>
        {#if hostInfo.moonlightHint}<code class="rc-hint">{hostInfo.moonlightHint}</code>{/if}
        <div class="rc-row">
          <span class="rc-dot" class:ok={hostInfo.sunshineRunning} class:warn={hostInfo.sunshineInstalled && !hostInfo.sunshineRunning}></span>
          <span><b>Sunshine</b>
            {#if hostInfo.sunshineRunning}is running: paired devices can control this computer.
            {:else if hostInfo.sunshineInstalled}is installed but not running (dsync starts it when needed).
            {:else}isn't installed: needed for other computers to control this one.{/if}
          </span>
        </div>
        {#if hostInfo.sunshineHint}<code class="rc-hint">{hostInfo.sunshineHint}</code>{/if}
      </div>
      {#if hostInfo.sunshineInstalled || hostInfo.sunshineRunning}
        {#if hostInfo.sunshineLogin}
          <div class="rc-login-saved">
            <span class="muted small"><Icon name="check" size={13} stroke={3} /> Sunshine login saved: paired devices finish setup on their own.</span>
            <button class="text-btn small" onclick={() => onSunshineLogin('', '')}>Remove</button>
          </div>
        {:else}
          <form class="rc-login" onsubmit={saveSunshine}>
            <p class="muted small">
              Optional: save the login you set in <button type="button" class="text-btn" onclick={() => onOpenURL(hostInfo.sunshineUrl)}>Sunshine's web page</button>
              so paired devices can set up remote control without you entering a PIN here.
            </p>
            <div class="input-row">
              <input placeholder="Sunshine username" bind:value={sunUser} autocomplete="off" />
              <input placeholder="Password" type="password" bind:value={sunPass} autocomplete="off" />
              <button class="btn secondary" disabled={sunBusy || !sunUser.trim() || !sunPass}>{sunBusy ? 'Checking…' : 'Save'}</button>
            </div>
            {#if sunError}<p class="form-error"><Icon name="alert" size={14} /> {sunError}</p>{/if}
          </form>
        {/if}
      {/if}
    </section>

    <section class="setting">
      <div class="setting-title">Security</div>
      <div class="security-row">
        <Icon name="lock" size={16} />
        <span>Connections are encrypted with TLS 1.3. Only paired devices ({pairedCount}) can send you data.</span>
      </div>
      <div class="security-row">
        <Icon name="key" size={16} />
        <span>This device's key: <span class="mono">{fingerprint}</span></span>
      </div>
    </section>

    <section class="setting">
      <div class="setting-title">Received files</div>
      <div class="input-row">
        <button class="path-box" title="Open folder" onclick={onOpenDir}>
          <Icon name="folder" size={16} />
          <span class="path-text">{'‎' + receiveDir + '‎'}</span>
        </button>
        <button class="btn secondary" onclick={onChangeDir}>Change…</button>
      </div>
    </section>

    <section class="setting">
      <div class="setting-title">Appearance</div>
      <div class="segmented" role="radiogroup" aria-label="Theme">
        {#each [['system', 'System'], ['light', 'Light'], ['dark', 'Dark']] as [value, label] (value)}
          <button role="radio" aria-checked={theme === value} class:on={theme === value} onclick={() => onTheme(value)}>
            {label}
          </button>
        {/each}
      </div>
    </section>
    {#if version}<p class="version muted small">dsync {version}</p>{/if}
  </div>
</div>
