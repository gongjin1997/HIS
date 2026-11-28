<script setup lang="ts">
import type { Component } from 'vue'

defineProps<{
  title: string
  description: string
  icon?: Component
  badge?: string
  tabs?: { key: string; label: string; count?: number }[]
  activeTab?: string
}>()

defineEmits<{
  (e: 'update:activeTab', key: string): void
}>()
</script>

<template>
  <section class="ks-banner">
    <div class="ks-banner__main">
      <div class="ks-banner__head">
        <div v-if="icon" class="ks-banner__icon">
          <component :is="icon" :size="24" :stroke-width="1.9" />
        </div>
        <div class="ks-banner__text">
          <div class="ks-banner__title-row">
            <h1 class="ks-banner__title">{{ title }}</h1>
            <span v-if="badge" class="ks-banner__badge">{{ badge }}</span>
          </div>
          <p class="ks-banner__desc">{{ description }}</p>
        </div>
      </div>
      <div v-if="$slots.actions" class="ks-banner__actions">
        <slot name="actions" />
      </div>
    </div>
    <div v-if="tabs && tabs.length > 0" class="ks-banner__tabs" role="tablist">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        type="button"
        role="tab"
        :aria-selected="activeTab === tab.key"
        class="ks-banner__tab"
        :class="{ 'is-active': activeTab === tab.key }"
        @click="$emit('update:activeTab', tab.key)"
      >
        <span>{{ tab.label }}</span>
        <span v-if="tab.count !== undefined" class="ks-banner__tab-count">{{ tab.count }}</span>
      </button>
    </div>
  </section>
</template>

<style scoped>
.ks-banner {
  background-color: var(--ki-banner-bg);
  border: 1px solid var(--ki-banner-border);
  border-radius: var(--ki-radius-md);
  box-shadow: var(--ki-shadow-surface);
  margin-bottom: var(--ki-space-4);
  overflow: hidden;
}

.ks-banner__main {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--ki-space-4);
  padding: 18px 24px;
}

@media (min-width: 1024px) {
  .ks-banner__main {
    flex-wrap: nowrap;
  }
}

.ks-banner__head {
  display: flex;
  align-items: center;
  gap: var(--ki-space-4);
  flex: 1;
  min-width: 0;
}

.ks-banner__icon {
  width: 48px;
  height: 48px;
  border-radius: var(--ki-radius-lg);
  background-color: var(--ki-bg-canvas);
  border: 1px solid var(--ki-border-subtle);
  color: var(--ki-text-primary);
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
}

.ks-banner__text {
  min-width: 0;
}

.ks-banner__title-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--ki-space-2);
}

.ks-banner__title {
  font-size: var(--ki-fs-page);
  font-weight: var(--ki-fw-bold);
  color: var(--ki-text-primary);
  line-height: 1.3;
  letter-spacing: var(--ki-tracking-snug);
}

.ks-banner__badge {
  display: inline-flex;
  align-items: center;
  height: 22px;
  padding: 0 8px;
  border-radius: var(--ki-radius-pill);
  background-color: var(--ki-status-ok-subtle);
  border: 1px solid var(--ki-status-ok-border);
  color: var(--ki-status-ok-text);
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-semibold);
}

.ks-banner__desc {
  margin: 4px 0 0;
  font-size: var(--ki-fs-sm);
  color: var(--ki-text-muted);
  line-height: 1.5;
}

.ks-banner__actions {
  display: flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: var(--ki-space-2);
  flex: none;
}

.ks-banner__tabs {
  display: flex;
  align-items: center;
  gap: var(--ki-space-1);
  padding: 0 20px;
  background-color: var(--ki-bg-subtle);
  border-top: 1px solid var(--ki-border-subtle);
}

.ks-banner__tab {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 40px;
  padding: 0 14px;
  border: none;
  background: transparent;
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-medium);
  cursor: pointer;
  transition: color var(--ki-dur-fast) var(--ki-ease);
}

.ks-banner__tab:hover {
  color: var(--ki-text-primary);
}

.ks-banner__tab.is-active {
  color: var(--ki-brand);
  font-weight: var(--ki-fw-semibold);
}

.ks-banner__tab.is-active::after {
  content: "";
  position: absolute;
  left: 12px;
  right: 12px;
  bottom: 0;
  height: 2px;
  background-color: var(--ki-ks-green);
  border-radius: var(--ki-radius-pill);
}

.ks-banner__tab-count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 20px;
  height: 18px;
  padding: 0 6px;
  border-radius: var(--ki-radius-pill);
  background-color: var(--ki-bg-canvas);
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-xs);
  font-variant-numeric: tabular-nums;
}
</style>
