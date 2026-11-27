<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
  KeyRound,
  RefreshCw,
  FolderPlus,
  ShieldCheck,
  Bell,
  Users,
  GitBranch,
  Download,
  Trash2,
  Send,
  Database,
} from 'lucide-vue-next'
import { apiDownload, apiRequest, apiRequestText } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import { useKiFormat } from '@/composables/useKiFormat'
import PageHeader from '@/components/PageHeader.vue'
import KiMonoText from '@/components/ki/KiMonoText.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiSecretInput from '@/components/ki/KiSecretInput.vue'
import KiDangerConfirmModal from '@/components/domain/KiDangerConfirmModal.vue'

const auth = useAuthStore()
const ws = useWorkspaceStore()
const { formatAbsTime } = useKiFormat()

const activeTab = ref('general')
const loading = ref(false)

// Tab 1: Project & Appearance
const newProj = reactive({ name: '', description: '' })

// Tab 2: Credentials Vault & KEK
const credentials = ref<Array<Record<string, unknown>>>([])
const newCred = reactive({ name: '', kind: 'ssh_password', secret: '' })
const rewrapping = ref(false)
const rewrappedCount = ref<number | null>(null)
const deleteCredModal = reactive({ open: false, cred: null as Record<string, unknown> | null, loading: false })

// Tab 3: Notification Channels
const channels = ref<Array<Record<string, unknown>>>([])
const newChan = reactive({
  name: 'ops-webhook',
  kind: 'webhook',
  url: '',
  events: ['approval.requested', 'operation.failed'],
})
const testingNotify = ref(false)

// Tab 4: Users & API Tokens
const users = ref<Array<Record<string, unknown>>>([])
const tokens = ref<Array<Record<string, unknown>>>([])
const showCreateUser = ref(false)
const newUser = reactive({ username: '', displayName: '', password: '', role: 'operator' })
const tokName = ref('ci-deployer')
const issuedToken = ref('')

// Tab 5: GitOps, Support Bundle & CMDB Sync
const gitopsYaml = ref('')
const gitopsDiffEntries = ref<Array<Record<string, unknown>>>([])
const gitopsExitCode = ref<number | null>(null)
const downloadingBundle = ref(false)
const cmdbPayload = ref(JSON.stringify({
  source: 'external-cmdb',
  hosts: [
    { externalId: 'cmdb-001', name: 'sync-node-01', address: '10.20.9.11', faultZone: 'az-1' },
  ],
}, null, 2))
const cmdbSyncResult = ref('')

async function loadGovernance() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const [cRes, nRes, tRes, uRes] = await Promise.all([
      apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${ws.currentProjectId}/credentials`).catch(() => ({ data: { items: [] } })),
      apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${ws.currentProjectId}/notification-channels`).catch(() => ({ data: { items: [] } })),
      apiRequest<{ items: Array<Record<string, unknown>> }>('GET', '/api/v1/auth/tokens').catch(() => ({ data: { items: [] } })),
      apiRequest<{ items: Array<Record<string, unknown>> }>('GET', '/api/v1/users').catch(() => ({ data: { items: [] } })),
    ])
    credentials.value = cRes.data.items || []
    channels.value = nRes.data.items || []
    tokens.value = tRes.data.items || []
    users.value = uRes.data.items || []
  } finally {
    loading.value = false
  }
}

