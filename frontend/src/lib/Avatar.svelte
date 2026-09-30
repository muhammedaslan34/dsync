<script>
  import { initials, hueFor } from './format.js'

  // online: true/false shows a status dot; null hides it.
  let { name = '', id = '', size = 38, online = null } = $props()
</script>

<span class="avatar" style="--size:{size}px; --hue:{hueFor(id || name)}">
  {initials(name)}
  {#if online !== null}
    <span class="status" class:online></span>
  {/if}
</span>
