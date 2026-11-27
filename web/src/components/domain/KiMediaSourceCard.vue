<script setup lang="ts">
import { computed } from 'vue'
import { GripVertical, ArrowUp, ArrowDown, Lock, HardDrive, Cloud, Globe, RefreshCw } from 'lucide-vue-next'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiButton from '@/components/ki/KiButton.vue'
import KiMonoText from '@/components/ki/KiMonoText.vue'

export interface MediaSourceItem {
  id: string
  type: 'local' | 'obs' | 's3' | 'online'
  name: string
  endpoint: string
  enabled: boolean
  probeStatus?: 'ok' | 'warn' | 'failed' | 'pending' | 'unprobed' | 'blocked'
  latencyMs?: number
  probeNote?: string
  lastChecked?: string
}

const props = withDefaults(
  defineProps<{
    source: MediaSourceItem
    index: number
    total: number
    offlineStrict?: boolean
    probing?: boolean
  }>(),
  {
    offlineStrict: false,
    probing: false,
  }
)

const emit = defineEmits<{
  (e: 'move-up', index: number): void
  (e: 'move-down', index: number): void
  (e: 'probe', source: MediaSourceItem): void
}>()

const lockedOffline = computed(
  () =>
    props.offlineStrict &&
    (props.source.type === 'online' || ((props.source.type === 'obs' || props.source.type === 's3') && !props.source.enabled))
)

function onKeydown(e: KeyboardEvent) {
  if (!e.altKey) return
  if (e.key === 'ArrowUp' && props.index > 0) {
    e.preventDefault()
    emit('move-up', props.index)
  } else if (e.key === 'ArrowDown' && props.index < props.total - 1) {
    e.preventDefault()
    emit('move-down', props.index)
  }
}

const typeLabel = computed(() => {
  switch (props.source.type) {
    case 'local':
      return 'Local 离线仓库'
    case 'obs':
      return '华为云 OBS'
    case 's3':
      return 'S3 兼容对象存储'
    default:
      return 'Online 上游源'
  }
})
</script>

<template>
  <article
    class="ki-media-card ki-surface"
    :class="{ 'ki-media-card--locked': lockedOffline }"
    tabindex="0"
    :aria-label="`介质源 ${index + 1}: ${source.name}，按 Alt+上下方向键调整优先级`"
    @keydown="onKeydown"
  >
    <div class="ki-media-card__left">
      <span class="ki-media-card__grip" title="按 Alt+↑/↓ 调整优先级" aria-hidden="true">
        <GripVertical :size="15" />
      </span>
      <span class="ki-media-card__rank ki-mono">[{{ index + 1 }}]</span>

      <div class="ki-media-card__meta">
        <div class="ki-flex-row ki-flex-wrap">
          <HardDrive v-if="source.type === 'local'" :size="14" class="ki-muted" />
          <Cloud v-else-if="source.type === 'obs' || source.type === 's3'" :size="14" class="ki-muted" />
          <Globe v-else :size="14" class="ki-muted" />
          <strong class="ki-media-card__name">{{ source.name }}</strong>
          <span class="ki-badge ki-media-card__type">{{ typeLabel }}</span>
          <span v-if="lockedOffline" class="ki-badge ki-media-card__lock">
            <Lock :size="11" /> 离线策略拦截
          </span>
        </div>
        <div class="ki-media-card__endpoint">
          <KiMonoText :value="source.endpoint" :head="34" :tail="14" />
          <span v-if="source.probeNote && !lockedOffline" class="ki-media-card__note">· {{ source.probeNote }}</span>
        </div>
      </div>
    </div>

    <div class="ki-media-card__right">
      <div class="ki-media-card__probe">
        <KiStatusBadge :status="lockedOffline ? 'blocked' : (source.probeStatus || 'unprobed')" />
        <span v-if="source.latencyMs !== undefined && !lockedOffline" class="ki-mono ki-muted ki-media-card__lat">
          {{ source.latencyMs }}ms
        </span>
      </div>

      <div class="ki-flex-row">
        <KiButton
          variant="ghost"
          size="sm"
          :disabled="index === 0"
          aria-label="上移优先级"
          @click="emit('move-up', index)"
        >
          <ArrowUp :size="13" />
        </KiButton>
        <KiButton
          variant="ghost"
          size="sm"
          :disabled="index >= total - 1"
          aria-label="下移优先级"
          @click="emit('move-down', index)"
        >
          <ArrowDown :size="13" />
        </KiButton>
        <KiButton
          variant="secondary"
          size="sm"
          :disabled="lockedOffline"
          :loading="probing"
          @click="emit('probe', source)"
        >
          <RefreshCw :size="12" /> 探测
        </KiButton>
      </div>
    </div>
  </article>
</template>

<style scoped>
.ki-media-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--ki-space-4);
  padding: var(--ki-space-3) var(--ki-space-4);
  transition: border-color var(--ki-dur-fast) var(--ki-ease), opacity var(--ki-dur-fast) var(--ki-ease);
}

.ki-media-card:hover {
  border-color: var(--ki-border-strong);
}

.ki-media-card--locked {
  opacity: 0.55;
  border-style: dashed;
}

.ki-media-card__left {
  display: flex;
  align-items: center;
  gap: var(--ki-space-3);
  min-width: 0;
}

.ki-media-card__grip {
  color: var(--ki-text-muted);
  cursor: grab;
  display: inline-flex;
}

.ki-media-card__rank {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 30px;
  height: 24px;
  border-radius: var(--ki-radius-xs);
  background-color: var(--ki-brand-subtle);
  color: var(--ki-brand-hover);
  font-weight: var(--ki-fw-semibold);
}

.ki-media-card__meta {
  display: flex;
  flex-direction: column;
  gap: var(--ki-space-1);
  min-width: 0;
}

.ki-media-card__name {
  color: var(--ki-text-primary);
}

.ki-media-card__type {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-secondary);
}

.ki-media-card__lock {
  background-color: var(--ki-status-warn-subtle);
  color: var(--ki-status-warn-text);
}

.ki-media-card__endpoint {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-secondary);
}

.ki-media-card__note {
  color: var(--ki-status-warn-text);
  font-size: var(--ki-fs-xs);
}

.ki-media-card__right {
  display: flex;
  align-items: center;
  gap: var(--ki-space-3);
  flex-shrink: 0;
}

.ki-media-card__probe {
  display: flex;
  align-items: center;
  gap: var(--ki-space-2);
}

.ki-media-card__lat {
  font-size: var(--ki-fs-xs);
}
</style>
