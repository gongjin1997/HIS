<script setup lang="ts">
import { computed, useId } from 'vue'

const props = withDefaults(
  defineProps<{
    modelValue: string
    label?: string
    placeholder?: string
    hint?: string
    error?: string
    required?: boolean
    disabled?: boolean
    readonly?: boolean
    mono?: boolean
    type?: string
    autocomplete?: string
  }>(),
  {
    label: '',
    placeholder: '',
    hint: '',
    error: '',
    required: false,
    disabled: false,
    readonly: false,
    mono: false,
    type: 'text',
    autocomplete: 'off',
  }
)

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
}>()

const inputId = useId()
const errId = computed(() => `${inputId}-err`)

function onInput(ev: Event) {
  const target = ev.target as HTMLInputElement
  emit('update:modelValue', target.value)
}
</script>

<template>
  <div class="ki-field">
    <label v-if="label" :for="inputId" class="ki-field__label">
      <span>{{ label }}</span>
      <span v-if="required" class="ki-field__req" aria-hidden="true">*</span>
    </label>
    <input
      :id="inputId"
      :type="type"
      :value="modelValue"
      :placeholder="placeholder"
      :disabled="disabled"
      :readonly="readonly"
      :autocomplete="autocomplete"
      :aria-invalid="error ? 'true' : undefined"
      :aria-describedby="error ? errId : undefined"
      class="ki-input"
      :class="{ 'ki-mono': mono, 'ki-input--error': Boolean(error) }"
      @input="onInput"
    />
    <p v-if="error" :id="errId" class="ki-field__error" role="alert">{{ error }}</p>
    <p v-else-if="hint" class="ki-field__hint">{{ hint }}</p>
  </div>
</template>

<style scoped>
.ki-field {
  display: flex;
  flex-direction: column;
  gap: var(--ki-space-1);
}

.ki-field__label {
  display: inline-flex;
  align-items: center;
  gap: var(--ki-space-1);
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-medium);
  color: var(--ki-text-secondary);
}

.ki-field__req {
  color: var(--ki-status-danger-text);
}

.ki-input {
  width: 100%;
  height: var(--ki-control-h);
  padding: 0 var(--ki-input-px);
  background-color: var(--ki-bg-surface);
  color: var(--ki-text-primary);
  border: 1px solid var(--ki-border-control);
  border-radius: var(--ki-radius-sm);
  font-family: var(--ki-font-sans);
  font-size: var(--ki-fs-body);
  transition: border-color var(--ki-dur-fast) var(--ki-ease);
}

.ki-input:focus {
  border-color: var(--ki-border-focus);
}

.ki-input:disabled {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-disabled);
  cursor: not-allowed;
}

.ki-input:hover:not(:disabled) {
  border-color: var(--ki-border-strong);
}

.ki-input--error {
  border-color: var(--ki-status-danger);
}

.ki-field__error {
  margin: 0;
  font-size: var(--ki-fs-xs);
  color: var(--ki-status-danger-text);
}

.ki-field__hint {
  margin: 0;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}
</style>
