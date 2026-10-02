<script>
  import Icon from './Icon.svelte'
  import Avatar from './Avatar.svelte'
  import { osLabel } from './format.js'
  import { t } from './i18n.svelte.js'

  // Outgoing: { kind: 'out', peerId, name, code, error }
  // Incoming: { kind: 'in', id, peerId, name, os, code }
  let { pair, onCancel, onRetry, onAnswer } = $props()
</script>

<div class="overlay">
  <div class="dialog pair-dialog" role="dialog" aria-label={t('pair.label')}>
    <div class="pair-avatars">
      <Avatar name={pair.name} id={pair.peerId} size={52} />
      <span class="pair-link"><Icon name="lock" size={16} /></span>
    </div>

    {#if pair.kind === 'in'}
      <h2>{t('pair.inTitle', { name: pair.name })}</h2>
      <p class="muted">{osLabel[pair.os] ?? pair.os} · {t('pair.inText')}</p>
    {:else if pair.error}
      <h2>{t('pair.failedTitle')}</h2>
      <p class="muted">{pair.error[0].toUpperCase() + pair.error.slice(1)}.</p>
    {:else}
      <h2>{t('pair.outTitle', { name: pair.name })}</h2>
      <p class="muted">{t('pair.outText', { name: pair.name })}</p>
    {/if}

    {#if !pair.error}
      <div class="pair-code" dir="ltr" aria-label={t('pair.code')}>{pair.code}</div>
      {#if pair.fingerprint}
        <p class="muted small">{t('pair.fingerprint')} <bdi class="mono" dir="ltr">{pair.fingerprint}</bdi></p>
      {/if}
    {/if}

    {#if pair.kind === 'in'}
      {#if pair.keyChanged}
        <p class="pair-warn"><Icon name="alert" size={15} /> {t('pair.keyChanged')}</p>
      {:else if pair.alreadyPaired}
        <p class="pair-warn"><Icon name="alert" size={15} /> {t('pair.alreadyPaired')}</p>
      {/if}
      <p class="pair-warn"><Icon name="shield" size={15} /> {t('pair.warn', { name: pair.name })}</p>
      <div class="dialog-actions">
        {#if pair.keyChanged}
          <button class="btn primary" onclick={() => onAnswer(false)}>{t('common.close')}</button>
        {:else}
          <button class="btn secondary" onclick={() => onAnswer(false)}>{t('pair.decline')}</button>
          <button class="btn primary" onclick={() => onAnswer(true)}>{t('pair.accept')}</button>
        {/if}
      </div>
    {:else if pair.error}
      <div class="dialog-actions">
        <button class="btn secondary" onclick={onCancel}>{t('common.close')}</button>
        <button class="btn primary" onclick={onRetry}>{t('common.tryAgain')}</button>
      </div>
    {:else}
      <p class="pair-wait muted small"><span class="spinner"></span> {t('pair.waiting', { name: pair.name })}</p>
      <div class="dialog-actions">
        <button class="btn secondary" onclick={onCancel}>{t('common.cancel')}</button>
      </div>
    {/if}
  </div>
</div>
