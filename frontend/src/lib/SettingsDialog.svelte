<script>
  import Icon from './Icon.svelte'
  import Avatar from './Avatar.svelte'
  import { osLabel, fmtSize } from './format.js'
  import { t, fmtList, languages } from './i18n.svelte.js'

  let {
    self, receiveDir, receiveSettings, onAskBeforeReceiving, localAddrs, theme, language = 'system', onLanguage, fingerprint, pairedCount, clipStatus, onClipboard,
    bg, onBackground, setters, hostInfo, onSunshineLogin, onOpenURL, version,
    firewall, onFixFirewall, onMakePrivate, onClearAll, onConnectPhone, phones = [],
    updateInfo, updateProgress, onCheckUpdate, onInstallUpdate, onInstallProgram, onSunshineSetup, onAllowSunshine,
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

  async function removeSunshine() {
    sunBusy = true
    sunError = ''
    try {
      await onSunshineLogin('', '')
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
  <div class="dialog" role="dialog" aria-label={t('common.settings')}>
    <header class="dialog-head">
      <h2>{t('common.settings')}</h2>
      <button class="icon-btn" title={t('common.close')} onclick={onClose}><Icon name="x" /></button>
    </header>

    <section class="setting">
      <div class="setting-title">{t('settings.thisDevice')}</div>
      <form class="input-row" onsubmit={save}>
        <Avatar name={name} id={self.id} size={38} />
        <input bind:value={name} aria-label={t('settings.deviceName')} maxlength="40" dir={name ? 'auto' : null} />
        <button class="btn primary" disabled={saving || !name.trim() || name.trim() === self.name}>{t('common.save')}</button>
      </form>
      <p class="muted small">{t('settings.nameHint')} {osLabel[self.os] ?? self.os} · {t('settings.port', { port: String(self.port) })}</p>
    </section>

    <section class="setting">
      <div class="setting-title">{t('settings.language')}</div>
      <div class="segmented wrap" role="radiogroup" aria-label={t('settings.language')}>
        <button role="radio" aria-checked={language === 'system'} class:on={language === 'system'} onclick={() => onLanguage('system')}>
          {t('settings.system')}
        </button>
        {#each languages as [code, label] (code)}
          <button role="radio" aria-checked={language === code} class:on={language === code} lang={code} dir="auto" onclick={() => onLanguage(code)}>
            {label}
          </button>
        {/each}
      </div>
    </section>

    <section class="setting">
      <div class="setting-title">{t('settings.addresses')}</div>
      <div class="addr-list">
        {#each localAddrs as a (a.ip)}
          <button class="addr-row" onclick={() => onCopy(a.ip)}>
            <Icon name={a.kind === 'Tailscale' || a.kind === 'ZeroTier' ? 'shield' : 'wifi'} size={16} />
            <span class="mono" dir="ltr">{a.ip}</span>
            <span class="muted small">{a.kind === 'Ethernet' ? t('settings.ethernet') : a.kind}</span>
            <span class="copy-hint"><Icon name="copy" size={14} /></span>
          </button>
        {:else}
          <p class="muted small">{t('settings.noNetwork')}</p>
        {/each}
      </div>
    </section>

    {#if firewall?.supported}
      <section class="setting">
        <div class="setting-title">{t('settings.firewall')}</div>
        {#if firewall.error}
          <p class="muted small">{firewall.error}</p>
        {:else}
          <div class="fw-row">
            <span class="rc-dot" class:ok={firewall.ruleOk} class:bad={!firewall.ruleOk}></span>
            <span class="fw-text">
              {t(firewall.ruleOk ? 'settings.fwAllowed' : 'settings.fwBlocked')}
            </span>
            {#if !firewall.ruleOk}<button class="btn primary sm" onclick={onFixFirewall}>{t('settings.allow')}</button>{/if}
          </div>
          <div class="fw-row">
            <span class="rc-dot" class:ok={!firewall.publicNetworks?.length} class:bad={firewall.publicNetworks?.length}></span>
            <span class="fw-text">
              {#if firewall.publicNetworks?.length}
                {t('settings.fwPublic', { names: fmtList(firewall.publicNetworks) })}
              {:else}
                {t('settings.fwPrivate')}
              {/if}
            </span>
            {#if firewall.publicNetworks?.length}<button class="btn primary sm" onclick={onMakePrivate}>{t('settings.makePrivate')}</button>{/if}
          </div>
          <p class="muted small fw-note">{t('settings.fwNote')}</p>
        {/if}
      </section>
    {/if}

    <section class="setting">
      <div class="setting-title">{t('settings.clipboard')}</div>
      <label class="switch-row" class:disabled={!clipStatus.available}>
        <span class="switch-text">
          <span>{t('settings.clipSync')}</span>
          <span class="muted small">
            {#if clipStatus.available}
              {t('settings.clipText')}
            {:else}
              {t('settings.clipUnavailable', { error: clipStatus.error })}
            {/if}
          </span>
        </span>
        <input type="checkbox" class="switch" role="switch" checked={clipStatus.enabled}
          disabled={!clipStatus.available} onchange={(e) => onClipboard(e.currentTarget.checked)} />
      </label>
    </section>

    <section class="setting">
      <div class="setting-title">{t('settings.background')}</div>
      <label class="switch-row" class:disabled={!bg.trayAvailable}>
        <span class="switch-text">
          <span>{t('settings.keepTray')}</span>
          <span class="muted small">
            {t(bg.trayAvailable ? 'settings.trayText' : 'settings.noTray')}
          </span>
        </span>
        <input type="checkbox" class="switch" role="switch" checked={bg.trayAvailable && bg.keepInTray}
          disabled={!bg.trayAvailable} onchange={(e) => onBackground(setters.SetKeepInTray, e.currentTarget.checked)} />
      </label>
      {#if bg.autostartSupported}
        <label class="switch-row">
          <span class="switch-text">
            <span>{t('settings.autostart')}</span>
            <span class="muted small">{t(bg.trayAvailable ? 'settings.autostartTray' : 'settings.autostartWindow')}</span>
          </span>
          <input type="checkbox" class="switch" role="switch" checked={bg.autostart}
            onchange={(e) => onBackground(setters.SetAutostart, e.currentTarget.checked)} />
        </label>
      {/if}
      {#if bg.appMenuSupported}
        <label class="switch-row">
          <span class="switch-text">
            <span>{t('settings.appMenu')}</span>
            <span class="muted small">{t('settings.appMenuText')}</span>
          </span>
          <input type="checkbox" class="switch" role="switch" checked={bg.appMenu}
            onchange={(e) => onBackground(setters.SetAppMenu, e.currentTarget.checked)} />
        </label>
      {/if}
    </section>

    <section class="setting">
      <div class="setting-title">{t('settings.remote')}</div>
      <div class="rc-status">
        <div class="rc-row">
          <span class="rc-dot" class:ok={hostInfo.moonlight}></span>
          <span><b>Moonlight</b> {t(hostInfo.moonlight ? 'settings.moonlightOk' : 'settings.moonlightMissing')}</span>
        </div>
        {#if !hostInfo.moonlight}
          <div class="rc-install">
            <button class="btn secondary sm" disabled={!!installing} onclick={() => install('moonlight')}>
              {installing === 'moonlight' ? t('common.installing') : t('settings.install', { name: 'Moonlight' })}
            </button>
            <span class="muted small">{t('settings.passwordHint')}</span>
          </div>
        {/if}
        <div class="rc-row">
          <span class="rc-dot" class:ok={hostInfo.sunshineRunning} class:warn={hostInfo.sunshineInstalled && !hostInfo.sunshineRunning}></span>
          <span><b>Sunshine</b>
            {t(hostInfo.sunshineRunning ? 'settings.sunRunning' : hostInfo.sunshineInstalled ? 'settings.sunStopped' : 'settings.sunMissing')}
          </span>
        </div>
        {#if !hostInfo.sunshineInstalled}
          <div class="rc-install">
            <button class="btn secondary sm" disabled={!!installing} onclick={() => install('sunshine')}>
              {installing === 'sunshine' ? t('common.installing') : t('settings.install', { name: 'Sunshine' })}
            </button>
            <span class="muted small">{t('settings.sunInstallHint')}</span>
          </div>
        {:else if hostInfo.sunshineBlocked}
          <div class="rc-install">
            <button class="btn primary sm" onclick={onAllowSunshine}>{t('settings.allowFw')}</button>
            <span class="muted small">{t('settings.sunBlocked')}</span>
          </div>
        {:else if !hostInfo.sunshineLogin}
          <div class="rc-install">
            <button class="btn secondary sm" onclick={onSunshineSetup}>{t('settings.openSetup')}</button>
            <span class="muted small">{t('settings.setupHint')}</span>
          </div>
        {/if}
      </div>
      {#if hostInfo.sunshineInstalled || hostInfo.sunshineRunning}
        {#if hostInfo.sunshineLoginError}
          <div class="rc-login-saved">
            <span class="form-error"><Icon name="alert" size={14} /> {t('settings.credentialError', { error: hostInfo.sunshineLoginError })}</span>
            <button class="text-btn small" disabled={sunBusy} onclick={removeSunshine}>{t('common.remove')}</button>
          </div>
        {/if}
        {#if hostInfo.sunshineLogin}
          <div class="rc-login-saved">
            <span class="muted small"><Icon name="check" size={13} stroke={3} /> {t('settings.loginSaved')}</span>
            <button class="text-btn small" disabled={sunBusy} onclick={removeSunshine}>{t('common.remove')}</button>
          </div>
        {:else}
          <form class="rc-login" onsubmit={saveSunshine}>
            <p class="muted small">
              {t('settings.loginBefore')} <button type="button" class="text-btn" onclick={onSunshineSetup}>{t('settings.loginLink')}</button>
              {t('settings.loginAfter')}
            </p>
            <div class="input-row">
              <input placeholder={t('settings.user')} bind:value={sunUser} autocomplete="off" dir={sunUser ? 'auto' : null} />
              <input placeholder={t('settings.password')} type="password" bind:value={sunPass} autocomplete="off" />
              <button class="btn secondary" disabled={sunBusy || !sunUser.trim() || !sunPass}>{sunBusy ? t('common.checking') : t('common.save')}</button>
            </div>
          </form>
        {/if}
        {#if sunError}<p class="form-error"><Icon name="alert" size={14} /> {sunError}</p>{/if}
      {/if}
    </section>

    <section class="setting">
      <div class="setting-title">{t('settings.security')}</div>
      <div class="security-row">
        <Icon name="lock" size={16} />
        <span>{t('settings.tls', { count: pairedCount })}</span>
      </div>
      <div class="security-row">
        <Icon name="key" size={16} />
        <span>{t('settings.key')} <bdi class="mono">{fingerprint}</bdi></span>
      </div>
    </section>

    <section class="setting">
      <div class="setting-title">{t('settings.phones')}</div>
      <div class="upd-row">
        <span class="fw-text">
          {phones.length ? t('settings.phonesConnected', { names: fmtList(phones.map((p) => p.name)) }) : t('settings.phonesText')}
        </span>
        <button class="btn primary sm" onclick={onConnectPhone}><Icon name="phone" size={14} /> {t('settings.connectPhone')}</button>
      </div>
    </section>

    <section class="setting">
      <div class="setting-title">{t('settings.history')}</div>
      <div class="upd-row">
        <span class="fw-text">{t('settings.historyText')}</span>
        <button class="btn secondary sm" onclick={onClearAll}>{t('settings.clearAll')}</button>
      </div>
    </section>

    <section class="setting">
      <div class="setting-title">{t('settings.received')}</div>
      <div class="input-row">
        <button class="path-box" title={t('settings.openFolder')} onclick={onOpenDir}>
          <Icon name="folder" size={16} />
          <span class="path-text">{'‎' + receiveDir + '‎'}</span>
        </button>
        <button class="btn secondary" onclick={onChangeDir}>{t('settings.change')}</button>
      </div>
      <label class="switch-row">
        <span class="switch-text">
          <span>{t('settings.askBeforeReceiving')}</span>
          <span class="muted small">{t('settings.askBeforeReceivingText')}</span>
        </span>
        <input type="checkbox" class="switch" role="switch" checked={receiveSettings.askBeforeAccepting}
          onchange={(e) => onAskBeforeReceiving(e.currentTarget.checked)} />
      </label>
    </section>

    <section class="setting">
      <div class="setting-title">{t('settings.appearance')}</div>
      <div class="segmented" role="radiogroup" aria-label={t('settings.theme')}>
        {#each ['system', 'light', 'dark'] as value (value)}
          <button role="radio" aria-checked={theme === value} class:on={theme === value} onclick={() => onTheme(value)}>
            {t('settings.' + value)}
          </button>
        {/each}
      </div>
    </section>
    <section class="setting">
      <div class="setting-title">{t('settings.updates')}</div>
      <div class="upd-row">
        <span class="fw-text">
          {#if updateProgress}
            {#if updateProgress.step === 'downloading'}{t('settings.downloading', { version: updateInfo?.latest ?? '' })} {updateProgress.total ? t('units.progress', { done: fmtSize(updateProgress.done), total: fmtSize(updateProgress.total) }) : ''}
            {:else if updateProgress.step === 'installing'}{t('settings.installingUpdate')}
            {:else}{t('settings.restarting')}{/if}
          {:else if updateInfo?.checking}{t('common.checking')}
          {:else if updateInfo?.error}<span class="error">{t('settings.checkFailed', { error: updateInfo.error })}</span>
          {:else if updateInfo?.available}<b>{t('settings.available', { version: updateInfo.latest })}</b> {t('settings.youHave', { version })}
          {:else if updateInfo}{t('settings.latest', { version })}
          {:else}{t('settings.current', { version })}{/if}
        </span>
        {#if updateInfo?.available && !updateProgress}
          <button class="btn primary sm" onclick={onInstallUpdate}>{updateInfo.canInstall ? t('settings.updateTo', { version: updateInfo.latest }) : t('settings.download')}</button>
        {:else if !updateProgress}
          <button class="btn secondary sm" disabled={updateInfo?.checking} onclick={onCheckUpdate}>{t('settings.check')}</button>
        {/if}
      </div>
      {#if updateProgress?.step === 'downloading' && updateProgress.total}
        <div class="bar"><div class="fill" style="width: {(updateProgress.done / updateProgress.total) * 100}%"></div></div>
      {/if}
      {#if updateInfo?.available && updateInfo.url}
        <button class="text-btn small" onclick={() => onOpenURL(updateInfo.url)}>{t('settings.whatsNew', { version: updateInfo.latest })}</button>
      {/if}
    </section>
  </div>
</div>
