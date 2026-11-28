<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  Copy,
  Eye,
  History,
  KeyRound,
  Lock,
  Plus,
  RefreshCw,
  RotateCcw,
  Save,
  Trash2,
} from 'lucide-vue-next'
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

interface ConfigSnapshot {
  version: number
  entries: ConfigEntry[]
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
const subView = ref<'editor' | 'yaml' | 'history'>('editor')
const loading = ref(false)
const searchCfg = ref('')

const configs = ref<ConfigItem[]>([])
const selectedCfg = ref<ConfigItem | null>(null)
const editEntries = ref<ConfigEntry[]>([])
const changeSummary = ref('')
const history = ref<ConfigSnapshot[]>([])
const compareVer = ref<number | null>(null)
const revealedSecrets = ref<Record<string, string>>({})

// Audited reveal confirmation modal
const revealDlg = ref(false)
const revealTargetKey = ref('')
const revealReason = ref('上线配置核对')

// Environment tab state
const envs = ref<EnvItem[]>([])
const selectedEnv = ref<EnvItem | null>(null)
const envMembersDraft = ref<Array<{ cfg_id: string; cfg_name: string; cfg_version: number; priority: number }>>([])
const viewCheck = ref<EnvViewCheck | null>(null)

// Create Config Modal
const createDlg = ref(false)
const createForm = ref({ name: '', format: 'kv', description: '', initialKey: 'APP_ENV', initialVal: 'production', isSecret: false })

const filteredConfigs = computed(() => {
  const q = searchCfg.value.trim().toLowerCase()
  if (!q) return configs.value
  return configs.value.filter((c) => c.name.toLowerCase().includes(q) || c.description.toLowerCase().includes(q))
})

const yamlPreview = computed(() => {
  return editEntries.value
    .filter((e) => e.key.trim())
    .map((e) => {
      const comment = e.comment ? `  # ${e.comment}` : ''
      const val = e.is_secret ? '"******"  # [加密存储]' : JSON.stringify(e.value)
      return `${e.key}: ${val}${comment}`
    })
    .join('\n')
})

const diffRows = computed(() => {
  if (!selectedCfg.value || compareVer.value === null) return []
  const snap = history.value.find((h) => h.version === compareVer.value)
  if (!snap) return []
  const oldMap = new Map((snap.entries || []).map((e) => [e.key, e]))
  const curMap = new Map((selectedCfg.value.entries || []).map((e) => [e.key, e]))
  const keys = Array.from(new Set([...oldMap.keys(), ...curMap.keys()])).sort()
  return keys.map((k) => {
    const o = oldMap.get(k)
    const c = curMap.get(k)
    let change: 'ADDED' | 'REMOVED' | 'MODIFIED' | 'SAME' = 'SAME'
    if (!o && c) change = 'ADDED'
    else if (o && !c) change = 'REMOVED'
    else if (o && c && (o.value !== c.value || o.is_secret !== c.is_secret)) change = 'MODIFIED'
    return { key: k, oldVal: o ? (o.is_secret ? '******' : o.value) : '-', newVal: c ? (c.is_secret ? '******' : c.value) : '-', change }
  })
})

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
  const { data } = await api.get<{ items: ConfigSnapshot[] }>(`/api/v1/config/configurations/${cfg.cfg_id}/history`)
  history.value = data.items || []
  compareVer.value = history.value.length > 1 ? history.value[1].version : history.value[0]?.version ?? null
}

