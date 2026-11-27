<script setup lang="ts" generic="T extends Record<string, unknown>">
import { computed, ref } from 'vue'
import { Columns3, Maximize2, ChevronLeft, ChevronRight } from 'lucide-vue-next'
import KiButton from './KiButton.vue'
import KiEmptyState from './KiEmptyState.vue'
import { useWorkspaceStore } from '@/stores/workspace'

export interface KiTableColumn {
  key: string
  label: string
  width?: string
  mono?: boolean
  align?: 'left' | 'right' | 'center'
  defaultVisible?: boolean
}

const props = withDefaults(
  defineProps<{
    columns: KiTableColumn[]
    rows: T[]
    rowKey?: string
    loading?: boolean
    hasPrev?: boolean
    hasNext?: boolean
    totalLabel?: string
    emptyTitle?: string
    emptyDescription?: string
  }>(),
  {
    rowKey: 'id',
    loading: false,
    hasPrev: false,
    hasNext: false,
    totalLabel: '',
    emptyTitle: '暂无记录',
    emptyDescription: '当前筛选条件下没有可展示的数据。',
  }
)

const emit = defineEmits<{
  (e: 'prev'): void
  (e: 'next'): void
  (e: 'row-click', row: T): void
}>()

const ws = useWorkspaceStore()
const showColMenu = ref(false)
const hiddenKeys = ref<Set<string>>(
  new Set(props.columns.filter((c) => c.defaultVisible === false).map((c) => c.key))
)

const visibleColumns = computed(() =>
  props.columns.filter((c) => !hiddenKeys.value.has(c.key))
)

function toggleCol(key: string) {
  const next = new Set(hiddenKeys.value)
  if (next.has(key)) next.delete(key)
  else if (visibleColumns.value.length > 1) next.add(key)
  hiddenKeys.value = next
}

function getRowKey(row: T, idx: number): string {
  const v = row[props.rowKey]
  return v !== undefined && v !== null ? String(v) : String(idx)
}
</script>

<template>
  <div class="ki-table-shell ki-surface">
    <div class="ki-table-toolbar">
      <div class="ki-flex-row">
        <slot name="toolbar" />
      </div>
      <div class="ki-flex-row ki-table-toolbar__right">
        <span v-if="totalLabel" class="ki-muted ki-num">{{ totalLabel }}</span>
        <KiButton
          variant="ghost"
          size="sm"
          :aria-label="ws.density === 'compact' ? '切换到舒适密度 (40px)' : '切换到紧凑密度 (32px)'"
          @click="ws.toggleDensity"
        >
          <Maximize2 :size="13" />
          {{ ws.density === 'compact' ? '紧凑' : '舒适' }}
        </KiButton>
        <div class="ki-col-picker">
          <KiButton variant="ghost" size="sm" aria-label="列显示设置" @click="showColMenu = !showColMenu">
            <Columns3 :size="13" />
            列
          </KiButton>
          <div v-if="showColMenu" class="ki-col-menu" role="menu">
            <label v-for="col in columns" :key="col.key" class="ki-col-menu__item">
              <input
                type="checkbox"
                :checked="!hiddenKeys.has(col.key)"
                @change="toggleCol(col.key)"
              />
              <span>{{ col.label }}</span>
            </label>
          </div>
        </div>
      </div>
    </div>

    <div class="ki-table-scroll">
      <table class="ki-table">
        <thead>
          <tr>
            <th
              v-for="col in visibleColumns"
              :key="col.key"
              :style="col.width ? { width: col.width } : undefined"
              :class="`ki-align-${col.align || 'left'}`"
            >
              {{ col.label }}
            </th>
          </tr>
        </thead>
        <tbody v-if="loading">
          <tr v-for="n in 5" :key="n" class="ki-skeleton-row">
            <td v-for="col in visibleColumns" :key="col.key">
              <span class="ki-skeleton-bar" />
            </td>
          </tr>
        </tbody>
        <tbody v-else-if="rows.length > 0">
          <tr
            v-for="(row, idx) in rows"
            :key="getRowKey(row, idx)"
            class="ki-table-row"
            @click="emit('row-click', row)"
          >
            <td
              v-for="col in visibleColumns"
              :key="col.key"
              :class="[{ 'ki-mono': col.mono }, `ki-align-${col.align || 'left'}`]"
            >
              <slot :name="`cell-${col.key}`" :row="row" :value="row[col.key]">
                {{ row[col.key] ?? '—' }}
              </slot>
            </td>
          </tr>
        </tbody>
      </table>

      <KiEmptyState
        v-if="!loading && rows.length === 0"
        variant="no-result"
        :title="emptyTitle"
        :description="emptyDescription"
      />
    </div>

    <div v-if="hasPrev || hasNext" class="ki-table-footer">
      <span class="ki-muted">游标分页</span>
      <div class="ki-flex-row">
        <KiButton variant="secondary" size="sm" :disabled="!hasPrev" @click="emit('prev')">
          <ChevronLeft :size="13" /> 上一页
        </KiButton>
        <KiButton variant="secondary" size="sm" :disabled="!hasNext" @click="emit('next')">
          下一页 <ChevronRight :size="13" />
        </KiButton>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ki-table-shell {
  overflow: hidden;
}

