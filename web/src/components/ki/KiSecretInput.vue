<script setup lang="ts">
import { ref, computed, useId } from 'vue'
import { Eye, EyeOff, KeyRound } from 'lucide-vue-next'

const props = withDefaults(
  defineProps<{
    modelValue: string
    label?: string
    credentialRef?: string
    placeholder?: string
    error?: string
    required?: boolean
    disabled?: boolean
  }>(),
  {
    label: '',
    credentialRef: '',
    placeholder: '••••••••',
    error: '',
    required: false,
    disabled: false,
  }
)

const emit = defineEmits<{ (e: 'update:modelValue', value: string): void }>()

const revealed = ref(false)
const inputId = useId()
const errId = computed(() => `${inputId}-err`)

const shortRef = computed(() => {
  if (!props.credentialRef) return ''
  return props.credentialRef.length > 14
    ? `${props.credentialRef.slice(0, 10)}…${props.credentialRef.slice(-4)}`
    : props.credentialRef
})

function onInput(ev: Event) {
  emit('update:modelValue', (ev.target as HTMLInputElement).value)
}
</script>

<template>
  <div class="ki-secret-field">
    <div v-if="label || credentialRef" class="ki-secret-field__top">
      <label v-if="label" :for="inputId" class="ki-secret-field__label">
        <span>{{ label }}</span>
        <span v-if="required" class="ki-secret-field__req">*</span>
      </label>
      <span v-if="shortRef" class="ki-secret-ref ki-mono" :title="credentialRef">
        <KeyRound :size="11" aria-hidden="true" />
        {{ shortRef }}
      </span>
    </div>
    <div class="ki-secret-wrap" :class="{ 'ki-secret-wrap--error': Boolean(error) }">
      <input
        :id="inputId"
        :type="revealed ? 'text' : 'password'"
        :value="modelValue"
        :placeholder="placeholder"
        :disabled="disabled"
        autocomplete="new-password"
        :aria-invalid="error ? 'true' : undefined"
        :aria-describedby="error ? errId : undefined"
        class="ki-secret-input ki-mono"
        @input="onInput"
      />
      <button
        type="button"
        class="ki-secret-toggle"
        :aria-label="revealed ? '隐藏密文' : '临时明文查看'"
        :disabled="disabled"
        @click="revealed = !revealed"
      >
        <EyeOff v-if="revealed" :size="14" />
        <Eye v-else :size="14" />
      </button>
    </div>
    <p v-if="error" :id="errId" class="ki-secret-field__error" role="alert">{{ error }}</p>
  </div>
</template>

<style scoped>
.ki-secret-field {
  display: flex;
  flex-direction: column;
  gap: var(--ki-space-1);
}

.ki-secret-field__top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--ki-space-2);
}

.ki-secret-field__label {
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-medium);
  color: var(--ki-text-secondary);
}

.ki-secret-field__req {
  margin-left: var(--ki-space-half);
  color: var(--ki-status-danger-text);
}

.ki-secret-ref {
  display: inline-flex;
  align-items: center;
  gap: var(--ki-space-1);
  padding: 0 var(--ki-space-2);
  height: var(--ki-badge-h);
  border-radius: var(--ki-radius-xs);
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-xs);
}

.ki-secret-wrap {
  display: flex;
  align-items: center;
  height: var(--ki-control-h);
  background-color: var(--ki-bg-surface);
  border: 1px solid var(--ki-border-control);
  border-radius: var(--ki-radius-sm);
  overflow: hidden;
}

.ki-secret-wrap:focus-within {
  border-color: var(--ki-border-focus);
}

.ki-secret-wrap--error {
  border-color: var(--ki-status-danger);
}

.ki-secret-input {
  flex: 1;
  min-width: 0;
  height: 100%;
  padding: 0 var(--ki-input-px);
  background-color: transparent;
  border: 0;
  color: var(--ki-text-primary);
}

.ki-secret-toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: var(--ki-control-h);
  height: 100%;
  background-color: transparent;
  border: 0;
  color: var(--ki-text-muted);
  cursor: pointer;
}

.ki-secret-toggle:hover:not(:disabled) {
  color: var(--ki-text-primary);
  background-color: var(--ki-bg-subtle);
}

.ki-secret-field__error {
  margin: 0;
  font-size: var(--ki-fs-xs);
  color: var(--ki-status-danger-text);
}
</style>
