<script setup lang="ts">
import type { Component } from 'vue'

withDefaults(
  defineProps<{
    title: string
    value: string | number
    unit?: string
    sub?: string
    icon: Component
    tone?: 'brand' | 'ok' | 'warn' | 'danger' | 'running'
    clickable?: boolean
  }>(),
  {
    unit: '',
    sub: '',
    tone: 'brand',
    clickable: false,
  }
)

defineEmits<{ (e: 'click'): void }>()
</script>

<template>
  <div
    class="ki-stat-card ki-surface"
    :class="[`ki-stat-card--${tone}`, { 'ki-stat-card--clickable': clickable }]"
    @click="clickable && $emit('click')"
  >
    <div class="ki-stat-card__icon" aria-hidden="true">
      <component :is="icon" :size="20" />
    </div>
    <div class="ki-stat-card__body">
      <div class="ki-stat-card__title">{{ title }}</div>
      <div class="ki-stat-card__val ki-num">
        {{ value }}<small v-if="unit">{{ unit }}</small>
      </div>
      <div v-if="sub || $slots.sub" class="ki-stat-card__sub">
        <slot name="sub">{{ sub }}</slot>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ki-stat-card {
  display: flex;
  gap: 14px;
  align-items: flex-start;
  height: 100%;
  padding: 16px 18px;
  transition: box-shadow var(--ki-dur-base) var(--ki-ease), border-color var(--ki-dur-base) var(--ki-ease);
}

.ki-stat-card--clickable {
  cursor: pointer;
}

.ki-stat-card--clickable:hover {
  box-shadow: var(--ki-shadow-hover);
  border-color: var(--ki-brand-border);
}

.ki-stat-card__icon {
  flex: none;
  width: 40px;
  height: 40px;
  border-radius: var(--ki-radius-card);
  display: grid;
  place-items: center;
}

.ki-stat-card--brand .ki-stat-card__icon {
  color: var(--ki-brand);
  background-color: var(--ki-brand-subtle);
}

.ki-stat-card--ok .ki-stat-card__icon {
  color: var(--ki-status-ok);
  background-color: var(--ki-status-ok-subtle);
}

.ki-stat-card--warn .ki-stat-card__icon {
  color: var(--ki-status-warn);
  background-color: var(--ki-status-warn-subtle);
}

.ki-stat-card--danger .ki-stat-card__icon {
  color: var(--ki-status-danger);
  background-color: var(--ki-status-danger-subtle);
}

.ki-stat-card--running .ki-stat-card__icon {
  color: var(--ki-status-running);
  background-color: var(--ki-status-running-subtle);
}

.ki-stat-card__body {
  min-width: 0;
  flex: 1;
}

.ki-stat-card__title {
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-sm);
}

.ki-stat-card__val {
  margin-top: 2px;
  font-size: var(--ki-fs-kpi);
  font-weight: var(--ki-fw-bold);
  line-height: 1.2;
  color: var(--ki-text-primary);
}

.ki-stat-card__val small {
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-medium);
  color: var(--ki-text-muted);
  margin-left: 4px;
}

.ki-stat-card__sub {
  margin-top: 6px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}
</style>
