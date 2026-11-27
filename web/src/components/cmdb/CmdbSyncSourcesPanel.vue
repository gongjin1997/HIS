<script setup lang="ts">
import { reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Eye, Plus, RefreshCw, Trash2 } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'

export interface CMDBSyncSourceItem {
  id: string
  name: string
  kind: 'liveops_json' | 'ansible_ini' | 'huawei_hcs' | 'http_json' | 'http_csv'
  endpoint: string
  credentialId?: string
  payloadText?: string
  lastSyncAt?: string
  lastStatus: string
  lastSummary: string
}

const props = defineProps<{
  projectId: string
  canWrite: boolean
  sources: CMDBSyncSourceItem[]
  credentials: Array<{ id: string; name: string; kind: string }>
}>()

const emit = defineEmits<{
  (e: 'refresh'): void
}>()

const runningId = ref('')
const previewResult = ref<{ sourceName: string; rows: Array<Record<string, unknown>>; errors: Array<Record<string, unknown>> } | null>(null)

const dialog = reactive({
  open: false,
  saving: false,
  name: '',
  kind: 'liveops_json' as CMDBSyncSourceItem['kind'],
  endpoint: '',
  credentialId: '',
  authHeader: '',
  defaultBiz: 'HIS核心系统',
  defaultSet: 'prod',
  defaultModule: '',
  defaultRegion: '',
  defaultSecurityArea: '核心内网',
  payloadText: '',
})

const kindLabels: Record<string, string> = {
  liveops_json: 'LiveOps CMDB JSON',
  ansible_ini: 'Ansible Inventory INI',
  huawei_hcs: '华为云 HCS / ECS 快照',
  http_json: '外部 HTTP JSON API',
  http_csv: '外部 HTTP CSV URL',
}

function fillPresetPayload() {
  if (dialog.kind === 'ansible_ini') {
    dialog.payloadText = `[his-core-app]
his-app-sync-01 ansible_host=10.30.1.11 ansible_port=22 ansible_user=root zone=az-1
his-app-sync-02 ansible_host=10.30.1.12 ansible_port=22 ansible_user=root zone=az-2`
  } else if (dialog.kind === 'huawei_hcs') {
    dialog.payloadText = JSON.stringify(
      {
        servers: [
          {
            name: 'hcs-his-kunpeng-01',
            'OS-EXT-AZ:availability_zone': 'az1.dc1',
            addresses: { 'vpc-his': [{ addr: '10.30.2.21' }] },
            metadata: { arch: 'arm64', module: '核心数据库' },
          },
        ],
      },
      null,
      2
    )
  } else {
    dialog.payloadText = JSON.stringify(
      {
        data: [
          {
            host_name: 'liveops-his-01',
            host_innerip: '10.30.3.11',
            group_name: '门诊微服务',
            region: '北京主中心',
            zone: 'az-1',
            arch: 'amd64',
          },
        ],
      },
      null,
      2
    )
  }
}

async function createSource() {
  if (!dialog.name.trim()) {
    ElMessage.warning('请输入同步源名称')
    return
  }
  if (!dialog.endpoint.trim() && !dialog.payloadText.trim()) {
    ElMessage.warning('请填写外部拉取 URL，或粘贴离线资产快照数据')
    return
  }
  dialog.saving = true
  try {
    await apiRequest('POST', `/api/v1/projects/${props.projectId}/cmdb/sync-sources`, {
      body: {
        name: dialog.name.trim(),
        kind: dialog.kind,
        endpoint: dialog.endpoint.trim(),
        credentialId: dialog.credentialId || undefined,
        payloadText: dialog.payloadText,
        mapping: {
          defaultBiz: dialog.defaultBiz,
          defaultSet: dialog.defaultSet,
          defaultModule: dialog.defaultModule,
          defaultRegion: dialog.defaultRegion,
          defaultSecurityArea: dialog.defaultSecurityArea,
          authHeader: dialog.authHeader,
          updateExisting: true,
        },
      },
      idempotent: true,
    })
    ElMessage.success('外部 CMDB 同步源已创建')
    dialog.open = false
    dialog.name = ''
    dialog.endpoint = ''
    dialog.payloadText = ''
    emit('refresh')
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    dialog.saving = false
  }
}