async function saveConfigVersion() {
  if (!selectedCfg.value) return
  try {
    const { data } = await api.put<ConfigItem>(
      `/api/v1/config/configurations/${selectedCfg.value.cfg_id}`,
      {
        description: selectedCfg.value.description,
        changeSummary: changeSummary.value || `发布配置版本 v${selectedCfg.value.version + 1}`,
        entries: editEntries.value.filter((e) => e.key.trim()),
      },
      { ifMatch: selectedCfg.value.version }
    )
    ElMessage.success(`配置已发布为版本 v${data.version}`)
    changeSummary.value = ''
    await load()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function rollbackToSnapshot(snap: ConfigSnapshot) {
  if (!selectedCfg.value) return
  try {
    const { data } = await api.put<ConfigItem>(
      `/api/v1/config/configurations/${selectedCfg.value.cfg_id}`,
      {
        description: selectedCfg.value.description,
        changeSummary: `从历史快照 v${snap.version} 回滚`,
        entries: snap.entries,
      },
      { ifMatch: selectedCfg.value.version }
    )
    ElMessage.success(`已成功回滚至 v${snap.version} 的配置内容（新版本 v${data.version}）`)
    subView.value = 'editor'
    await load()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

function promptReveal(key: string) {
  revealTargetKey.value = key
  revealReason.value = '上线配置核对'
  revealDlg.value = true
}

async function confirmReveal() {
  if (!selectedCfg.value || !revealTargetKey.value) return
  try {
    const { data } = await api.post<{ plaintext: string }>(
      `/api/v1/config/configurations/${selectedCfg.value.cfg_id}/reveal`,
      { key: revealTargetKey.value, reason: revealReason.value.trim() || '日常巡检' }
    )
    revealedSecrets.value[revealTargetKey.value] = data.plaintext
    revealDlg.value = false
    ElMessage.warning('已临时展示明文，本次访问已记入操作审计日志')
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
      entries: [{ key: createForm.value.initialKey, value: createForm.value.initialVal, is_secret: createForm.value.isSecret }],
    })
    ElMessage.success('配置集创建成功')
    createDlg.value = false
    createForm.value = { name: '', format: 'kv', description: '', initialKey: 'APP_ENV', initialVal: 'production', isSecret: false }
    await load()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function selectEnv(env: EnvItem) {
  selectedEnv.value = env
  envMembersDraft.value = (env.members || []).map((m) => ({ ...m }))
  const { data } = await api.get<EnvViewCheck>(`/api/v1/config/environments/${env.env_id}/viewcheck`)
  viewCheck.value = data
}

function moveMember(idx: number, delta: number) {
  const next = idx + delta
  if (next < 0 || next >= envMembersDraft.value.length) return
  const arr = [...envMembersDraft.value]
  const tmp = arr[idx]
  arr[idx] = arr[next]
  arr[next] = tmp
  envMembersDraft.value = arr.map((m, i) => ({ ...m, priority: (i + 1) * 10 }))
}

async function saveEnvOrder() {
  if (!selectedEnv.value) return
  try {
    await api.put(
      `/api/v1/config/environments/${selectedEnv.value.env_id}/members`,
      { members: envMembersDraft.value.map((m, i) => ({ cfg_id: m.cfg_id, priority: (i + 1) * 10 })) },
      { ifMatch: selectedEnv.value.version }
    )
    ElMessage.success('环境配置优先级已保存，合并预览已刷新')
    await load()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function copyYAML() {
  try {
    await navigator.clipboard.writeText(yamlPreview.value)
    ElMessage.success('YAML 配置已复制')
  } catch {
    ElMessage.info('请手动复制')
  }
}

onMounted(load)
watch(() => [scope.tenantId, scope.appId], load)
</script>

<template>
  <div class="ks-page">
    <KsBanner
      title="配置中心"
      description="集中管理应用配置集与加密敏感凭证，支持 YAML 实时预览、版本差异对比回滚及多环境配置合并冲突检测。"
      :icon="KeyRound"
      :badge="`${configs.length} 配置集 · ${envs.length} 环境`"
      :tabs="[
        { key: 'configs', label: '配置集管理', count: configs.length },
        { key: 'environments', label: '环境合集与冲突检测', count: envs.length },
      ]"
      :active-tab="activeTab"
      @update:active-tab="activeTab = $event"
    >
      <template #actions>
        <el-button :loading="loading" @click="load"><RefreshCw :size="14" /><span class="ks-gap">刷新</span></el-button>
        <el-button type="primary" @click="createDlg = true"><Plus :size="14" /><span class="ks-gap">新建配置集</span></el-button>
      </template>
    </KsBanner>

    <!-- Tab 1: Config Sets Workbench -->
    <div v-if="activeTab === 'configs'" class="ks-split">
      <div class="ki-card ks-side-card">
        <el-input v-model="searchCfg" size="small" placeholder="搜索配置集名称..." clearable class="ks-mb-sm" />
        <div class="ks-cfg-list">
          <button
            v-for="c in filteredConfigs"
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

      <div v-if="selectedCfg" class="ki-card ks-main-card">
        <div class="ks-editor-top">
          <div>
            <div class="ks-editor-title">{{ selectedCfg.name }}</div>
            <div class="ks-editor-sub">{{ selectedCfg.description || '应用级键值与加密配置' }}</div>
          </div>

          <div class="ks-sub-tabs">
            <button type="button" class="ks-sub-tab" :class="{ 'is-active': subView === 'editor' }" @click="subView = 'editor'">键值编辑</button>
            <button type="button" class="ks-sub-tab" :class="{ 'is-active': subView === 'yaml' }" @click="subView = 'yaml'">YAML 预览</button>
            <button type="button" class="ks-sub-tab" :class="{ 'is-active': subView === 'history' }" @click="subView = 'history'">
              版本历史与对比 ({{ history.length }})
            </button>
          </div>
        </div>

        <!-- SubView A: KV Editor -->
        <template v-if="subView === 'editor'">
          <el-table :data="editEntries" size="small">
            <el-table-column label="配置键" width="180">
              <template #default="{ row }"><el-input v-model="row.key" size="small" class="ki-mono" /></template>
            </el-table-column>
            <el-table-column label="配置值" min-width="220">
              <template #default="{ row }">
                <div class="ks-val-cell">
                  <el-input v-model="row.value" size="small" :placeholder="row.is_secret ? '******（不修改则保留原密文）' : ''" />
                  <el-button v-if="row.is_secret" size="small" title="查看明文（记入审计）" @click="promptReveal(row.key)">
                    <Eye :size="12" />
                  </el-button>
                </div>
                <div v-if="revealedSecrets[row.key]" class="ks-revealed ki-mono">
                  <Lock :size="11" /> 明文: {{ revealedSecrets[row.key] }}
                </div>
              </template>
            </el-table-column>
            <el-table-column label="加密保护" width="90">
              <template #default="{ row }"><el-switch v-model="row.is_secret" size="small" /></template>
            </el-table-column>
            <el-table-column label="备注" width="160">
              <template #default="{ row }"><el-input v-model="row.comment" size="small" /></template>
            </el-table-column>
            <el-table-column label="" width="54">
              <template #default="{ $index }">
                <el-button size="small" type="danger" plain @click="editEntries.splice($index, 1)"><Trash2 :size="12" /></el-button>
              </template>
            </el-table-column>
          </el-table>

          <div class="ks-save-bar">
            <div class="ks-save-left">
              <el-button size="small" @click="editEntries.push({ key: '', value: '', is_secret: false, comment: '' })">
                <Plus :size="12" /><span class="ks-gap-xs">新增配置项</span>
              </el-button>
              <el-input v-model="changeSummary" size="small" placeholder="填写本次变更说明..." style="width: 280px" />
            </div>
            <el-button type="primary" size="small" @click="saveConfigVersion">
              <Save :size="13" /><span class="ks-gap">发布新版本 (v{{ selectedCfg.version + 1 }})</span>
            </el-button>
          </div>
        </template>

        <!-- SubView B: YAML Preview -->
        <template v-else-if="subView === 'yaml'">
          <div class="ks-yaml-bar">
            <span class="ki-muted">只读格式化预览（敏感凭证已自动掩码保护）</span>
            <el-button size="small" @click="copyYAML"><Copy :size="12" /><span class="ks-gap-xs">复制 YAML</span></el-button>
          </div>
          <pre class="ks-yaml-box ki-mono">{{ yamlPreview || '# 暂无配置项' }}</pre>
        </template>

        <!-- SubView C: Version History & Diff + One-Click Rollback -->
        <template v-else>
          <div class="ks-diff-head">
            <div class="ks-diff-sel">
              <History :size="15" />
              <span>对比历史版本：</span>
              <el-select v-model="compareVer" size="small" style="width: 140px">
                <el-option v-for="h in history" :key="h.version" :label="`版本 v${h.version}`" :value="h.version" />
              </el-select>
              <span class="ki-muted">→ 当前版本 v{{ selectedCfg.version }}</span>
            </div>
            <el-button
              v-if="compareVer !== null && compareVer !== selectedCfg.version"
              size="small"
              type="primary"
              @click="rollbackToSnapshot(history.find((h) => h.version === compareVer)!)"
            >
              <RotateCcw :size="12" /><span class="ks-gap-xs">回滚至 v{{ compareVer }}</span>
            </el-button>
          </div>

          <el-table :data="diffRows" size="small" class="ks-mb-md">
            <el-table-column prop="key" label="配置键" width="180"><template #default="{ row }"><span class="ki-mono">{{ row.key }}</span></template></el-table-column>
            <el-table-column prop="oldVal" :label="`历史 v${compareVer ?? '-'}`" min-width="160"><template #default="{ row }"><span class="ki-mono">{{ row.oldVal }}</span></template></el-table-column>
            <el-table-column prop="newVal" :label="`当前 v${selectedCfg.version}`" min-width="160"><template #default="{ row }"><span class="ki-mono">{{ row.newVal }}</span></template></el-table-column>
            <el-table-column label="差异" width="90">
              <template #default="{ row }">
                <span
                  class="ki-badge"
                  :class="row.change === 'ADDED' ? 'ks-pill-ok' : row.change === 'MODIFIED' ? 'ks-pill-warn' : row.change === 'REMOVED' ? 'ks-pill-danger' : 'ks-pill-neutral'"
                >
                  {{ row.change === 'ADDED' ? '新增' : row.change === 'MODIFIED' ? '变更' : row.change === 'REMOVED' ? '删除' : '无变化' }}
                </span>
              </template>
            </el-table-column>
          </el-table>

          <div class="section-title">全部历史快照</div>
          <el-table :data="history" size="small">
            <el-table-column label="版本" width="80"><template #default="{ row }"><span class="ki-mono">v{{ row.version }}</span></template></el-table-column>
            <el-table-column prop="change_summary" label="变更说明" min-width="200" />
            <el-table-column prop="changed_by" label="操作人" width="120" />
            <el-table-column label="操作" width="110">
              <template #default="{ row }">
                <el-button v-if="row.version !== selectedCfg.version" size="small" @click="rollbackToSnapshot(row)">回滚</el-button>
                <span v-else class="ki-badge ks-pill-ok">当前生效</span>
              </template>
            </el-table-column>
          </el-table>
        </template>
      </div>
    </div>

    <!-- Tab 2: Environment Composition & Priority Reordering -->
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
            <div class="ks-cfg-item__desc">包含 {{ e.members.length }} 个配置集（高优先级覆写低优先级）</div>
          </button>
        </div>
      </div>

      <div v-if="viewCheck && selectedEnv" class="ks-env-col">
        <div class="ki-card">
          <div class="ki-card__title">
            <span>配置叠加顺序（由上至下优先级递增）</span>
            <el-button size="small" type="primary" @click="saveEnvOrder">保存优先级排序</el-button>
          </div>
          <div class="ks-order-list">
            <div v-for="(m, idx) in envMembersDraft" :key="m.cfg_id" class="ks-order-row">
              <span class="ki-badge ks-pill-neutral ki-mono">#{{ idx + 1 }}</span>
              <span class="ks-order-name">{{ m.cfg_name }}</span>
              <span class="ki-muted ki-mono">v{{ m.cfg_version }}</span>
              <div class="ks-order-btns">
                <el-button size="small" :disabled="idx === 0" @click="moveMember(idx, -1)"><ArrowUp :size="12" /></el-button>
                <el-button size="small" :disabled="idx === envMembersDraft.length - 1" @click="moveMember(idx, 1)"><ArrowDown :size="12" /></el-button>
              </div>
            </div>
          </div>
        </div>

        <div class="ki-card">
          <div class="ki-card__title">
            <span>环境最终生效配置预览</span>
            <span class="ki-badge" :class="viewCheck.conflicts?.length ? 'ks-pill-warn' : 'ks-pill-ok'">
              {{ viewCheck.conflicts?.length || 0 }} 项键覆写
            </span>
          </div>

          <div v-if="viewCheck.conflicts?.length" class="ks-conflict-box">
            <div v-for="cf in viewCheck.conflicts" :key="cf.key" class="ks-conflict-row">
              <AlertTriangle :size="14" />
              <span class="ki-mono">{{ cf.key }}</span>
              <span>被多个配置集定义，当前按优先级采用 <strong>{{ cf.effective_source }}</strong> 的取值</span>
            </div>
          </div>

          <el-table :data="Object.entries(viewCheck.merged).map(([k, v]) => ({ key: k, value: v }))" size="small">
            <el-table-column prop="key" label="生效键名" width="220"><template #default="{ row }"><span class="ki-mono">{{ row.key }}</span></template></el-table-column>
            <el-table-column prop="value" label="合并后取值（敏感凭证已脱敏）" min-width="220"><template #default="{ row }"><span class="ki-mono">{{ row.value }}</span></template></el-table-column>
          </el-table>
        </div>
      </div>
    </div>

    <!-- Audited Secret Reveal Dialog -->
    <el-dialog v-model="revealDlg" title="查看加密配置明文" width="440px">
      <p class="ki-muted">您正在申请查看加密字段 <code class="ki-mono">{{ revealTargetKey }}</code> 的明文内容，系统将记录本次查看操作至审计日志。</p>
      <el-form label-position="top">
        <el-form-item label="查看事由" required>
          <el-input v-model="revealReason" placeholder="例如: 生产故障排查 / 数据库连接核对" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="revealDlg = false">取消</el-button>
        <el-button type="primary" @click="confirmReveal">确认查看明文</el-button>
      </template>
    </el-dialog>

    <!-- Create Config Modal -->
    <el-dialog v-model="createDlg" title="新建配置集" width="460px">
      <el-form label-position="top">
        <el-form-item label="配置集名称" required><el-input v-model="createForm.name" placeholder="例如: his-redis-cluster" /></el-form-item>
        <el-form-item label="描述说明"><el-input v-model="createForm.description" /></el-form-item>
        <el-form-item label="首个配置键" required><el-input v-model="createForm.initialKey" /></el-form-item>
        <el-form-item label="首个配置值" required><el-input v-model="createForm.initialVal" /></el-form-item>
        <el-form-item label="作为敏感密文加密存储"><el-switch v-model="createForm.isSecret" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDlg = false">取消</el-button>
        <el-button type="primary" @click="createConfig">确认创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.ks-gap {
  margin-left: 6px;
}

.ks-gap-xs {
  margin-left: 4px;
}

.ks-mb-sm {
  margin-bottom: 10px;
}

.ks-mb-md {
  margin-bottom: 16px;
}

.ks-split {
  display: grid;
  grid-template-columns: 280px minmax(0, 1fr);
  gap: var(--ki-space-4);
  align-items: start;
}

@media (max-width: 1024px) {
  .ks-split {
    grid-template-columns: 1fr;
  }
}

.ks-env-col {
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

.ks-editor-top {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--ki-border-subtle);
}

.ks-editor-title {
  font-size: var(--ki-fs-section);
  font-weight: var(--ki-fw-bold);
  color: var(--ki-text-primary);
}

.ks-editor-sub {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
  margin-top: 2px;
}

.ks-sub-tabs {
  display: inline-flex;
  gap: 4px;
  padding: 3px;
  border-radius: var(--ki-radius-pill);
  background-color: var(--ki-bg-canvas);
}

.ks-sub-tab {
  height: 28px;
  padding: 0 12px;
  border: none;
  border-radius: var(--ki-radius-pill);
  background: transparent;
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-medium);
  cursor: pointer;
}

.ks-sub-tab.is-active {
  background-color: var(--ki-bg-surface);
  color: var(--ki-text-primary);
  font-weight: var(--ki-fw-semibold);
  box-shadow: var(--ki-shadow-surface);
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
  display: flex;
  align-items: center;
  gap: 4px;
}

.ks-save-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid var(--ki-border-subtle);
}

.ks-save-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.ks-yaml-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
  font-size: var(--ki-fs-xs);
}

.ks-yaml-box {
  margin: 0;
  padding: 16px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-terminal);
  color: var(--ki-term-fg);
  line-height: 1.65;
  overflow-x: auto;
}

.ks-diff-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 12px;
}

.ks-diff-sel {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: var(--ki-fs-sm);
}

.ks-order-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ks-order-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-subtle);
  border: 1px solid var(--ki-border-subtle);
}

.ks-order-name {
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
  flex: 1;
}

.ks-order-btns {
  display: flex;
  gap: 4px;
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

.ks-pill-danger {
  background-color: var(--ki-status-danger-subtle);
  border: 1px solid var(--ki-status-danger-border);
  color: var(--ki-status-danger-text);
}

.ks-pill-neutral {
  background-color: var(--ki-status-neutral-subtle);
  border: 1px solid var(--ki-status-neutral-border);
  color: var(--ki-status-neutral-text);
}
</style>
