<script setup lang="ts">
import { computed, ref, watch, nextTick } from 'vue'
import { Search, CornerDownLeft, Keyboard } from 'lucide-vue-next'
import type { Component } from 'vue'
import { GLOBAL_SHORTCUTS } from '@/composables/useShortcuts'
import KiModal from './KiModal.vue'

export interface CommandItem {
  id: string
  label: string
  path?: string
  group?: string
  keywords?: string
  icon?: Component
  action?: () => void
}

const props = defineProps<{
  modelValue: boolean
  helpOpen: boolean
  items: CommandItem[]
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'update:helpOpen', v: boolean): void
  (e: 'select', item: CommandItem): void
}>()

const query = ref('')
const activeIdx = ref(0)
const inputEl = ref<HTMLInputElement | null>(null)

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return props.items
  return props.items.filter((it) => {
    const hay = `${it.label} ${it.path || ''} ${it.group || ''} ${it.keywords || ''}`.toLowerCase()
    return hay.includes(q)
  })
})

watch(
  () => props.modelValue,
  async (open) => {
    if (open) {
      query.value = ''
      activeIdx.value = 0
      await nextTick()
      inputEl.value?.focus()
    }
  }
)

watch(filtered, () => {
  activeIdx.value = 0
})

function pick(item: CommandItem) {
  emit('update:modelValue', false)
  if (item.action) item.action()
  else emit('select', item)
}

function onInputKeydown(e: KeyboardEvent) {
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    if (filtered.value.length) activeIdx.value = (activeIdx.value + 1) % filtered.value.length
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    if (filtered.value.length) activeIdx.value = (activeIdx.value - 1 + filtered.value.length) % filtered.value.length
  } else if (e.key === 'Enter' && filtered.value[activeIdx.value]) {
    e.preventDefault()
    pick(filtered.value[activeIdx.value])
  } else if (e.key === 'Escape') {
    emit('update:modelValue', false)
  }
}
</script>

<template>
  <Teleport to="body">
    <div
      v-if="modelValue"
      class="ki-cmdk-backdrop"
      @click.self="emit('update:modelValue', false)"
    >
      <div class="ki-cmdk" role="dialog" aria-modal="true" aria-label="全局命令面板">
        <div class="ki-cmdk__search">
          <Search :size="15" class="ki-muted" aria-hidden="true" />
          <input
            ref="inputEl"
            v-model="query"
            type="text"
            class="ki-cmdk__input"
            placeholder="输入模块名称、路径或命令… (↑↓ 选择，Enter 跳转)"
            @keydown="onInputKeydown"
          />
          <kbd class="ki-kbd">Esc</kbd>
        </div>
        <div class="ki-cmdk__list" role="listbox">
          <button
            v-for="(item, idx) in filtered"
            :key="item.id"
            type="button"
            role="option"
            :aria-selected="idx === activeIdx"
            class="ki-cmdk__item"
            :class="{ 'ki-cmdk__item--active': idx === activeIdx }"
            @mouseenter="activeIdx = idx"
            @click="pick(item)"
          >
            <component :is="item.icon" v-if="item.icon" :size="15" aria-hidden="true" />
            <span class="ki-cmdk__label">{{ item.label }}</span>
            <span v-if="item.path" class="ki-mono ki-muted ki-cmdk__path">{{ item.path }}</span>
            <CornerDownLeft v-if="idx === activeIdx" :size="13" class="ki-muted" aria-hidden="true" />
          </button>
          <div v-if="filtered.length === 0" class="ki-cmdk__empty">未找到匹配的页面或命令</div>
        </div>
      </div>
    </div>
  </Teleport>

  <KiModal
    :model-value="helpOpen"
    title="全局快捷键速查 (?)"
    size="md"
    @update:model-value="emit('update:helpOpen', $event)"
  >
    <template #title-prefix>
      <Keyboard :size="16" aria-hidden="true" />
    </template>
    <div class="ki-shortcut-list">
      <div v-for="sc in GLOBAL_SHORTCUTS" :key="sc.keys" class="ki-shortcut-row">
        <div>
          <span class="ki-shortcut-cat">{{ sc.category }}</span>
          <span class="ki-shortcut-desc">{{ sc.description }}</span>
        </div>
        <kbd class="ki-kbd ki-mono">{{ sc.keys }}</kbd>
      </div>
    </div>
  </KiModal>
</template>

<style scoped>
.ki-cmdk-backdrop {
  position: fixed;
  inset: 0;
  z-index: var(--ki-z-command);
  background-color: var(--ki-overlay);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding-top: 12vh;
}

.ki-cmdk {
  width: var(--ki-cmdk-w);
  max-width: 94vw;
  background-color: var(--ki-bg-elevated);
  border: 1px solid var(--ki-border-default);
  border-radius: var(--ki-radius-lg);
  box-shadow: var(--ki-shadow-modal);
  overflow: hidden;
}

.ki-cmdk__search {
  display: flex;
  align-items: center;
  gap: var(--ki-space-2);
  padding: var(--ki-space-3) var(--ki-space-4);
  border-bottom: 1px solid var(--ki-border-subtle);
}

.ki-cmdk__input {
  flex: 1;
  border: 0;
  background-color: transparent;
  color: var(--ki-text-primary);
  font-family: var(--ki-font-sans);
  font-size: var(--ki-fs-sub);
}

.ki-cmdk__list {
  max-height: 340px;
  overflow-y: auto;
  padding: var(--ki-space-2);
  display: flex;
  flex-direction: column;
  gap: var(--ki-space-half);
}

.ki-cmdk__item {
  display: flex;
  align-items: center;
  gap: var(--ki-space-2);
  width: 100%;
  padding: var(--ki-space-2) var(--ki-space-3);
  border: 0;
  border-radius: var(--ki-radius-sm);
  background-color: transparent;
  color: var(--ki-text-primary);
  font-family: var(--ki-font-sans);
  font-size: var(--ki-fs-body);
  text-align: left;
  cursor: pointer;
}

.ki-cmdk__item--active {
  background-color: var(--ki-brand-subtle);
}

.ki-cmdk__label {
  flex: 1;
}

.ki-cmdk__path {
  font-size: var(--ki-fs-xs);
}

.ki-cmdk__empty {
  padding: var(--ki-space-5);
  text-align: center;
  color: var(--ki-text-muted);
}

.ki-kbd {
  display: inline-flex;
  align-items: center;
  padding: 2px 6px;
  border-radius: var(--ki-radius-xs);
  background-color: var(--ki-bg-subtle);
  border: 1px solid var(--ki-border-default);
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-xs);
}

.ki-shortcut-list {
  display: flex;
  flex-direction: column;
  gap: var(--ki-space-2);
}

.ki-shortcut-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--ki-space-3);
  padding: var(--ki-space-2) 0;
  border-bottom: 1px solid var(--ki-border-subtle);
}

.ki-shortcut-cat {
  display: inline-block;
  min-width: 64px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.ki-shortcut-desc {
  color: var(--ki-text-primary);
}
</style>