async function runSync(src: CMDBSyncSourceItem, dryRun: boolean) {
  runningId.value = src.id
  try {
    const { data } = await apiRequest<{
      imported: number
      updated: number
      result: { rows?: Array<Record<string, unknown>>; errors?: Array<Record<string, unknown>> }
    }>('POST', `/api/v1/projects/${props.projectId}/cmdb/sync-sources/${src.id}:run`, {
      body: { dryRun },
    })
    if (dryRun) {
      previewResult.value = {
        sourceName: src.name,
        rows: data.result?.rows || [],
        errors: data.result?.errors || [],
      }
      ElMessage.success(`Dry-Run 预览：识别 ${previewResult.value.rows.length} 台主机，${previewResult.value.errors.length} 处异常`)
    } else {
      previewResult.value = null
      ElMessage.success(`同步完成：新增 ${data.imported} 台，更新 ${data.updated} 台`)
      emit('refresh')
    }
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    runningId.value = ''
  }
}

async function deleteSource(src: CMDBSyncSourceItem) {
  try {
    await apiRequest('DELETE', `/api/v1/projects/${props.projectId}/cmdb/sync-sources/${src.id}`)
    ElMessage.success(`已移除同步源 ${src.name}`)
    emit('refresh')
  } catch (e) {
    ElMessage.error(String(e))
  }
}
</script>

