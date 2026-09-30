<script>
  import { onMount } from 'svelte'
  import { AddPeer, FindOnNetwork } from '../../wailsjs/go/main/App'
  import Icon from './Icon.svelte'
  import Avatar from './Avatar.svelte'
  import { osLabel } from './format.js'
  import { t } from './i18n.svelte.js'

  let { peers, localAddrs, onClose, onConnected, onCopy } = $props()

  let searching = $state(false)
  let results = $state(null) // null until the first search finishes
  let addr = $state('')
  let busy = $state(false)
  let error = $state('')
  let hint = $state('')

  onMount(search)

  async function search() {
    searching = true
    try {
      results = (await FindOnNetwork()) ?? []
    } finally {
      searching = false
    }
  }

  async function connect(a) {
    busy = true
    error = ''
    try {
      const p = await AddPeer(a)
      onConnected(p.id)
    } catch (e) {
      error = String(e)
      hint = hintFor(a, error)
    } finally {
      busy = false
    }
  }

  // hintFor explains the usual reasons a device can't be reached.
  function hintFor(a, err) {
    const onTailscale = localAddrs.some((x) => x.kind === 'Tailscale')
    if (/^100\.(6[4-9]|[7-9]\d|1[01]\d|12[0-7])\./.test(a.trim()) && !onTailscale) {
      return 'connect.hintTailscale'
    }
    if (/deadline|timeout|refused|no route/i.test(err)) {
      return 'connect.hintNoAnswer'
    }
    return ''
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="overlay" role="presentation" onclick={(e) => { if (e.target === e.currentTarget) onClose() }}>
  <div class="dialog" role="dialog" aria-label={t('connect.title')}>
    <header class="dialog-head">
      <div>
        <h2>{t('connect.title')}</h2>
        <p class="muted">{t('connect.text')}</p>
      </div>
      <button class="icon-btn" title={t('common.close')} onclick={onClose}><Icon name="x" /></button>
    </header>

    <div class="section-head">
      <span class="label">{t('connect.onNetwork')}</span>
      <button class="text-btn" onclick={search} disabled={searching}>
        <span class:spin={searching}><Icon name="refresh" size={14} /></span>
        {searching ? t('connect.searching') : t('connect.searchAgain')}
      </button>
    </div>

    <div class="results">
      {#if searching && !results?.length}
        {#each [0, 1] as i (i)}
          <div class="result skeleton"><span class="sk-avatar"></span><span class="sk-lines"><span></span><span></span></span></div>
        {/each}
      {:else if results?.length}
        {#each results as r (r.id)}
          {@const known = peers.some((p) => p.id === r.id)}
          <div class="result">
            <Avatar name={r.name} id={r.id} size={34} online={true} />
            <span class="device-text">
              <span class="device-name"><bdi>{r.name}</bdi></span>
              <span class="muted small"><bdi>{osLabel[r.os] ?? r.os}</bdi> · <bdi class="ltr">{r.addr}</bdi></span>
            </span>
            <button class="btn {known ? 'secondary' : 'primary'} sm" disabled={busy} onclick={() => connect(r.addr)}>
              {known ? t('common.open') : t('common.connect')}
            </button>
          </div>
        {/each}
      {:else if results}
        <div class="results-empty">
          <Icon name="search" size={22} />
          <div>
            <b>{t('connect.noneTitle')}</b>
            <p class="muted small">{t('connect.noneText')}</p>
          </div>
        </div>
      {/if}
    </div>

    <form class="addr-form" onsubmit={(e) => { e.preventDefault(); connect(addr) }}>
      <label class="label" for="addr-input">{t('connect.byAddress')}</label>
      <div class="input-row">
        <input id="addr-input" bind:value={addr} placeholder={t('connect.placeholder')} dir={addr ? 'ltr' : null} />
        <button type="submit" class="btn primary" disabled={busy || !addr.trim()}>
          {busy ? t('common.connecting') : t('common.connect')}
        </button>
      </div>
      {#if error}<p class="form-error"><Icon name="alert" size={14} /> {error}</p>{/if}
      {#if hint}<p class="form-hint">{t(hint)}</p>{/if}
    </form>

    {#if localAddrs.length}
      <footer class="dialog-foot">
        <span class="muted small">{t('connect.thisComputer')}</span>
        {#each localAddrs as a (a.ip)}
          <button class="addr-chip" title={t('common.copy')} onclick={() => onCopy(a.ip)}>
            <span class="mono" dir="ltr">{a.ip}</span><span class="muted">{a.kind}</span><Icon name="copy" size={12} />
          </button>
        {/each}
      </footer>
    {/if}
  </div>
</div>
