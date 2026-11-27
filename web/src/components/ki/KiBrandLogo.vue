<template>
  <span
    class="ki-brand-logo"
    :class="[`ki-brand-logo--${variant}`]"
    :style="{ width: `${size}px`, height: `${size}px` }"
    aria-hidden="true"
  >
    <svg
      :width="svgSize"
      :height="svgSize"
      viewBox="0 0 32 32"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <!-- Top orchestration plane -->
      <path
        d="M16 3.5L27.5 9.8L16 16.1L4.5 9.8L16 3.5Z"
        fill="currentColor"
        fill-opacity="0.96"
      />
      <!-- Middle middleware layer -->
      <path
        d="M5.6 15.7L16 21.4L26.4 15.7"
        stroke="currentColor"
        stroke-width="2.4"
        stroke-linecap="round"
        stroke-linejoin="round"
        stroke-opacity="0.82"
      />
      <!-- Base infrastructure layer -->
      <path
        d="M5.6 21.8L16 27.5L26.4 21.8"
        stroke="currentColor"
        stroke-width="2.4"
        stroke-linecap="round"
        stroke-linejoin="round"
        stroke-opacity="0.54"
      />
      <!-- Precision core node -->
      <circle cx="16" cy="9.8" r="2.1" class="ki-brand-logo__core" />
    </svg>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    size?: number
    variant?: 'badge' | 'hero' | 'mono'
  }>(),
  {
    size: 32,
    variant: 'badge',
  },
)

const svgSize = computed(() => {
  if (props.variant === 'mono') return props.size
  return Math.round(props.size * 0.64)
})
</script>

<style scoped>
.ki-brand-logo {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  user-select: none;
}

.ki-brand-logo--badge {
  border-radius: var(--ki-radius-md);
  background: var(--ki-brand-logo-bg);
  color: var(--ki-text-on-solid);
  box-shadow: var(--ki-brand-logo-shadow);
}

.ki-brand-logo--hero {
  border-radius: var(--ki-radius-card);
  background: var(--ki-brand-logo-bg);
  color: var(--ki-text-on-solid);
  box-shadow: var(--ki-brand-logo-hero-shadow);
}

.ki-brand-logo--mono {
  color: var(--ki-brand);
}

.ki-brand-logo__core {
  fill: var(--ki-brand-logo-core);
}
</style>
