<script setup lang="ts">
withDefaults(
  defineProps<{
    title?: string
    subtitle?: string
    padded?: boolean
  }>(),
  {
    title: '',
    subtitle: '',
    padded: true,
  }
)
</script>

<template>
  <section class="ki-panel-card ki-surface">
    <header v-if="title || $slots.header || $slots.actions" class="ki-panel-card__head">
      <div class="ki-panel-card__title-wrap">
        <slot name="header">
          <h2 class="ki-panel-card__title">{{ title }}</h2>
          <p v-if="subtitle" class="ki-panel-card__sub">{{ subtitle }}</p>
        </slot>
      </div>
      <div v-if="$slots.actions" class="ki-flex-row">
        <slot name="actions" />
      </div>
    </header>
    <div v-if="$slots.toolbar" class="ki-panel-card__toolbar">
      <slot name="toolbar" />
    </div>
    <div :class="{ 'ki-panel-card__body': padded }">
      <slot />
    </div>
  </section>
</template>

<style scoped>
.ki-panel-card {
  overflow: hidden;
}

.ki-panel-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--ki-space-3);
  padding: var(--ki-space-3) var(--ki-panel-pad);
  border-bottom: 1px solid var(--ki-border-subtle);
}

.ki-panel-card__title-wrap {
  min-width: 0;
}

.ki-panel-card__title {
  font-size: var(--ki-fs-section);
  line-height: var(--ki-lh-section);
  color: var(--ki-text-primary);
}

.ki-panel-card__sub {
  margin: var(--ki-space-half) 0 0;
  font-size: var(--ki-fs-sm);
  color: var(--ki-text-muted);
}

.ki-panel-card__toolbar {
  padding: var(--ki-space-2) var(--ki-panel-pad);
  background-color: var(--ki-bg-subtle);
  border-bottom: 1px solid var(--ki-border-subtle);
}

.ki-panel-card__body {
  padding: var(--ki-panel-pad);
}
</style>
