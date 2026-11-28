<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { AlertTriangle, Eye, History, KeyRound, Plus, RefreshCw, Save } from 'lucide-vue-next'
import KsBanner from '@/components/ks/KsBanner.vue'
import { api, ApiError } from '@/api/client'
import { useScopeStore } from '@/stores/scope'

interface ConfigEntry {
  key: string
  value: string
  is_secret: boolean
  comment?: string
}

interface ConfigItem {
  cfg_id: string
  project_id: string
  name: string
  format: string
  description: string
  entries: ConfigEntry[]
  version: number
  updated_at: string
}

interface ConfigHistoryItem {
  version: number
  change_summary: string
  changed_by: string
  created_at: string
}

interface EnvItem {
  env_id: string
  code: string
  name: string
  description: string
  version: number
  members: Array<{ cfg_id: string; cfg_name: string; cfg_version: number; priority: number }>
}

interface EnvViewCheck {
  env_id: string
  version: number
  merged: Record<string, string>
  conflicts: Array<{ key: string; sources: string[]; effective_source: string }>
}

const scope = useScopeStore()
const activeTab = ref('configs')
const loading = ref(false)
const configs = ref<ConfigItem[]>([])
const selectedCfg = ref<ConfigItem | null>(null)
const editEntries = ref<ConfigEntry[]>([])
const changeSummary = ref('')
const history = ref<ConfigHistoryItem[]>([])
const revealedSecrets = ref<Record<string, string>>({})

const envs = ref<EnvItem[]>([])
const selectedEnv = ref<EnvItem | null>(null)
const viewCheck = ref<EnvViewCheck | null>(null)

const createDlg = ref(false)
const createForm = ref({ name: '', format: 'kv', description: '', initialKey: 'APP_MODE', initialVal: 'production', isSecret: false })

async function load() {
  loading.value = true
  try {
    const [cRes, eRes] = await Promise.all([
      api.get<{ items: ConfigItem[] }>('/api/v1/config/configurations'),
      api.get<{ items: EnvItem[] }>('/api/v1/config/environments'),
    ])
    configs.value = cRes.data.items || []
    envs.value = eRes.data.items || []
    if (configs.value.length > 0) {
      const target = configs.value.find((c) => c.cfg_id === selectedCfg.value?.cfg_id) || configs.value[0]
      await selectConfig(target)
    }
    if (envs.value.length > 0) {
      const targetEnv = envs.value.find((e) => e.env_id === selectedEnv.value?.env_id) || envs.value[0]
      await selectEnv(targetEnv)
    }
  } finally {
    loading.value = false
  }
}

async function selectConfig(cfg: ConfigItem) {
  selectedCfg.value = cfg
  editEntries.value = (cfg.entries || []).map((e) => ({ ...e }))
  revealedSecrets.value = {}
  const { data } = await api.get<{ items: ConfigHistoryItem[] }>(`/api/v1/config/configurations/${cfg.cfg_id}/history`)
  history.value = data.items || []
}

async function saveConfigVersion() {
  if (!selectedCfg.value) return
  try {
    const { data } = await api.put<ConfigItem>(
      `/api/v1/config/configurations/${selectedCfg.value.cfg_id}`,
      {
        description: selectedCfg.value.description,
        changeSummary: changeSummary.value || `发布版本 v${selectedCfg.value.version + 1}`,
        entries: editEntries.value,
      },
      { ifMatch: selectedCfg.value.version }
    )
    ElMessage.success(`配置集已通过 If-Match 原子更新至 v${data.version} 并归档不可变快照`)
    changeSummary.value = ''
    await load()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e))
  }
}