async function createProject() {
  if (!newProj.name.trim()) return
  try {
    await apiRequest('POST', '/api/v1/projects', { body: { ...newProj }, idempotent: true })
    ElMessage.success('项目已创建')
    newProj.name = ''
    newProj.description = ''
    await ws.loadProjects()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function createCredential() {
  if (!newCred.name.trim() || !newCred.secret) {
    ElMessage.warning('请输入凭据名称和密文内容')
    return
  }
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/credentials`, {
      body: { name: newCred.name, kind: newCred.kind, secret: newCred.secret },
      idempotent: true,
    })
    ElMessage.success('凭据已使用信封加密存入保险库（列表中永不回显明文）')
    newCred.name = ''
    newCred.secret = ''
    await loadGovernance()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

function openDeleteCred(row: Record<string, unknown>) {
  deleteCredModal.cred = row
  deleteCredModal.open = true
}

async function confirmDeleteCred() {
  if (!deleteCredModal.cred) return
  deleteCredModal.loading = true
  try {
    await apiRequest('DELETE', `/api/v1/projects/${ws.currentProjectId}/credentials/${deleteCredModal.cred.id}`)
    ElMessage.success('凭据已吊销并删除')
    deleteCredModal.open = false
    await loadGovernance()
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    deleteCredModal.loading = false
  }
}

async function rewrapSecrets() {
  if (!ws.currentProjectId) return
  rewrapping.value = true
  try {
    const { data } = await apiRequest<{ rewrapped: number }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/secrets:rewrap`,
      { idempotent: true }
    )
    rewrappedCount.value = data.rewrapped
    ElMessage.success(`KEK 信封密钥重包装完成：已轮换 ${data.rewrapped} 条凭据`)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    rewrapping.value = false
  }
}

async function createChannel() {
  if (!newChan.name.trim()) return
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/notification-channels`, {
      body: {
        name: newChan.name,
        kind: newChan.kind,
        enabled: true,
        events: newChan.events,
        config: { url: newChan.url },
      },
      idempotent: true,
    })
    ElMessage.success('通知通道已创建')
    newChan.url = ''
    await loadGovernance()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function testNotify() {
  testingNotify.value = true
  try {
    const { data } = await apiRequest<{ sent: number; folded: number }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/notification-channels:test`,
      { body: { summary: '来自控制台的告警通知防风暴连通测试' } }
    )
    ElMessage.success(`通知测试完成：已投递 ${data.sent} 条，防风暴折叠 ${data.folded} 条`)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    testingNotify.value = false
  }
}

