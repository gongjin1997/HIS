<script setup lang="ts">
import { computed, ref } from 'vue'
import { AlertTriangle, HardDrive, Cloud, Globe, Cpu, Scale, Layers } from 'lucide-vue-next'
import KiModal from './KiModal.vue'

const props = withDefaults(
  defineProps<{
    kind: 'target' | 'arch' | 'media' | 'license'
    value: string
    pageSize?: number
    licenseNotice?: string
  }>(),
  {
    pageSize: 4096,
    licenseNotice: '',
  }
)

const showLicenseModal = ref(false)

const is64K = computed(() => props.kind === 'arch' && (props.pageSize || 4096) >= 65536)

const tagClass = computed(() => {
  const v = (props.value || '').toLowerCase()
  if (props.kind === 'target') {
    if (v === 'host-container') return 'ki-domain-tag--container'
    if (v === 'kubernetes') return 'ki-domain-tag--k8s'
    return 'ki-domain-tag--binary'
  }
  if (props.kind === 'arch') {
    return is64K.value ? 'ki-domain-tag--arm64k' : 'ki-domain-tag--arch'
  }
  if (props.kind === 'media') {
    if (v.includes('local')) return 'ki-domain-tag--ok'
    if (v.includes('obs') || v.includes('s3')) return 'ki-domain-tag--container'
    return 'ki-domain-tag--warn'
  }
  // license traffic light (green: Apache/MIT/BSD/PostgreSQL, yellow: GPL/MPL/LGPL, red: SSPL/BSL/AGPL/Commercial)
  if (/(apache|mit|bsd|postgresql|isc)/i.test(v)) return 'ki-domain-tag--ok'
  if (/(sspl|bsl|agpl|elastic|commercial|proprietary)/i.test(v)) return 'ki-domain-tag--danger'
  return 'ki-domain-tag--warn'
})

const displayText = computed(() => {
  if (props.kind === 'arch') {
    const a = props.value || 'amd64'
    return is64K.value ? `${a}-64K` : `${a}-4K`
  }
  return props.value || '—'
})

const licenseRiskTitle = computed(() => {
  if (tagClass.value === 'ki-domain-tag--ok') return '绿灯 — 宽松开源协议（可直接商业离线交付）'
  if (tagClass.value === 'ki-domain-tag--danger') return '红灯 — 限制性/源可用协议（需法务/商业授权确认）'
  return '黄灯 — 传染性/弱著佐权协议（需保留声明与合规归档）'
})
</script>

<template>
  <button
    v-if="kind === 'license'"
    type="button"
    class="ki-badge ki-domain-tag ki-mono ki-domain-tag--clickable"
    :class="tagClass"
    @click="showLicenseModal = true"
  >
    <Scale :size="11" aria-hidden="true" />
    <span>{{ displayText }}</span>
  </button>
  <span v-else class="ki-badge ki-domain-tag ki-mono" :class="tagClass">
    <AlertTriangle v-if="is64K" :size="11" aria-hidden="true" />
    <Cpu v-else-if="kind === 'arch'" :size="11" aria-hidden="true" />
    <HardDrive v-else-if="kind === 'media' && value.toLowerCase().includes('local')" :size="11" aria-hidden="true" />
    <Cloud v-else-if="kind === 'media' && (value.toLowerCase().includes('obs') || value.toLowerCase().includes('s3'))" :size="11" aria-hidden="true" />
    <Globe v-else-if="kind === 'media'" :size="11" aria-hidden="true" />
    <Layers v-else :size="11" aria-hidden="true" />
    <span>{{ displayText }}</span>
  </span>

  <KiModal v-if="kind === 'license'" v-model="showLicenseModal" title="开源许可证合规提示" size="sm">
    <div class="ki-flex-col">
      <div class="ki-flex-row">
        <span class="ki-muted">许可证标识：</span>
        <strong class="ki-mono">{{ displayText }}</strong>
      </div>
      <div class="ki-license-box">{{ licenseRiskTitle }}</div>
      <p class="ki-secondary ki-license-note">
        {{ licenseNotice || '请确认离线交付包中的 THIRD-PARTY-NOTICES 与法务审计记录一致。' }}
      </p>
    </div>
  </KiModal>
</template>

<style scoped>
.ki-domain-tag {
  border: 1px solid transparent;
}

.ki-domain-tag--clickable {
  cursor: pointer;
}

.ki-domain-tag--binary {
  background-color: var(--ki-badge-binary-bg);
  color: var(--ki-badge-binary-text);
}

.ki-domain-tag--container {
  background-color: var(--ki-badge-container-bg);
  color: var(--ki-badge-container-text);
}

.ki-domain-tag--k8s {
  background-color: var(--ki-badge-k8s-bg);
  color: var(--ki-badge-k8s-text);
}

.ki-domain-tag--arch {
  background-color: var(--ki-badge-arch-bg);
  color: var(--ki-badge-arch-text);
}

.ki-domain-tag--arm64k {
  background-color: var(--ki-badge-arm64k-bg);
  color: var(--ki-badge-arm64k-text);
  border-color: var(--ki-status-warn-border);
}

.ki-domain-tag--ok {
  background-color: var(--ki-status-ok-subtle);
  color: var(--ki-status-ok-text);
  border-color: var(--ki-status-ok-border);
}

.ki-domain-tag--warn {
  background-color: var(--ki-status-warn-subtle);
  color: var(--ki-status-warn-text);
  border-color: var(--ki-status-warn-border);
}

.ki-domain-tag--danger {
  background-color: var(--ki-status-danger-subtle);
  color: var(--ki-status-danger-text);
  border-color: var(--ki-status-danger-border);
}

.ki-license-box {
  padding: var(--ki-space-2) var(--ki-space-3);
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-bg-subtle);
  border: 1px solid var(--ki-border-subtle);
  font-weight: var(--ki-fw-medium);
}

.ki-license-note {
  margin: 0;
  font-size: var(--ki-fs-sm);
}
</style>
