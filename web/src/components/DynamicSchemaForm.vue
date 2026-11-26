<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { AlertTriangle } from 'lucide-vue-next'

export interface SchemaProperty {
  type: string
  default?: unknown
  enum?: string[]
  minimum?: number
  maximum?: number
  'x-kinstall-ui'?: {
    group?: string
    order?: number
    label?: string
    widget?: string
    visibleWhen?: string
    riskWarning?: string
  }
}

export interface PluginJSONSchema {
  title?: string
  properties: Record<string, SchemaProperty>
}

const props = defineProps<{
  schema: PluginJSONSchema
  modelValue: Record<string, unknown>
}>()

const emit = defineEmits<{ (e: 'update:modelValue', v: Record<string, unknown>): void }>()

const form = reactive<Record<string, unknown>>({})

watch(
  () => props.schema,
  (s) => {
    for (const [k, def] of Object.entries(s.properties || {})) {
      if (props.modelValue[k] !== undefined) form[k] = props.modelValue[k]
      else if (def.default !== undefined) form[k] = def.default
      else form[k] = def.type === 'boolean' ? false : def.type === 'integer' ? 0 : ''
    }
    emit('update:modelValue', { ...form })
  },
  { immediate: true }
)

function onFieldChange(key: string, val: unknown) {
  form[key] = val
  emit('update:modelValue', { ...form })
}

function isVisible(prop: SchemaProperty): boolean {
  const expr = prop['x-kinstall-ui']?.visibleWhen
  if (!expr) return true
  const m = expr.match(/^(\w+)\s*==\s*['"]([^'"]+)['"]$/)
  if (m) return String(form[m[1]] ?? '') === m[2]
  return true
}

const groupedFields = computed(() => {
  const groups: Record<string, Array<{ key: string; prop: SchemaProperty }>> = {}
  const entries = Object.entries(props.schema.properties || {}).sort(
    (a, b) => (a[1]['x-kinstall-ui']?.order ?? 99) - (b[1]['x-kinstall-ui']?.order ?? 99)
  )
  for (const [key, prop] of entries) {
    if (!isVisible(prop)) continue
    const g = prop['x-kinstall-ui']?.group || 'basic'
    if (!groups[g]) groups[g] = []
    groups[g].push({ key, prop })
  }
  return groups
})
</script>

<template>
  <div class="ki-schema-form">
    <div v-for="(fields, groupName) in groupedFields" :key="groupName" class="ki-panel" style="margin-bottom: 12px">
      <div style="font-weight: 600; text-transform: uppercase; font-size: 11px; color: var(--ki-text-muted); margin-bottom: 10px">
        {{ groupName }}
      </div>
      <el-form label-position="top">
        <div class="ki-grid-2">
          <div v-for="item in fields" :key="item.key">
            <el-form-item :label="item.prop['x-kinstall-ui']?.label || item.key">
              <div
                v-if="item.prop['x-kinstall-ui']?.riskWarning"
                style="display:flex;align-items:center;gap:6px;width:100%;padding:6px 10px;margin-bottom:6px;border-radius:4px;background:var(--ki-status-warn-subtle);color:var(--ki-status-warn-text);font-size:12px"
              >
                <AlertTriangle :size="14" />
                <span>{{ item.prop['x-kinstall-ui']?.riskWarning }}</span>
              </div>

              <el-select
                v-if="item.prop.enum"
                :model-value="String(form[item.key] ?? '')"
                style="width: 100%"
                @update:model-value="(v: string) => onFieldChange(item.key, v)"
              >
                <el-option v-for="opt in item.prop.enum" :key="opt" :label="opt" :value="opt" />
              </el-select>

              <el-switch
                v-else-if="item.prop.type === 'boolean'"
                :model-value="Boolean(form[item.key])"
                @update:model-value="(v: boolean) => onFieldChange(item.key, v)"
              />

              <el-input-number
                v-else-if="item.prop.type === 'integer'"
                :model-value="Number(form[item.key] ?? 0)"
                :min="item.prop.minimum ?? 0"
                :max="item.prop.maximum ?? 65535"
                style="width: 100%"
                @update:model-value="(v: number | undefined) => onFieldChange(item.key, v ?? 0)"
              />

              <el-input
                v-else
                :model-value="String(form[item.key] ?? '')"
                :placeholder="item.prop['x-kinstall-ui']?.widget === 'credential-select' ? 'cred_01J...' : item.key"
                class="ki-mono"
                @update:model-value="(v: string) => onFieldChange(item.key, v)"
              />
            </el-form-item>
          </div>
        </div>
      </el-form>
    </div>
  </div>
</template>
