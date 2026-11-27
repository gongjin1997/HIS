<script setup lang="ts">
import { ShieldCheck, ShieldAlert, RefreshCw, Fingerprint } from 'lucide-vue-next'
import KiButton from '@/components/ki/KiButton.vue'
import KiMonoText from '@/components/ki/KiMonoText.vue'

withDefaults(
  defineProps<{
    valid: boolean
    lastSeq: number
    totalChecked: number
    brokenAtSeq?: number
    sm3Enabled?: boolean
    latestHash?: string
    verifying?: boolean
  }>(),
  {
    brokenAtSeq: 0,
    sm3Enabled: false,
    latestHash: '',
    verifying: false,
  }
)

const emit = defineEmits<{
  (e: 'verify'): void
  (e: 'jump-tamper', seq: number): void
}>()
</script>

<template>
  <div class="ki-audit-badge ki-surface" :class="valid ? 'ki-audit-badge--ok' : 'ki-audit-badge--broken'">
    <div class="ki-flex-row ki-flex-wrap">
      <ShieldCheck v-if="valid" :size="18" class="ki-audit-badge__ok-icon" aria-hidden="true" />
      <ShieldAlert v-else :size="18" class="ki-audit-badge__err-icon" aria-hidden="true" />

      <div class="ki-flex-col ki-audit-badge__meta">
        <div class="ki-flex-row">
          <strong>{{ valid ? '审计防篡改链校验通过' : '检测到审计链断裂/篡改！' }}</strong>
          <span class="ki-badge ki-mono ki-audit-badge__alg">
            <Fingerprint :size="11" />
            {{ sm3Enabled ? 'SHA-256 + SM3 (国密双链)' : 'SHA-256' }}
          </span>
          <span class="ki-badge ki-mono ki-audit-badge__seq">
            seq #{{ lastSeq }} · {{ totalChecked }} 条
          </span>
        </div>
        <div v-if="latestHash" class="ki-flex-row ki-audit-badge__hash">
          <span class="ki-muted">最新链尾哈希：</span>
          <KiMonoText :value="latestHash" :head="16" :tail="8" />
        </div>
      </div>
    </div>

    <div class="ki-flex-row">
      <KiButton
        v-if="!valid && brokenAtSeq > 0"
        variant="danger-ghost"
        size="sm"
        @click="emit('jump-tamper', brokenAtSeq)"
      >
        定位篡改点 #{{ brokenAtSeq }}
      </KiButton>
      <KiButton variant="secondary" size="sm" :loading="verifying" @click="emit('verify')">
        <RefreshCw :size="13" /> 立即重验
      </KiButton>
    </div>
  </div>
</template>

<style scoped>
.ki-audit-badge {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--ki-space-4);
  padding: var(--ki-space-3) var(--ki-space-4);
  flex-wrap: wrap;
}

.ki-audit-badge--ok {
  border-color: var(--ki-status-ok-border);
  background-color: var(--ki-status-ok-subtle);
}

.ki-audit-badge--broken {
  border-color: var(--ki-status-danger-border);
  background-color: var(--ki-status-danger-subtle);
}

.ki-audit-badge__ok-icon {
  color: var(--ki-status-ok-text);
  flex-shrink: 0;
}

.ki-audit-badge__err-icon {
  color: var(--ki-status-danger-text);
  flex-shrink: 0;
}

.ki-audit-badge__meta {
  gap: var(--ki-space-1);
}

.ki-audit-badge__alg {
  background-color: var(--ki-bg-surface);
  color: var(--ki-text-primary);
  border: 1px solid var(--ki-border-default);
}

.ki-audit-badge__seq {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-secondary);
}

.ki-audit-badge__hash {
  font-size: var(--ki-fs-xs);
}
</style>