async function createUser() {
  if (!newUser.username.trim() || !newUser.password) return
  try {
    await apiRequest('POST', '/api/v1/users', {
      body: {
        username: newUser.username,
        displayName: newUser.displayName || newUser.username,
        password: newUser.password,
        roleBindings: [{ projectId: ws.currentProjectId || '*', role: newUser.role }],
      },
      idempotent: true,
    })
    ElMessage.success(`用户 ${newUser.username} 已创建`)
    showCreateUser.value = false
    newUser.username = ''
    newUser.displayName = ''
    newUser.password = ''
    await loadGovernance()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function createToken() {
  try {
    const { data } = await apiRequest<{ token: string }>('POST', '/api/v1/auth/tokens', {
      body: { name: tokName.value, scopes: ['*'], ttlDays: 90 },
      idempotent: true,
    })
    issuedToken.value = data.token
    ElMessage.success('API Token 已签发（仅本次明文展示，请妥善保存）')
    await loadGovernance()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function revokeToken(tokenId: string) {
  try {
    await apiRequest('DELETE', `/api/v1/auth/tokens/${tokenId}`)
    ElMessage.success('API Token 已立即吊销')
    await loadGovernance()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function exportGitOps() {
  try {
    gitopsYaml.value = await apiRequestText('GET', `/api/v1/projects/${ws.currentProjectId}/gitops/export`)
    ElMessage.success('GitOps 声明式清单已导出')
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function diffGitOps() {
  if (!gitopsYaml.value.trim()) {
    await exportGitOps()
  }
  try {
    const { data } = await apiRequest<{ entries: Array<Record<string, unknown>>; exitCode: number }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/gitops:diff`,
      { body: { desiredYaml: gitopsYaml.value } }
    )
    gitopsDiffEntries.value = data.entries || []
    gitopsExitCode.value = data.exitCode
    ElMessage.success(`GitOps Diff 完成（exitCode=${data.exitCode}，共 ${gitopsDiffEntries.value.length} 项差异）`)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function downloadSupportBundle() {
  downloadingBundle.value = true
  try {
    const res = await apiDownload(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/support-bundle`,
      'kinstall-support-bundle.tar.gz'
    )
    ElMessage.success(`已下载脱敏诊断包 ${res.filename} (${res.size} B)`)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    downloadingBundle.value = false
  }
}

async function syncExternalCMDB() {
  try {
    const parsed = JSON.parse(cmdbPayload.value) as Record<string, unknown>
    const { data } = await apiRequest<Record<string, unknown>>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/cmdb:sync`,
      { body: parsed, idempotent: true }
    )
    cmdbSyncResult.value = JSON.stringify(data, null, 2)
    ElMessage.success('外部 CMDB 资产同步已完成')
  } catch (e) {
    ElMessage.error(String(e))
  }
}

watch(() => ws.currentProjectId, loadGovernance)
onMounted(loadGovernance)
</script>

<template>
  <div>
    <PageHeader title="平台治理与系统设置" :refresh="loadGovernance" :refreshing="loading" />

    <div class="ki-card">
      <el-tabs v-model="activeTab">
        <!-- Tab 1: 项目与外观 -->
        <el-tab-pane label="项目与外观" name="general">
          <div class="ki-grid-2">
            <div class="ki-surface panel-box">
              <div class="section-title">新建租户项目</div>
              <el-form label-position="top">
                <el-form-item label="项目标识"><el-input v-model="newProj.name" placeholder="his-prod" class="ki-mono" /></el-form-item>
                <el-form-item label="项目说明"><el-input v-model="newProj.description" placeholder="核心生产集群" /></el-form-item>
                <el-button type="primary" @click="createProject">
                  <FolderPlus :size="14" class="btn-ico" /> 创建项目
                </el-button>
              </el-form>
            </div>

            <div class="ki-surface panel-box">
              <div class="section-title">外观与界面偏好</div>
              <div class="kv-grid">
                <div class="k">界面主题</div>
                <div>
                  <el-radio-group :model-value="ws.themePreference" size="small" @update:model-value="(v: string) => ws.setTheme(v as 'light' | 'dark' | 'system')">
                    <el-radio-button value="light">浅色（默认）</el-radio-button>
                    <el-radio-button value="dark">暗色</el-radio-button>
                    <el-radio-button value="system">跟随系统</el-radio-button>
                  </el-radio-group>
                </div>
                <div class="k">行密度</div>
                <div>
                  <el-radio-group v-model="ws.density" size="small">
                    <el-radio-button value="comfortable">舒适 (44px)</el-radio-button>
                    <el-radio-button value="compact">紧凑 (34px)</el-radio-button>
                  </el-radio-group>
                </div>
                <div class="k">当前角色</div>
                <div class="ki-flex-row"><ShieldCheck :size="14" class="ki-text-ok" /> <b class="ki-mono">{{ auth.primaryRole }}</b></div>
                <div class="k">界面语言</div>
                <div><el-button size="small" @click="ws.toggleLang">{{ ws.lang }}</el-button></div>
              </div>
            </div>
          </div>
        </el-tab-pane>

        <!-- Tab 2: 凭据保险库 & KEK 轮换 -->
        <el-tab-pane :label="`凭据保险库 (${credentials.length})`" name="vault">
          <div class="ki-grid-2 ki-mb-4">
            <div class="ki-surface panel-box">
              <div class="section-title">新增信封加密凭据</div>
              <el-form label-position="top">
                <div class="ki-grid-2">
                  <el-form-item label="凭据名称"><el-input v-model="newCred.name" placeholder="ssh-prod-root" class="ki-mono" /></el-form-item>
                  <el-form-item label="凭据类型">
                    <el-select v-model="newCred.kind" class="ki-w-full">
                      <el-option label="SSH 密码 (ssh_password)" value="ssh_password" />
                      <el-option label="SSH 私钥 (ssh_key)" value="ssh_key" />
                      <el-option label="Sudo 提权密码 (sudo_password)" value="sudo_password" />
                      <el-option label="OBS / S3 AK/SK (s3_aksk)" value="s3_aksk" />
                      <el-option label="镜像仓库认证 (registry_auth)" value="registry_auth" />
                      <el-option label="Webhook 签名密钥 (webhook_secret)" value="webhook_secret" />
                    </el-select>
                  </el-form-item>
                </div>
                <div class="ki-mb-3">
                  <KiSecretInput v-model="newCred.secret" label="密文内容（AES-256-GCM / SM4-GCM 信封加密）" required />
                </div>
                <el-button type="primary" @click="createCredential">
                  <KeyRound :size="14" class="btn-ico" /> 加密存入保险库
                </el-button>
              </el-form>
            </div>

            <div class="ki-surface panel-box">
              <div class="section-title">KEK 主密钥零停机重包装 (O-11)</div>
              <p class="ki-secondary settings-desc">
                使用最新 KEK 版本重新封装当前项目内全部凭据的 DEK，密文不落明文、全过程写入 SHA-256 + SM3 审计链。
              </p>
              <div class="ki-flex-row">
                <el-button
                  v-if="auth.can('secrets:rewrap', ws.currentProjectId)"
                  type="primary"
                  plain
                  :loading="rewrapping"
                  @click="rewrapSecrets"
                >
                  <RefreshCw :size="14" class="btn-ico" /> 立即重包装项目凭据
                </el-button>
                <span v-if="rewrappedCount !== null" class="ki-text-ok ki-mono">已轮换：{{ rewrappedCount }} 条</span>
              </div>
            </div>
          </div>

          <el-table :data="credentials" size="small">
            <el-table-column prop="name" label="名称" min-width="160">
              <template #default="{ row }"><b class="ki-mono">{{ row.name }}</b></template>
            </el-table-column>
            <el-table-column label="凭据 ID" width="210">
              <template #default="{ row }"><KiMonoText :value="String(row.id || '')" /></template>
            </el-table-column>
            <el-table-column prop="kind" label="类型" width="140" />
            <el-table-column label="KEK 版本" width="90">
              <template #default="{ row }"><span class="ki-mono">v{{ row.keyVersion || 1 }}</span></template>
            </el-table-column>
            <el-table-column label="状态" width="100">
              <template #default="{ row }"><KiStatusBadge :status="String(row.state || 'active')" /></template>
            </el-table-column>
            <el-table-column label="操作" width="100" fixed="right">
              <template #default="{ row }">
                <el-button link type="danger" @click="openDeleteCred(row)">
                  <Trash2 :size="13" class="btn-ico" /> 吊销
                </el-button>
              </template>
            </el-table-column>
            <template #empty><el-empty description="当前项目暂无保险库凭据" :image-size="60" /></template>
          </el-table>
        </el-tab-pane>

        <!-- Tab 3: 通知通道 & 防风暴 -->
        <el-tab-pane :label="`通知通道 (${channels.length})`" name="notify">
          <div class="ki-surface panel-box ki-mb-4">
            <div class="section-title">新增告警与审批通知通道 (5min 防风暴折叠)</div>
            <el-form label-position="top">
              <div class="ki-grid-3">
                <el-form-item label="通道名称"><el-input v-model="newChan.name" class="ki-mono" /></el-form-item>
                <el-form-item label="通道类型">
                  <el-select v-model="newChan.kind" class="ki-w-full">
                    <el-option label="Generic Webhook" value="webhook" />
                    <el-option label="企业微信 (wecom)" value="wecom" />
                    <el-option label="钉钉机器人 (dingtalk)" value="dingtalk" />
                    <el-option label="飞书机器人 (feishu)" value="feishu" />
                    <el-option label="邮件 SMTP (email)" value="email" />
                  </el-select>
                </el-form-item>
                <el-form-item label="目标 URL / 地址"><el-input v-model="newChan.url" placeholder="https://hook.internal/notify" class="ki-mono" /></el-form-item>
              </div>
              <div class="ki-flex-row">
                <el-button type="primary" @click="createChannel">
                  <Bell :size="14" class="btn-ico" /> 保存通知通道
                </el-button>
                <el-button :loading="testingNotify" @click="testNotify">
                  <Send :size="14" class="btn-ico" /> 发送连通与防风暴测试
                </el-button>
              </div>
            </el-form>
          </div>

          <el-table :data="channels" size="small">
            <el-table-column prop="name" label="通道名称" min-width="160" />
            <el-table-column prop="kind" label="类型" width="110" />
            <el-table-column label="订阅事件">
              <template #default="{ row }"><span class="ki-mono">{{ (row.events as string[] || []).join(', ') || '*' }}</span></template>
            </el-table-column>
            <el-table-column label="状态" width="100">
              <template #default="{ row }"><KiStatusBadge :status="row.enabled ? 'ok' : 'pending'" :label="row.enabled ? '启用' : '停用'" /></template>
            </el-table-column>
            <template #empty><el-empty description="尚未配置通知通道" :image-size="60" /></template>
          </el-table>
        </el-tab-pane>

        <!-- Tab 4: 用户管理 & API Token -->
        <el-tab-pane :label="`用户与 Token (${users.length}/${tokens.length})`" name="iam">
          <div class="ki-grid-2">
            <div class="ki-surface panel-box">
              <div class="table-heading">
                <span class="section-title no-mb">平台用户与 5 级 RBAC</span>
                <el-button size="small" type="primary" @click="showCreateUser = true">
                  <Users :size="13" class="btn-ico" /> 新增用户
                </el-button>
              </div>
              <el-table :data="users" size="small">
                <el-table-column prop="username" label="用户名" width="130">
                  <template #default="{ row }"><b class="ki-mono">{{ row.username }}</b></template>
                </el-table-column>
                <el-table-column prop="displayName" label="显示名" />
                <el-table-column label="角色绑定">
                  <template #default="{ row }">
                    <span class="ki-mono">{{ ((row.roleBindings as Array<{ role: string }>) || []).map((r) => r.role).join(', ') }}</span>
                  </template>
                </el-table-column>
              </el-table>
            </div>

            <div class="ki-surface panel-box">
              <div class="section-title">CLI / CI 专用 API Token</div>
              <div class="ki-flex-row ki-mb-3">
                <el-input v-model="tokName" placeholder="ci-deployer" class="ki-mono ki-flex-1" />
                <el-button type="primary" @click="createToken">
                  <KeyRound :size="14" class="btn-ico" /> 签发 90 天 Token
                </el-button>
              </div>
              <div v-if="issuedToken" class="token-out ki-mb-3">
                <KiMonoText :value="issuedToken" :ellipsis="false" />
              </div>
              <el-table :data="tokens" size="small">
                <el-table-column prop="name" label="名称" />
                <el-table-column label="前缀" width="110">
                  <template #default="{ row }"><span class="ki-mono">{{ row.tokenPrefix }}…</span></template>
                </el-table-column>
                <el-table-column label="到期时间" width="145">
                  <template #default="{ row }"><span class="ki-mono">{{ formatAbsTime(String(row.expiresAt || '')) }}</span></template>
                </el-table-column>
                <el-table-column label="操作" width="75">
                  <template #default="{ row }">
                    <el-button link type="danger" @click="revokeToken(String(row.id))">吊销</el-button>
                  </template>
                </el-table-column>
              </el-table>
            </div>
          </div>
        </el-tab-pane>

        <!-- Tab 5: GitOps · 诊断包 · CMDB 同步 -->
        <el-tab-pane label="GitOps · 诊断包 · CMDB" name="ops">
          <div class="ki-grid-2">
            <div class="ki-surface panel-box">
              <div class="table-heading">
                <span class="section-title no-mb">GitOps 声明式清单导出与 Drift Diff</span>
                <div class="ki-flex-row">
                  <el-button size="small" @click="exportGitOps">
                    <GitBranch :size="13" class="btn-ico" /> 导出 YAML
                  </el-button>
                  <el-button size="small" type="primary" plain @click="diffGitOps">执行 Diff Dry-Run</el-button>
                </div>
              </div>
              <el-input v-model="gitopsYaml" type="textarea" :rows="10" class="ki-mono ki-mb-3" placeholder="点击「导出 YAML」拉取当前项目完整声明式快照" />
              <div v-if="gitopsExitCode !== null" class="ki-secondary ki-mono">
                ExitCode: {{ gitopsExitCode }} · Diff 项数: {{ gitopsDiffEntries.length }}
              </div>
            </div>

            <div class="ki-flex-col">
              <div class="ki-surface panel-box">
                <div class="section-title">一键脱敏诊断包 (Support Bundle)</div>
                <p class="ki-secondary settings-desc">
                  自动汇总项目状态、最近操作日志与环境拓扑，自动脱敏全部密码/Token/私钥后打包为 `.tar.gz` 下载。
                </p>
                <el-button type="primary" :loading="downloadingBundle" @click="downloadSupportBundle">
                  <Download :size="14" class="btn-ico" /> 下载脱敏诊断包 (.tar.gz)
                </el-button>
              </div>

              <div class="ki-surface panel-box">
                <div class="table-heading">
                  <span class="section-title no-mb">外部 CMDB 资产同步 (cmdb:sync)</span>
                  <el-button size="small" @click="syncExternalCMDB">
                    <Database :size="13" class="btn-ico" /> 推送同步
                  </el-button>
                </div>
                <el-input v-model="cmdbPayload" type="textarea" :rows="5" class="ki-mono" />
                <pre v-if="cmdbSyncResult" class="ki-mono cmdb-out ki-mt-3">{{ cmdbSyncResult }}</pre>
              </div>
            </div>
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>

    <el-dialog v-model="showCreateUser" title="新增平台用户 (Argon2id + 5 级 RBAC)" width="460px">
      <el-form label-position="top">
        <el-form-item label="用户名"><el-input v-model="newUser.username" class="ki-mono" /></el-form-item>
        <el-form-item label="显示名称"><el-input v-model="newUser.displayName" /></el-form-item>
        <el-form-item label="角色">
          <el-select v-model="newUser.role" class="ki-w-full">
            <el-option label="平台超级管理员 (admin)" value="admin" />
            <el-option label="项目管理员 (project-admin)" value="project-admin" />
            <el-option label="运维执行员 (operator)" value="operator" />
            <el-option label="安全审计员 (auditor)" value="auditor" />
            <el-option label="只读观察者 (viewer)" value="viewer" />
          </el-select>
        </el-form-item>
        <KiSecretInput v-model="newUser.password" label="初始密码（至少 10 位强口令）" required />
      </el-form>
      <template #footer>
        <el-button @click="showCreateUser = false">取消</el-button>
        <el-button type="primary" @click="createUser">创建用户</el-button>
      </template>
    </el-dialog>

    <KiDangerConfirmModal
      v-model="deleteCredModal.open"
      :env="ws.currentEnv"
      title="确认吊销并删除保险库凭据"
      :resource-name="String(deleteCredModal.cred?.name || '')"
      risk-summary="若仍有主机引用此凭据，数据库外键保护将阻止删除；否则信封密文将被永久清除。"
      confirm-label="解锁并删除凭据"
      :loading="deleteCredModal.loading"
      @confirm="confirmDeleteCred"
    />
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.panel-box {
  padding: 16px 18px;
}

.no-mb {
  margin-bottom: 0;
}

.settings-desc {
  margin: 0 0 12px;
  font-size: var(--ki-fs-sm);
}

.token-out {
  padding: 10px 12px;
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-status-ok-subtle);
  border: 1px solid var(--ki-status-ok-border);
  color: var(--ki-status-ok-text);
  word-break: break-all;
}

.cmdb-out {
  margin: 0;
  padding: 10px;
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-xs);
  overflow: auto;
}
</style>
