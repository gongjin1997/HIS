<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { ShieldAlert } from 'lucide-vue-next'
import KiModal from '@/components/ki/KiModal.vue'
import KiEnvBadge from '@/components/ki/KiEnvBadge.vue'
import KiButton from '@/components/ki/KiButton.vue'
import KiInput from '@/components/ki/KiInput.vue'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    env: string
    title: string
    resourceName: string
    riskSummary: string
    confirmLabel?: string
    loading?: boolean
  }>(),
  {
    confirmLabel: '确认高危操作',
    loading: false,
  }
)

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'confirm'): void
}>()

const typedName = ref('')
const cancelWrap = ref<HTMLElement | null>(null)

const matched = computed(() => typedName.value.trim() === props.resourceName)

watch(
  () => props.modelValue,
  async (open) => {
    if (open) {
      typedName.value = ''
      await nextTick()
      // DESIGN.md §8.1: default focus MUST land on Cancel so accidental Enter never triggers danger action!
      const btn = cancelWrap.value?.querySelector('button')
      btn?.focus()
    }
  }
)

function close() {
  emit('update:modelValue', false)
}

function submit() {
  if (!matched.value || props.loading) return
  emit('confirm')
}
</script>

<template>
  <KiModal
    :model-value="modelValue"
    :title="title"
    size="sm"
    :close-on-backdrop="false"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <template #title-prefix>
      <KiEnvBadge :env="env" />
    </template>

    <div class="ki-flex-col">
      <div class="ki-danger-summary">
        <ShieldAlert :size="16" class="ki-danger-summary__icon" aria-hidden="true" />
        <p class="ki-danger-summary__text">{{ riskSummary }}</p>
      </div>

      <div class="ki-danger-prompt">
        请输入目标资源名称 <strong class="ki-mono">{{ resourceName }}</strong> 以解锁确认按钮：
      </div>

      <KiInput
        v-model="typedName"
        mono
        :placeholder="resourceName"
        aria-label="输入目标资源名称以确认"
      />
    </div>

    <template #footer>
      <span ref="cancelWrap">
        <KiButton variant="secondary" @click="close">取消</KiButton>
      </span>
      <KiButton variant="danger" :disabled="!matched" :loading="loading" @click="submit">
        {{ confirmLabel }}
      </KiButton>
    </template>
  </KiModal>
</template>

<style scoped>
.ki-danger-summary {
  display: flex;
  align-items: flex-start;
  gap: var(--ki-space-2);
  padding: var(--ki-space-3);
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-status-danger-subtle);
  border: 1px solid var(--ki-status-danger-border);
  color: var(--ki-status-danger-text);
}

.ki-danger-summary__icon {
  flex-shrink: 0;
  margin-top: 2px;
}

.ki-danger-summary__text {
  margin: 0;
  font-size: var(--ki-fs-sm);
}

.ki-danger-prompt {
  font-size: var(--ki-fs-sm);
  color: var(--ki-text-secondary);
}
</style>