.ki-table-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--ki-space-3);
  padding: var(--ki-space-2) var(--ki-space-3);
  border-bottom: 1px solid var(--ki-border-subtle);
  flex-wrap: wrap;
}

.ki-table-toolbar__right {
  position: relative;
  font-size: var(--ki-fs-sm);
}

.ki-col-picker {
  position: relative;
}

.ki-col-menu {
  position: absolute;
  right: 0;
  top: calc(100% + var(--ki-space-1));
  z-index: var(--ki-z-dropdown);
  min-width: 168px;
  padding: var(--ki-space-2);
  background-color: var(--ki-bg-elevated);
  border: 1px solid var(--ki-border-default);
  border-radius: var(--ki-radius-md);
  box-shadow: var(--ki-shadow-popover);
  display: flex;
  flex-direction: column;
  gap: var(--ki-space-1);
}

.ki-col-menu__item {
  display: flex;
  align-items: center;
  gap: var(--ki-space-2);
  font-size: var(--ki-fs-sm);
  color: var(--ki-text-primary);
  cursor: pointer;
}

.ki-table-scroll {
  overflow-x: auto;
}

.ki-table {
  width: 100%;
  border-collapse: collapse;
}

.ki-table th {
  height: var(--ki-table-header-h);
  padding: 0 var(--ki-space-3);
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-medium);
  border-bottom: 1px solid var(--ki-border-subtle);
  white-space: nowrap;
}

.ki-table td {
  height: var(--ki-row-h);
  padding: var(--ki-space-1) var(--ki-space-3);
  border-bottom: 1px solid var(--ki-border-subtle);
  color: var(--ki-text-primary);
  vertical-align: middle;
}

.ki-table-row:hover td {
  background-color: var(--ki-bg-subtle);
}

.ki-align-left {
  text-align: left;
}

.ki-align-right {
  text-align: right;
}

.ki-align-center {
  text-align: center;
}

.ki-skeleton-bar {
  display: block;
  height: 12px;
  width: 72%;
  border-radius: var(--ki-radius-xs);
  background-color: var(--ki-bg-subtle);
  animation: ki-skel 1.4s ease-in-out infinite;
}

@keyframes ki-skel {
  0%,
  100% {
    opacity: 0.45;
  }

  50% {
    opacity: 0.9;
  }
}

.ki-table-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--ki-space-2) var(--ki-space-3);
  border-top: 1px solid var(--ki-border-subtle);
  font-size: var(--ki-fs-sm);
}
</style>