async function revealSecret(key: string) {
  if (!selectedCfg.value) return
  try {
    const { data } = await api.post<{ plaintext: string }>(
      `/api/v1/config/configurations/${selectedCfg.value.cfg_id}/reveal`,
      { key, reason: 'console-audit-inspection' }
    )
    revealedSecrets.value[key] = data.plaintext
    ElMessage.warning(`已解密密文键 ${key}（该次揭示已写入防篡改 HMAC 审计链）`)
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function createConfig() {
  try {
    await api.post('/api/v1/config/configurations', {
      projectId: scope.projectId || 'prod-clinical',
      name: createForm.value.name,
      format: createForm.value.format,
      description: createForm.value.description,
      entries: [
        {
          key: createForm.value.initialKey,
          value: createForm.value.initialVal,
          is_secret: createForm.value.isSecret,
        },
      ],
    })
    ElMessage.success('配置集已创建（含初始不可变快照 v1 与 Outbox 事件）')
    createDlg.value = false
    createForm.value = { name: '', format: 'kv', description: '', initialKey: 'APP_MODE', initialVal: 'production', isSecret: false }
    await load()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function selectEnv(env: EnvItem) {
  selectedEnv.value = env
  const { data } = await api.get<EnvViewCheck>(`/api/v1/config/environments/${env.env_id}/viewcheck`)
  viewCheck.value = data
}

function addEntryRow() {
  editEntries.value.push({ key: '', value: '', is_secret: false, comment: '' })
}

onMounted(load)
watch(() => [scope.tenantId, scope.appId], load)
</script>

<template>
  <div class="ks-page">
    <KsBanner
      title="配置中心与环境冲突预检 (S05–S07)"
      description="强版本 ETag / If-Match 乐观锁（缺失 428 / 过期 412）、v2 KMS AES-256/SM4 信封加密、强审计密文揭示、不可变快照历史与环境合并 ViewCheck。"
      :icon="KeyRound"
      :badge="`${configs.length} 配置集 · ${envs.length} 环境`"
      :tabs="[
        { key: 'configs', label: '配置集与 KMS 密文', count: configs.length },
        { key: 'environments', label: '环境合集与 ViewCheck', count: envs.length },
      ]"
      :active-tab="activeTab"
      @update:active-tab="activeTab = $event"
    >
      <template #actions>
        <el-button :loading="loading" @click="load"><RefreshCw :size="14" /><span class="ks-gap">刷新</span></el-button>
        <el-button type="primary" @click="createDlg = true"><Plus :size="14" /><span class="ks-gap">新建配置集</span></el-button>
      </template>
    </KsBanner>

    <!-- Tab 1: Configs -->
    <div v-if="activeTab === 'configs'" class="ks-split">
      <div class="ki-card">
        <div class="ki-card__title"><span>配置集列表</span></div>
        <div class="ks-cfg-list">
          <button
            v-for="c in configs"
            :key="c.cfg_id"
            type="button"
            class="ks-cfg-item"
            :class="{ 'is-active': selectedCfg?.cfg_id === c.cfg_id }"
            @click="selectConfig(c)"
          >
            <div class="ks-cfg-item__top">
              <span class="ks-cfg-item__name">{{ c.name }}</span>
              <span class="ki-badge ks-pill-blue ki-mono">v{{ c.version }}</span>
            </div>
            <div class="ks-cfg-item__desc">{{ c.description }}</div>
          </button>
        </div>
      </div>

      <div v-if="selectedCfg" class="ks-editor-col">
        <div class="ki-card">
          <div class="ki-card__title">
            <span>{{ selectedCfg.name }}</span>
            <div class="ks-head-meta">
              <span class="ki-badge ks-pill-ok ki-mono">If-Match: "{{ selectedCfg.version }}"</span>
              <el-button size="small" @click="addEntryRow">添加键值</el-button>
            </div>
          </div>

          <el-table :data="editEntries" size="small" stripe>
            <el-table-column label="Key" width="180">
              <template #default="{ row }"><el-input v-model="row.key" size="small" class="ki-mono" /></template>
            </el-table-column>
            <el-table-column label="Value" min-width="200">
              <template #default="{ row }">
                <div class="ks-val-cell">
                  <el-input v-model="row.value" size="small" :placeholder="row.is_secret ? '****** (留空保留原密文)' : ''" />
                  <el-button v-if="row.is_secret" size="small" @click="revealSecret(row.key)" title="审计揭示明文">
                    <Eye :size="12" />
                  </el-button>
                </div>
                <div v-if="revealedSecrets[row.key]" class="ks-revealed ki-mono">明文: {{ revealedSecrets[row.key] }}</div>
              </template>
            </el-table-column>
            <el-table-column label="KMS 密文" width="90">
              <template #default="{ row }"><el-switch v-model="row.is_secret" size="small" /></template>
            </el-table-column>
            <el-table-column label="说明" width="170">
              <template #default="{ row }"><el-input v-model="row.comment" size="small" /></template>
            </el-table-column>
          </el-table>

          <div class="ks-save-bar">
            <el-input v-model="changeSummary" size="small" placeholder="变更说明（将归档至不可变 cfg_history 快照）" style="max-width: 360px" />
            <el-button type="primary" size="small" @click="saveConfigVersion">
              <Save :size="13" /><span class="ks-gap">发布新版本 (CAS)</span>
            </el-button>
          </div>
        </div>

        <div class="ki-card">
          <div class="ki-card__title">
            <span><History :size="14" class="ks-inline-icon" />不可变历史快照 ({{ history.length }})</span>
          </div>
          <el-table :data="history" size="small">
            <el-table-column prop="version" label="版本" width="80"><template #default="{ row }"><span class="ki-mono">v{{ row.version }}</span></template></el-table-column>
            <el-table-column prop="change_summary" label="变更摘要" min-width="200" />
            <el-table-column prop="changed_by" label="变更人" width="120" />
          </el-table>
        </div>
      </div>
    </div>

    <!-- Tab 2: Environments & ViewCheck -->
    <div v-else class="ks-split">
      <div class="ki-card">
        <div class="ki-card__title"><span>环境列表</span></div>
        <div class="ks-cfg-list">
          <button
            v-for="e in envs"
            :key="e.env_id"
            type="button"
            class="ks-cfg-item"
            :class="{ 'is-active': selectedEnv?.env_id === e.env_id }"
            @click="selectEnv(e)"
          >
            <div class="ks-cfg-item__top">
              <span class="ks-cfg-item__name">{{ e.name }}</span>
              <span class="ki-badge ks-pill-ok ki-mono">{{ e.code }}</span>
            </div>
            <div class="ks-cfg-item__desc">{{ e.members.length }} 个有序成员配置 · v{{ e.version }}</div>
          </button>
        </div>
      </div>

      <div v-if="viewCheck" class="ki-card">
        <div class="ki-card__title">
          <span>环境合并预检 ViewCheck ({{ selectedEnv?.name }})</span>
          <span class="ki-badge" :class="viewCheck.conflicts?.length ? 'ks-pill-warn' : 'ks-pill-ok'">
            {{ viewCheck.conflicts?.length || 0 }} 项键覆写冲突
          </span>
        </div>

        <div v-if="viewCheck.conflicts?.length" class="ks-conflict-box">
          <div v-for="cf in viewCheck.conflicts" :key="cf.key" class="ks-conflict-row">
            <AlertTriangle :size="14" />
            <span class="ki-mono">{{ cf.key }}</span>
            <span>来源: {{ cf.sources.join(' → ') }}（高优先级生效: <strong>{{ cf.effective_source }}</strong>）</span>
          </div>
        </div>

        <el-table :data="Object.entries(viewCheck.merged).map(([k, v]) => ({ key: k, value: v }))" size="small" stripe>
          <el-table-column prop="key" label="生效键名" width="220"><template #default="{ row }"><span class="ki-mono">{{ row.key }}</span></template></el-table-column>
          <el-table-column prop="value" label="合并后生效值（密文自动掩码）" min-width="240"><template #default="{ row }"><span class="ki-mono">{{ row.value }}</span></template></el-table-column>
        </el-table>
      </div>
    </div>

    <el-dialog v-model="createDlg" title="新建 KMS 配置集" width="460px">
      <el-form label-position="top">
        <el-form-item label="配置名称" required><el-input v-model="createForm.name" placeholder="例如: his-redis-cluster" /></el-form-item>
        <el-form-item label="描述"><el-input v-model="createForm.description" /></el-form-item>
        <el-form-item label="初始键名" required><el-input v-model="createForm.initialKey" /></el-form-item>
        <el-form-item label="初始键值" required><el-input v-model="createForm.initialVal" /></el-form-item>
        <el-form-item label="启用 KMS 信封加密 (v2 AES-256-GCM)"><el-switch v-model="createForm.isSecret" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDlg = false">取消</el-button>
        <el-button type="primary" @click="createConfig">创建配置</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.ks-gap {
  margin-left: 6px;
}

.ks-inline-icon {
  vertical-align: -2px;
  margin-right: 6px;
}

.ks-split {
  display: grid;
  grid-template-columns: 300px minmax(0, 1fr);
  gap: var(--ki-space-4);
}

@media (max-width: 1024px) {
  .ks-split {
    grid-template-columns: 1fr;
  }
}

.ks-editor-col {
  display: flex;
  flex-direction: column;
  gap: var(--ki-space-4);
  min-width: 0;
}

.ks-cfg-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ks-cfg-item {
  padding: 12px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-subtle);
  text-align: left;
  cursor: pointer;
}

.ks-cfg-item.is-active {
  border-color: var(--ki-brand);
  background-color: var(--ki-sider-active-bg);
}

.ks-cfg-item__top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.ks-cfg-item__name {
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.ks-cfg-item__desc {
  margin-top: 4px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.ks-head-meta {
  display: flex;
  align-items: center;
  gap: 8px;
}

.ks-val-cell {
  display: flex;
  align-items: center;
  gap: 6px;
}

.ks-revealed {
  margin-top: 4px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-status-warn-text);
}

.ks-save-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid var(--ki-border-subtle);
}

.ks-conflict-box {
  margin-bottom: 12px;
  padding: 10px 12px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-status-warn-subtle);
  border: 1px solid var(--ki-status-warn-border);
  color: var(--ki-status-warn-text);
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: var(--ki-fs-xs);
}

.ks-conflict-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.ks-pill-ok {
  background-color: var(--ki-status-ok-subtle);
  border: 1px solid var(--ki-status-ok-border);
  color: var(--ki-status-ok-text);
}

.ks-pill-blue {
  background-color: var(--ki-status-running-subtle);
  border: 1px solid var(--ki-status-running-border);
  color: var(--ki-status-running-text);
}

.ks-pill-warn {
  background-color: var(--ki-status-warn-subtle);
  border: 1px solid var(--ki-status-warn-border);
  color: var(--ki-status-warn-text);
}
</style>
