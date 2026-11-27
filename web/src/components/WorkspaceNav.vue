<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { workspaceForPath } from '@/config/menu'

const route = useRoute()
const workspace = computed(() => workspaceForPath(route.path))
</script>

<template>
  <nav v-if="workspace" class="ki-workspace-nav" :aria-label="workspace.title">
    <router-link
      v-for="item in workspace.items"
      :key="item.path"
      :to="item.path"
      class="ki-workspace-link"
      :class="{ 'is-active': route.path === item.path }"
      :aria-current="route.path === item.path ? 'page' : undefined"
    >
      {{ item.label }}
    </router-link>
  </nav>
</template>

<style scoped>
.ki-workspace-nav {
  display: flex;
  gap: var(--ki-space-1);
  padding-bottom: 10px;
  margin-bottom: var(--ki-space-4);
  border-bottom: 1px solid var(--ki-border-default);
  overflow-x: auto;
}

.ki-workspace-link {
  flex: 0 0 auto;
  padding: 8px 16px;
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-sub);
  border-radius: var(--ki-radius-sm);
  white-space: nowrap;
  text-decoration: none;
  transition: background-color var(--ki-dur-fast) var(--ki-ease), color var(--ki-dur-fast) var(--ki-ease);
}

.ki-workspace-link:hover {
  color: var(--ki-brand);
  background-color: var(--ki-bg-subtle);
  text-decoration: none;
}

.ki-workspace-link.is-active {
  color: var(--ki-brand);
  background-color: var(--ki-sider-active-bg);
  font-weight: var(--ki-fw-semibold);
}
</style>
