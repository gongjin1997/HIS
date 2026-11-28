<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Boxes, KeyRound, Plus, RefreshCw, RotateCw, Trash2 } from 'lucide-vue-next'
import KsBanner from '@/components/ks/KsBanner.vue'
import { api, ApiError } from '@/api/client'
import { useScopeStore, type AppItem } from '@/stores/scope'

interface AppKeyRow {
  key_id: string
  key_prefix: string
  status: string
  grace_until?: string
  last_used_at?: string
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
    ElMessage.success('应用已创建并自动初始化隔离作用域与 CMDB 元模型')
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
    ElMessage.success('已签发 256-bit lo_live_ AppKey，旧密钥自动进入 24h 双轨平滑期')
    await load()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function revokeKey(keyId: string) {
  try {
    await api.del(`/api/v1/iam/apps/keys/${keyId}`)
    ElMessage.success('AppKey 已吊销')
    await load()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

onMounted(load)
watch(() => [scope.tenantId, scope.appId], load)
</script>

<template>
  <div class="ks-page">
    <KsBanner
      title="应用中心与双轨 AppKey 轮换"
      description="管理当前企业空间下的隔离应用 (app_id) 及 256-bit lo_live_ 双轨 API 密钥，零停机轮换，数据库仅存 SHA-256 哈希。"
      :icon="Boxes"
      :badge="`${apps.length} 个应用`"
    >
      <template #actions>
        <el-button :loading="loading" @click="load"><RefreshCw :size="14" /><span class="ks-gap">刷新</span></el-button>
        <el-button @click="rotateAppKey"><RotateCw :size="14" /><span class="ks-gap">轮换当前应用 AppKey</span></el-button>
        <el-button type="primary" @click="appDlg = true"><Plus :size="14" /><span class="ks-gap">新建应用</span></el-button>
      </template>
    </KsBanner>

    <div v-if="newPlaintext" class="ki-card ks-secret-banner">
      <div class="ks-secret-banner__title"><KeyRound :size="15" /><span>新签发 AppKey（明文仅展示一次，请立即保存）：</span></div>
      <code class="ki-mono ks-secret-banner__val">{{ newPlaintext }}</code>
    </div>

    <div class="ks-two-col">
      <div class="ki-card">
        <div class="ki-card__title"><span>应用列表 ({{ scope.tenantId }})</span></div>
        <el-table :data="apps" size="small" stripe>
          <el-table-column prop="code" label="应用编码" width="140"><template #default="{ row }"><span class="ki-mono">{{ row.code }}</span></template></el-table-column>
          <el-table-column prop="name" label="应用名称" min-width="150" />
          <el-table-column label="切换" width="100">
            <template #default="{ row }">
              <el-button size="small" :type="row.app_id === scope.appId ? 'primary' : 'default'" @click="scope.switchApp(row.app_id)">
                {{ row.app_id === scope.appId ? '活跃' : '切换' }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <div class="ki-card">
        <div class="ki-card__title"><span>当前应用 AppKey 密钥列表 ({{ scope.appId }})</span></div>
        <el-table :data="appKeys" size="small" stripe empty-text="尚未签发 AppKey，点击右上角「轮换当前应用 AppKey」生成">
          <el-table-column label="密钥前缀" width="160">
            <template #default="{ row }"><span class="ki-mono">lo_live_{{ row.key_prefix }}***</span></template>
          </el-table-column>
          <el-table-column prop="status" label="双轨状态" width="110">
            <template #default="{ row }">
              <span class="ki-badge" :class="row.status === 'ACTIVE' ? 'ks-pill-ok' : row.status === 'ROTATING' ? 'ks-pill-warn' : 'ks-pill-danger'">{{ row.status }}</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="90">
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
        <el-form-item label="应用编码" required><el-input v-model="form.code" placeholder="例如: lis-core" /></el-form-item>
        <el-form-item label="应用名称" required><el-input v-model="form.name" placeholder="例如: 检验科核心系统" /></el-form-item>
        <el-form-item label="描述"><el-input v-model="form.description" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="appDlg = false">取消</el-button>
        <el-button type="primary" @click="createApp">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.ks-gap {
  margin-left: 6px;
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

.ks-secret-banner {
  margin-bottom: var(--ki-space-4);
  background-color: var(--ki-status-ok-subtle);
  border-color: var(--ki-status-ok-border);
}

.ks-secret-banner__title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-status-ok-text);
  margin-bottom: 6px;
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