<template>
  <div>
    <div class="ki-flex-between ki-mb-3">
      <span class="ki-secondary">支持对接 LiveOps 导出 JSON、Ansible Inventory、华为云 HCS/ECS 与外部 HTTP JSON/CSV CMDB 接口，按主机名自动增量 Upsert。</span>
      <el-button v-if="canWrite" type="primary" size="small" @click="dialog.open = true">
        <Plus :size="13" class="btn-ico" /> 添加同步源
      </el-button>
    </div>

    <el-table v-if="sources.length > 0" :data="sources" size="small" class="ki-mb-3">
      <el-table-column prop="name" label="同步源名称" min-width="140">
        <template #default="{ row }"><b>{{ row.name }}</b></template>
      </el-table-column>
      <el-table-column label="数据源类型" width="168">
        <template #default="{ row }"><span class="ki-badge ki-mono">{{ kindLabels[row.kind] || row.kind }}</span></template>
      </el-table-column>
      <el-table-column label="拉取端点 / 离线模式" min-width="180">
        <template #default="{ row }">
          <span class="ki-mono ki-secondary">{{ row.endpoint || '离线快照模式 (Payload)' }}</span>
        </template>
      </el-table-column>
      <el-table-column label="最近状态" width="96">
        <template #default="{ row }"><KiStatusBadge :status="row.lastStatus || 'pending'" /></template>
      </el-table-column>
      <el-table-column prop="lastSummary" label="同步摘要" min-width="160">
        <template #default="{ row }"><span class="ki-secondary">{{ row.lastSummary || '尚未执行' }}</span></template>
      </el-table-column>
      <el-table-column label="操作" width="210" fixed="right">
        <template #default="{ row }">
          <div class="ki-row-actions">
            <el-button v-if="canWrite" link type="primary" :loading="runningId === row.id" @click="runSync(row, true)">
              <Eye :size="13" class="btn-ico" /> 预检
            </el-button>
            <el-button v-if="canWrite" link type="primary" :loading="runningId === row.id" @click="runSync(row, false)">
              <RefreshCw :size="13" class="btn-ico" /> 立即同步
            </el-button>
            <el-button v-if="canWrite" link type="danger" @click="deleteSource(row)">
              <Trash2 :size="13" />
            </el-button>
          </div>
        </template>
      </el-table-column>
    </el-table>

    <div v-else class="ki-empty-state ki-mb-3">
      <div class="ki-empty-state__title">尚未配置外部 CMDB 同步源</div>
      <p class="ki-empty-state__desc">可将现有 LiveOps CMDB、Ansible hosts 清单、华为云 HCS 服务器列表或内网 CMDB HTTP API 接入自动同步。</p>
      <el-button v-if="canWrite" type="primary" size="small" @click="dialog.open = true">
        <Plus :size="13" class="btn-ico" /> 配置首个同步源
      </el-button>
    </div>

    <div v-if="previewResult" class="preview-box">
      <div class="section-title">
        同步预检预览 · {{ previewResult.sourceName }}
        <span class="sub">识别 {{ previewResult.rows.length }} 台 / 异常 {{ previewResult.errors.length }} 行</span>
      </div>
      <el-table :data="previewResult.rows" size="small" max-height="200">
        <el-table-column prop="name" label="主机名" min-width="140">
          <template #default="{ row }"><span class="ki-mono">{{ row.name }}</span></template>
        </el-table-column>
        <el-table-column prop="address" label="IP" width="130">
          <template #default="{ row }"><span class="ki-mono">{{ row.address }}</span></template>
        </el-table-column>
        <el-table-column label="归属拓扑" min-width="180">
          <template #default="{ row }">{{ [row.biz, row.set, row.module].filter(Boolean).join(' / ') || '—' }}</template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="dialog.open" title="配置外部 CMDB 同步源" width="660px">
      <el-form label-position="top">
        <div class="ki-grid-2">
          <el-form-item label="同步源名称">
            <el-input v-model="dialog.name" placeholder="如：院内 LiveOps 资产同步" />
          </el-form-item>
          <el-form-item label="数据源协议 / 格式">
            <el-select v-model="dialog.kind" class="ki-w-full">
              <el-option v-for="(label, k) in kindLabels" :key="k" :label="label" :value="k" />
            </el-select>
          </el-form-item>
        </div>

        <div class="ki-grid-2">
          <el-form-item label="外部拉取 HTTP URL（在线同步时填写）">
            <el-input v-model="dialog.endpoint" class="ki-mono" placeholder="http://cmdb.internal/api/hosts" />
          </el-form-item>
          <el-form-item label="默认绑定 SSH 凭据">
            <el-select v-model="dialog.credentialId" clearable class="ki-w-full" placeholder="留空默认项目首个凭据">
              <el-option v-for="c in credentials" :key="c.id" :label="`${c.name} (${c.kind})`" :value="c.id" />
            </el-select>
          </el-form-item>
        </div>

        <div class="ki-grid-3">
          <el-form-item label="默认业务系统"><el-input v-model="dialog.defaultBiz" /></el-form-item>
          <el-form-item label="默认集群环境"><el-input v-model="dialog.defaultSet" /></el-form-item>
          <el-form-item label="默认安全域"><el-input v-model="dialog.defaultSecurityArea" /></el-form-item>
        </div>

        <el-form-item>
          <template #label>
            <div class="ki-flex-between ki-w-full">
              <span>离线资产快照 Payload（离线环境可直接粘贴，优先于 URL）</span>
              <el-button link size="small" type="primary" @click="fillPresetPayload">填入格式示例</el-button>
            </div>
          </template>
          <el-input v-model="dialog.payloadText" type="textarea" :rows="6" class="ki-mono" placeholder="粘贴 LiveOps JSON / Ansible INI / HCS JSON 快照" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog.open = false">取消</el-button>
        <el-button type="primary" :loading="dialog.saving" @click="createSource">保存同步源</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.preview-box {
  padding: 12px;
  border-radius: var(--ki-radius-sm);
  border: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-subtle);
}

.btn-ico {
  margin-right: 4px;
}
</style>
