<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Boxes, Copy, KeyRound, Plus, RefreshCw, RotateCw, Trash2 } from 'lucide-vue-next'
import KsBanner from '@/components/ks/KsBanner.vue'
import { api, ApiError } from '@/api/client'
import { useScopeStore, type AppItem } from '@/stores/scope'

interface AppKeyRow {
  key_id: string
  key_prefix: string
  status: string
  created_at: string
}

const scope = useScopeStore()
const apps = ref<AppItem[]>([])
const appKeys = ref<AppKeyRow[]>([])
const loading = ref(false)
const newPlaintext = ref('')
const appDlg = ref(false)
const form = ref({ code: '', name: '', description: '' })

async function load() {
  loading.value = true
  try {
    const [a, k] = await Promise.all([
      api.get<{ items: AppItem[] }>('/api/v1/iam/apps'),
      api.get<{ items: AppKeyRow[] }>('/api/v1/iam/apps/keys'),
    ])
    apps.value = a.data.items || []
    appKeys.value = k.data.items || []
  } finally {
    loading.value = false
  }
}

async function createApp() {
  try {
    await api.post('/api/v1/iam/apps', form.value)
    ElMessage.success('应用已创建')
    appDlg.value = false
    form.value = { code: '', name: '', description: '' }
    await Promise.all([load(), scope.refreshMe()])
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function rotateAppKey() {
  try {
    const { data } = await api.post<{ plaintext: string }>('/api/v1/iam/apps/keys/rotate', { graceMinutes: 1440 })
    newPlaintext.value = data.plaintext
    ElMessage.success('新密钥已生成，原有密钥保留 24 小时平滑过渡期')
    await load()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function revokeKey(keyId: string) {
  try {
    await api.del(`/api/v1/iam/apps/keys/${keyId}`)
    ElMessage.success('访问密钥已吊销')
    await load()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function copyKey() {
  try {
    await navigator.clipboard.writeText(newPlaintext.value)
    ElMessage.success('密钥已复制')
  } catch {
    ElMessage.info('请手动选中并复制')
  }
}

onMounted(load)
watch(() => [scope.tenantId, scope.appId], load)
</script>

<template>
  <div class="ks-page">
    <KsBanner
      title="应用管理"
      description="管理企业空间下的业务应用及应用级 API 访问密钥，支持零停机双轨轮换。"
      :icon="Boxes"
      :badge="`${apps.length} 个应用`"
    >
      <template #actions>
        <el-button :loading="loading" @click="load"><RefreshCw :size="14" /><span class="ks-gap">刷新</span></el-button>
        <el-button @click="rotateAppKey"><RotateCw :size="14" /><span class="ks-gap">签发 / 轮换密钥</span></el-button>
        <el-button type="primary" @click="appDlg = true"><Plus :size="14" /><span class="ks-gap">新建应用</span></el-button>
      </template>
    </KsBanner>

    <div v-if="newPlaintext" class="ki-card ks-secret-banner">
      <div class="ks-secret-head">
        <div class="ks-secret-banner__title"><KeyRound :size="15" /><span>新签发的应用访问密钥（明文仅展示一次，请妥善保存）</span></div>
        <el-button size="small" @click="copyKey"><Copy :size="12" /><span class="ks-gap-xs">复制密钥</span></el-button>
      </div>
      <code class="ki-mono ks-secret-banner__val">{{ newPlaintext }}</code>
    </div>

    <div class="ks-two-col">
      <div class="ki-card">
        <div class="ki-card__title"><span>应用列表</span></div>
        <el-table :data="apps" size="small">
          <el-table-column label="应用名称" min-width="180">
            <template #default="{ row }">
              <div class="ks-cell-main">{{ row.name }}</div>
              <div class="ks-cell-sub ki-mono">{{ row.code }}</div>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="100">
            <template #default="{ row }">
              <el-button size="small" :type="row.app_id === scope.appId ? 'primary' : 'default'" @click="scope.switchApp(row.app_id)">
                {{ row.app_id === scope.appId ? '当前' : '切换' }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <div class="ki-card">
        <div class="ki-card__title"><span>应用访问密钥（{{ scope.currentApp?.name || scope.appId }}）</span></div>
        <el-table :data="appKeys" size="small" empty-text="当前应用尚未签发访问密钥，点击「签发 / 轮换密钥」即可创建">
          <el-table-column label="密钥前缀" min-width="170">
            <template #default="{ row }"><span class="ki-mono">lo_live_{{ row.key_prefix }}***</span></template>
          </el-table-column>
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <span class="ki-badge" :class="row.status === 'ACTIVE' ? 'ks-pill-ok' : row.status === 'ROTATING' ? 'ks-pill-warn' : 'ks-pill-danger'">
                {{ row.status === 'ACTIVE' ? '生效中' : row.status === 'ROTATING' ? '过渡期' : '已吊销' }}
              </span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="80">
            <template #default="{ row }">
              <el-button size="small" type="danger" plain :disabled="row.status === 'REVOKED'" @click="revokeKey(row.key_id)">
                <Trash2 :size="12" />
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </div>

    <el-dialog v-model="appDlg" title="新建空间应用" width="440px">
      <el-form label-position="top">
        <el-form-item label="应用标识" required><el-input v-model="form.code" placeholder="例如: lis-core" /></el-form-item>
        <el-form-item label="应用名称" required><el-input v-model="form.name" placeholder="例如: 检验科核心系统" /></el-form-item>
        <el-form-item label="说明"><el-input v-model="form.description" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="appDlg = false">取消</el-button>
        <el-button type="primary" @click="createApp">确认创建</el-button>
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

.ks-two-col {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--ki-space-4);
}

@media (max-width: 1024px) {
  .ks-two-col {
    grid-template-columns: 1fr;
  }
}

.ks-cell-main {
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.ks-cell-sub {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.ks-secret-banner {
  margin-bottom: var(--ki-space-4);
  background-color: var(--ki-status-ok-subtle);
  border-color: var(--ki-status-ok-border);
}

.ks-secret-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 6px;
}

.ks-secret-banner__title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-status-ok-text);
}

.ks-secret-banner__val {
  display: block;
  word-break: break-all;
}

.ks-pill-ok {
  background-color: var(--ki-status-ok-subtle);
  border: 1px solid var(--ki-status-ok-border);
  color: var(--ki-status-ok-text);
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
</style>
