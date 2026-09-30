<script>
  import Icon from './Icon.svelte'
  import Avatar from './Avatar.svelte'
  import { osLabel } from './format.js'

  let {
    self, receiveDir, localAddrs, theme, fingerprint, pairedCount, clipStatus, onClipboard,
    onClose, onRename, onChangeDir, onOpenDir, onCopy, onTheme,
  } = $props()

  let name = $state('')
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
  </div>
</div>
