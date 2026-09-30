<script>
  import Icon from './Icon.svelte'
  import Avatar from './Avatar.svelte'
  import { osLabel } from './format.js'

  let {
    self, receiveDir, localAddrs, theme, fingerprint, pairedCount, clipStatus, onClipboard,
    bg, onBackground, setters, hostInfo, onSunshineLogin, onOpenURL, version,
    firewall, onFixFirewall, onMakePrivate,
    updateInfo, updateProgress, onCheckUpdate, onInstallUpdate, onInstallProgram, onSunshineSetup,
    onClose, onRename, onChangeDir, onOpenDir, onCopy, onTheme,
  } = $props()

  let name = $state('')
  let sunUser = $state('')
  let sunPass = $state('')
  let sunBusy = $state(false)
  let sunError = $state('')
  let installing = $state('') // program being installed

  async function install(name) {
    installing = name
    try {
      await onInstallProgram(name)
    } finally {
      installing = ''
    }
  }

  function fmtMB(n) {
    return (n / 1048576).toFixed(1) + ' MB'
  }

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

    {#if firewall?.supported}
      <section class="setting">
        <div class="setting-title">Windows Firewall</div>
        {#if firewall.error}
          <p class="muted small">{firewall.error}</p>
        {:else}
          <div class="fw-row">
            <span class="rc-dot" class:ok={firewall.ruleOk} class:bad={!firewall.ruleOk}></span>
            <span class="fw-text">
              {#if firewall.ruleOk}dsync is allowed through the firewall.{:else}dsync isn't allowed through the firewall, so other computers can't reach this PC.{/if}
            </span>
            {#if !firewall.ruleOk}<button class="btn primary sm" onclick={onFixFirewall}>Allow</button>{/if}
          </div>
          <div class="fw-row">
            <span class="rc-dot" class:ok={!firewall.publicNetworks?.length} class:bad={firewall.publicNetworks?.length}></span>
            <span class="fw-text">
              {#if firewall.publicNetworks?.length}
                Your network "{firewall.publicNetworks.join('", "')}" is set to Public, where Windows blocks dsync. Make it Private if it's your home or work network.
              {:else}
                Your network is Private, so your other computers can connect.
              {/if}
            </span>
            {#if firewall.publicNetworks?.length}<button class="btn primary sm" onclick={onMakePrivate}>Make private</button>{/if}
          </div>
          <p class="muted small fw-note">Windows asks for permission when you click a button. No command line or sudo needed.</p>
        {/if}
      </section>
    {/if}

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
        {#if !hostInfo.moonlight}
          <div class="rc-install">
            <button class="btn secondary sm" disabled={!!installing} onclick={() => install('moonlight')}>
              {installing === 'moonlight' ? 'Installing…' : 'Install Moonlight'}
            </button>
            <span class="muted small">You may be asked for your password.</span>
          </div>
        {/if}
        <div class="rc-row">
          <span class="rc-dot" class:ok={hostInfo.sunshineRunning} class:warn={hostInfo.sunshineInstalled && !hostInfo.sunshineRunning}></span>
          <span><b>Sunshine</b>
            {#if hostInfo.sunshineRunning}is running: paired devices can control this computer.
            {:else if hostInfo.sunshineInstalled}is installed but not running (dsync starts it when needed).
            {:else}isn't installed: needed for other computers to control this one.{/if}
          </span>
        </div>
        {#if !hostInfo.sunshineInstalled}
          <div class="rc-install">
            <button class="btn secondary sm" disabled={!!installing} onclick={() => install('sunshine')}>
              {installing === 'sunshine' ? 'Installing…' : 'Install Sunshine'}
            </button>
            <span class="muted small">Then set its username and password once.</span>
          </div>
        {:else if !hostInfo.sunshineLogin}
          <div class="rc-install">
            <button class="btn secondary sm" onclick={onSunshineSetup}>Open Sunshine setup</button>
            <span class="muted small">Choose Sunshine's username and password there (first time only).</span>
          </div>
        {/if}
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
              Optional: save the login you set in <button type="button" class="text-btn" onclick={onSunshineSetup}>Sunshine's web page</button>
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
    <section class="setting">
      <div class="setting-title">Updates</div>
      <div class="upd-row">
        <span class="fw-text">
          {#if updateProgress}
            {#if updateProgress.step === 'downloading'}Downloading dsync {updateInfo?.latest}… {updateProgress.total ? `${fmtMB(updateProgress.done)} of ${fmtMB(updateProgress.total)}` : ''}
            {:else if updateProgress.step === 'installing'}Installing… (you may be asked for your password)
            {:else}Restarting dsync…{/if}
          {:else if updateInfo?.checking}Checking…
          {:else if updateInfo?.error}<span class="error">Couldn't check: {updateInfo.error}</span>
          {:else if updateInfo?.available}<b>dsync {updateInfo.latest} is available.</b> You have {version}.
          {:else if updateInfo}You have the latest version ({version}).
          {:else}You have dsync {version}.{/if}
        </span>
        {#if updateInfo?.available && !updateProgress}
          <button class="btn primary sm" onclick={onInstallUpdate}>{updateInfo.canInstall ? `Update to ${updateInfo.latest}` : 'Download'}</button>
        {:else if !updateProgress}
          <button class="btn secondary sm" disabled={updateInfo?.checking} onclick={onCheckUpdate}>Check for updates</button>
        {/if}
      </div>
      {#if updateProgress?.step === 'downloading' && updateProgress.total}
        <div class="bar"><div class="fill" style="width: {(updateProgress.done / updateProgress.total) * 100}%"></div></div>
      {/if}
      {#if updateInfo?.available && updateInfo.url}
        <button class="text-btn small" onclick={() => onOpenURL(updateInfo.url)}>What's new in {updateInfo.latest}</button>
      {/if}
    </section>
  </div>
</div>
