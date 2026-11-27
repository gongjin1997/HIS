<script setup lang="ts">
import KiDynamicSchemaForm, {
  type PluginJSONSchema,
  type HostOption,
} from './domain/KiDynamicSchemaForm.vue'

export type { PluginJSONSchema, SchemaProperty } from './domain/KiDynamicSchemaForm.vue'

defineProps<{
  schema: PluginJSONSchema
  modelValue: Record<string, unknown>
  hosts?: HostOption[]
  credentials?: Array<{ id: string; name: string }>
}>()

defineEmits<{ (e: 'update:modelValue', v: Record<string, unknown>): void }>()
</script>

<template>
  <KiDynamicSchemaForm
    :schema="schema"
    :model-value="modelValue"
    :hosts="hosts"
    :credentials="credentials"
    @update:model-value="$emit('update:modelValue', $event)"
  />
</template>
