<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { AlertTriangle, ChevronDown, ChevronRight, Server, Box, Layers, KeyRound, Network } from 'lucide-vue-next'
import KiDomainTag from '@/components/ki/KiDomainTag.vue'

export interface HostOption {
  id: string
  name: string
  address: string
  arch?: string
  pageSize?: number
  faultZone?: string
}

export interface SchemaProperty {
  type: string
  default?: unknown
  enum?: string[]
  minimum?: number
  maximum?: number
  description?: string
  'x-kinstall-ui'?: {
    group?: string
    order?: number
    label?: string
    widget?:
      | 'host-role-picker'
      | 'credential-select'
      | 'deploy-target-cards'
      | 'cidr-allocator'
      | string
    visibleWhen?: string
    riskWarning?: string
  }
}

export interface PluginJSONSchema {
  title?: string
  properties: Record<string, SchemaProperty>
}

const props = withDefaults(
  defineProps<{
    schema: PluginJSONSchema
    modelValue: Record<string, unknown>
    hosts?: HostOption[]
    credentials?: Array<{ id: string; name: string }>
  }>(),
  {
    hosts: () => [],
    credentials: () => [],
  }
)

const emit = defineEmits<{ (e: 'update:modelValue', v: Record<string, unknown>): void }>()

const form = reactive<Record<string, unknown>>({})
const advancedOpen = ref(false)

watch(
  () => props.schema,
  (s) => {
    for (const [k, def] of Object.entries(s?.properties || {})) {
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
  const eq = expr.match(/^(\w+)\s*==\s*['"]([^'"]+)['"]$/)
  if (eq) return String(form[eq[1]] ?? '') === eq[2]
  const neq = expr.match(/^(\w+)\s*!=\s*['"]([^'"]+)['"]$/)
  if (neq) return String(form[neq[1]] ?? '') !== neq[2]
  const boolEq = expr.match(/^(\w+)\s*==\s*(true|false)$/)
  if (boolEq) return Boolean(form[boolEq[1]]) === (boolEq[2] === 'true')
  return true
}

const groupedFields = computed(() => {
  const groups: Record<string, Array<{ key: string; prop: SchemaProperty }>> = {}
  const entries = Object.entries(props.schema?.properties || {}).sort(
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

const hostsByZone = computed(() => {
  const map: Record<string, HostOption[]> = {}
  for (const h of props.hosts) {
    const z = h.faultZone || 'default-az'
    if (!map[z]) map[z] = []
    map[z].push(h)
  }
  return map
})

const targetCards = [
  { id: 'host-binary', title: '主机二进制 (Systemd)', desc: '原生 systemd 单元托管，零容器依赖，支持国产 OS', icon: Server },
  { id: 'host-container', title: '单机容器 (Podman/Docker)', desc: 'rootless OCI 镜像交付，离线 tar 包一键加载', icon: Box },
  { id: 'kubernetes', title: 'Kubernetes 编排', desc: 'StatefulSet / Helm 声明式部署至目标集群', icon: Layers },
]

const groupTitles: Record<string, string> = {
  basic: '基础配置',
  network: '网络与拓扑',
  storage: '存储与高可用',
  security: '安全与凭据',
  advanced: '高级参数（默认折叠）',
}

function suggestCidr(key: string) {
  const presets = ['10.244.0.0/16', '10.96.0.0/12', '172.20.0.0/16']
  const cur = String(form[key] ?? '')
  const next = presets[(presets.indexOf(cur) + 1) % presets.length]
  onFieldChange(key, next)
}
</script>

<template>
  <div class="ki-schema-form">
    <section
      v-for="(fields, groupName) in groupedFields"
      :key="groupName"
      class="ki-card ki-schema-group"
    >
      <div
        class="ki-schema-group__head"
        :class="{ 'is-clickable': groupName === 'advanced' }"
        @click="groupName === 'advanced' && (advancedOpen = !advancedOpen)"
      >
        <div class="section-title ki-schema-group__title">
          {{ groupTitles[groupName] || groupName }}
          <span class="sub">{{ fields.length }} 项</span>
        </div>
        <button
          v-if="groupName === 'advanced'"
          type="button"
          class="ki-schema-toggle"
          :aria-expanded="advancedOpen"
        >
          <ChevronDown v-if="advancedOpen" :size="15" />
          <ChevronRight v-else :size="15" />
          {{ advancedOpen ? '收起' : '展开' }}
        </button>
      </div>

      <el-form v-show="groupName !== 'advanced' || advancedOpen" label-position="top">
        <div class="ki-grid-2">
          <div
            v-for="item in fields"
            :key="item.key"
            :class="{ 'ki-span-2': item.prop['x-kinstall-ui']?.widget === 'deploy-target-cards' || item.prop['x-kinstall-ui']?.widget === 'host-role-picker' }"
          >
            <el-form-item :label="item.prop['x-kinstall-ui']?.label || item.key">
              <div v-if="item.prop['x-kinstall-ui']?.riskWarning" class="ki-risk-banner">
                <AlertTriangle :size="14" aria-hidden="true" />
                <span>{{ item.prop['x-kinstall-ui']?.riskWarning }}</span>
              </div>

              <!-- Widget 1: deploy-target-cards -->
              <div
                v-if="item.prop['x-kinstall-ui']?.widget === 'deploy-target-cards'"
                class="ki-target-grid"
              >
                <button
                  v-for="card in targetCards"
                  :key="card.id"
                  type="button"
                  class="ki-target-card"
                  :class="{ 'is-selected': String(form[item.key] ?? '') === card.id }"
                  @click="onFieldChange(item.key, card.id)"
                >
                  <component :is="card.icon" :size="18" class="ki-target-card__icon" />
                  <b>{{ card.title }}</b>
                  <span>{{ card.desc }}</span>
                </button>
              </div>

              <!-- Widget 2: host-role-picker (grouped by faultZone with arm64-64K warning) -->
              <div
                v-else-if="item.prop['x-kinstall-ui']?.widget === 'host-role-picker'"
                class="ki-host-picker"
              >
                <div v-if="hosts.length === 0" class="ki-muted ki-host-picker__empty">
                  暂无可用 CMDB 主机，请直接输入主机 ID（逗号分隔）
                  <el-input
                    :model-value="String(form[item.key] ?? '')"
                    class="ki-mono ki-mt-3"
                    placeholder="host_01J..."
                    @update:model-value="(v: string) => onFieldChange(item.key, v)"
                  />
                </div>
                <div v-else class="ki-zone-list">
                  <div v-for="(zoneHosts, zone) in hostsByZone" :key="zone" class="ki-zone-group">
                    <div class="ki-zone-label ki-mono">AZ: {{ zone }}</div>
                    <div class="ki-zone-hosts">
                      <button
                        v-for="h in zoneHosts"
                        :key="h.id"
                        type="button"
                        class="ki-host-chip"
                        :class="{ 'is-selected': String(form[item.key] ?? '').includes(h.id) }"
                        @click="onFieldChange(item.key, h.id)"
                      >
                        <strong>{{ h.name }}</strong>
                        <span class="ki-mono ki-muted">{{ h.address }}</span>
                        <KiDomainTag kind="arch" :value="h.arch || 'amd64'" :page-size="h.pageSize" />
                      </button>
                    </div>
                  </div>
                </div>
              </div>

              <!-- Widget 3: credential-select -->
              <div
                v-else-if="item.prop['x-kinstall-ui']?.widget === 'credential-select'"
                class="ki-flex-row ki-w-full"
              >
                <el-select
                  v-if="credentials.length"
                  :model-value="String(form[item.key] ?? '')"
                  filterable
                  allow-create
                  class="ki-w-full"
                  placeholder="选择或输入凭据引用 (cred_01J...)"
                  @update:model-value="(v: string) => onFieldChange(item.key, v)"
                >
                  <el-option v-for="c in credentials" :key="c.id" :label="`${c.name} (${c.id})`" :value="c.id" />
                </el-select>
                <el-input
                  v-else
                  :model-value="String(form[item.key] ?? '')"
                  placeholder="cred_01J... (信封加密凭据引用)"
                  class="ki-mono"
                  @update:model-value="(v: string) => onFieldChange(item.key, v)"
                >
                  <template #prefix><KeyRound :size="13" /></template>
                </el-input>
              </div>

              <!-- Widget 4: cidr-allocator -->
              <div
                v-else-if="item.prop['x-kinstall-ui']?.widget === 'cidr-allocator'"
                class="ki-flex-row ki-w-full"
              >
                <el-input
                  :model-value="String(form[item.key] ?? '')"
                  placeholder="10.244.0.0/16"
                  class="ki-mono ki-flex-1"
                  @update:model-value="(v: string) => onFieldChange(item.key, v)"
                >
                  <template #prefix><Network :size="13" /></template>
                </el-input>
                <el-button size="small" @click="suggestCidr(item.key)">推荐网段</el-button>
              </div>

              <!-- Standard JSON Schema controls -->
              <el-select
                v-else-if="item.prop.enum"
                :model-value="String(form[item.key] ?? '')"
                class="ki-w-full"
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
                class="ki-w-full"
                @update:model-value="(v: number | undefined) => onFieldChange(item.key, v ?? 0)"
              />

              <el-input
                v-else
                :model-value="String(form[item.key] ?? '')"
                :placeholder="item.prop.description || item.key"
                class="ki-mono"
                @update:model-value="(v: string) => onFieldChange(item.key, v)"
              />
            </el-form-item>
          </div>
        </div>
      </el-form>
    </section>
  </div>
</template>

<style scoped>
.ki-schema-form {
  display: flex;
  flex-direction: column;
  gap: var(--ki-space-3);
}

.ki-schema-group__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: var(--ki-space-3);
}

.ki-schema-group__head.is-clickable {
  cursor: pointer;
}

.ki-schema-group__title {
  margin: 0;
}

.ki-schema-toggle {
  display: inline-flex;
  align-items: center;
  gap: var(--ki-space-1);
  border: 0;
  background-color: transparent;
  color: var(--ki-brand);
  font-size: var(--ki-fs-sm);
  cursor: pointer;
}

.ki-span-2 {
  grid-column: 1 / -1;
}

.ki-risk-banner {
  display: flex;
  align-items: center;
  gap: var(--ki-space-2);
  width: 100%;
  padding: 6px 10px;
  margin-bottom: 6px;
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-status-warn-subtle);
  border: 1px solid var(--ki-status-warn-border);
  color: var(--ki-status-warn-text);
  font-size: var(--ki-fs-xs);
}

.ki-target-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--ki-space-3);
  width: 100%;
}

.ki-target-card {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: var(--ki-space-1);
  padding: var(--ki-space-3);
  background-color: var(--ki-bg-surface);
  border: 1px solid var(--ki-border-default);
  border-radius: var(--ki-radius-md);
  text-align: left;
  cursor: pointer;
  transition: border-color var(--ki-dur-fast) var(--ki-ease), background-color var(--ki-dur-fast) var(--ki-ease);
}

.ki-target-card:hover {
  border-color: var(--ki-border-strong);
}

.ki-target-card.is-selected {
  border: 2px solid var(--ki-brand);
  background-color: var(--ki-brand-subtle);
}

.ki-target-card__icon {
  color: var(--ki-brand);
}

.ki-target-card b {
  font-size: var(--ki-fs-body);
  color: var(--ki-text-primary);
}

.ki-target-card span {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.ki-host-picker {
  width: 100%;
}

.ki-host-picker__empty {
  font-size: var(--ki-fs-xs);
}

.ki-zone-list {
  display: flex;
  flex-direction: column;
  gap: var(--ki-space-2);
}

.ki-zone-group {
  padding: var(--ki-space-2) var(--ki-space-3);
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-bg-subtle);
  border: 1px solid var(--ki-border-subtle);
}

.ki-zone-label {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
  margin-bottom: var(--ki-space-2);
}

.ki-zone-hosts {
  display: flex;
  flex-wrap: wrap;
  gap: var(--ki-space-2);
}

.ki-host-chip {
  display: inline-flex;
  align-items: center;
  gap: var(--ki-space-2);
  padding: 6px 10px;
  border-radius: var(--ki-radius-sm);
  border: 1px solid var(--ki-border-default);
  background-color: var(--ki-bg-surface);
  color: var(--ki-text-primary);
  cursor: pointer;
}

.ki-host-chip.is-selected {
  border-color: var(--ki-brand);
  background-color: var(--ki-brand-subtle);
}
</style>
