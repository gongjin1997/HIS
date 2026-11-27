<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Download, FileSpreadsheet, KeyRound, Settings2, Upload } from 'lucide-vue-next'
import { apiDownload, apiRequest } from '@/api/client'
import KiDrawer from '@/components/ki/KiDrawer.vue'
import KiBanner from '@/components/ki/KiBanner.vue'

interface ImportRow {
  rowNumber: number
  name: string
  address: string
  port: number
  user: string
  biz?: string
  set?: string
  module?: string
  region?: string
  securityArea?: string
  zone?: string
  rack?: string
  arch?: string
  pageSize?: number
}

interface ImportRowError {
  rowNumber: number
  field: string
  code: string
  message: string
}

const props = defineProps<{
  modelValue: boolean
  projectId: string
  credentials: Array<{ id: string; name: string; kind: string }>
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'imported'): void
}>()

const open = computed({
  get: () => props.modelValue,
  set: (v: boolean) => emit('update:modelValue', v),
})

const state = reactive({
  format: 'csv' as 'csv' | 'xlsx' | 'yaml' | 'json' | 'ansible',
  data: '',
  fileName: '',
  defaultCredentialId: '',
  createInlineCred: false,
  inlineCredName: 'cmdb-batch-ssh',
  inlineCredKind: 'ssh_password' as 'ssh_password' | 'ssh_key',
  inlineCredSecret: '',
  defaultBiz: '',
  defaultSet: '',
  defaultModule: '',
  defaultRegion: '',
  defaultSecurityArea: '',
  defaultZone: '',
  updateExisting: true,
  sanitizeHostname: true,
  showMapping: false,
  columnMap: {} as Record<string, string>,
  detectedHeaders: [] as string[],
  busy: false,
  previewRows: [] as ImportRow[],
  previewErrors: [] as ImportRowError[],
  checked: false,
})

const downloadingTpl = ref(false)

const canonicalFields = [
  { label: '自动识别（不覆盖）', value: '' },
  { label: '主机名 (name)', value: 'name' },
  { label: '内网 IP (address)', value: 'address' },
  { label: 'SSH 端口 (port)', value: 'port' },
  { label: 'SSH 用户 (user)', value: 'user' },
  { label: '业务系统 (biz)', value: 'biz' },
  { label: '集群环境 (set)', value: 'set' },
  { label: '业务模块 (module)', value: 'module' },
  { label: '区域机房 (region)', value: 'region' },
  { label: '安全域 (security_area)', value: 'security_area' },
  { label: '故障域 (zone)', value: 'zone' },
  { label: '机架 (rack)', value: 'rack' },
  { label: 'CPU 架构 (arch)', value: 'arch' },
  { label: '页大小 (page_size)', value: 'page_size' },
  { label: '凭据名 (credential)', value: 'credential' },
  { label: '负责人 (owner)', value: 'owner' },
  { label: '资产编号 (asset_tag)', value: 'asset_tag' },
  { label: '标签 (labels)', value: 'labels' },
]

async function downloadTemplate(fmt: 'xlsx' | 'csv') {
  if (!props.projectId) return
  downloadingTpl.value = true
  try {
    const res = await apiDownload(
      'GET',
      `/api/v1/projects/${props.projectId}/hosts:template?format=${fmt}`,
      `kinstall-cmdb-hosts-template.${fmt}`
    )
    ElMessage.success(`已下载模板 ${res.filename}`)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    downloadingTpl.value = false
  }
}

function fillSample() {
  state.checked = false
  state.previewErrors = []
  state.previewRows = []
  if (state.format === 'json') {
    state.fileName = 'sample-liveops-cmdb.json'
    state.data = JSON.stringify(
      {
        data: [
          {
            host_name: 'his-outpatient-01',
            host_innerip: '10.20.3.11',
            ssh_port: 22,
            biz: 'HIS核心门诊',
            set: 'prod-交易集群',
            group_name: '应用网关',
            region: '北京主中心',
            security_area: '核心内网',
            zone: 'az-1',
            arch: 'arm64',
            page_size: 65536,
          },
          {
            host_name: 'his-emr-db-01',
            host_innerip: '10.20.3.12',
            ssh_port: 22,
            biz: '电子病历EMR',
            set: 'prod-交易集群',
            group_name: '关系数据库',
            region: '北京主中心',
            security_area: '核心内网',
            zone: 'az-2',
            arch: 'amd64',
          },
        ],
      },
      null,
      2
    )
    return
  }
  if (state.format === 'ansible') {
    state.fileName = 'sample-hosts.ini'
    state.data = `[his-db]
his-pg-01 ansible_host=10.20.4.11 ansible_port=22 ansible_user=root biz=HIS核心门诊 set=prod zone=az-1 arch=arm64
his-pg-02 ansible_host=10.20.4.12 ansible_port=22 ansible_user=root biz=HIS核心门诊 set=prod zone=az-2 arch=arm64

[his-cache]
his-valkey-01 ansible_host=10.20.4.21 ansible_user=root biz=HIS核心门诊 set=prod zone=az-1
`
    return
  }
  if (state.format === 'yaml') {
    state.fileName = 'sample-hosts.yaml'
    state.data = `- name: node-app-01
  address: 10.20.1.21
  port: 22
  user: root
  biz: HIS核心门诊
  set: prod-交易集群
  module: 应用网关
  region: 北京主中心
  security_area: 核心内网
  zone: az-1
  rack: rack-a01
`
    return
  }
  state.format = 'csv'
  state.fileName = 'sample-hosts.csv'
  state.data =
    '主机名,内网IP,SSH端口,SSH用户,业务系统,集群环境,业务模块,区域机房,安全域,故障域,机架,CPU架构,页大小\n' +
    'node-app-01,10.20.1.21,22,root,HIS核心门诊,prod-交易集群,应用网关,北京主中心,核心内网,az-1,rack-a01,amd64,4096\n' +
    'node-app-02,10.20.1.22,22,root,HIS核心门诊,prod-交易集群,关系数据库,北京主中心,核心内网,az-2,rack-b02,arm64,65536\n'
}

function onImportFileSelected(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  state.fileName = file.name
  state.checked = false
  const low = file.name.toLowerCase()
  if (low.endsWith('.xlsx')) {
    state.format = 'xlsx'
    const reader = new FileReader()
    reader.onload = () => {
      const res = String(reader.result || '')
      state.data = res.includes(',') ? res.split(',')[1] : res
    }
    reader.readAsDataURL(file)
  } else {
    if (low.endsWith('.yaml') || low.endsWith('.yml')) state.format = 'yaml'
    else if (low.endsWith('.json')) state.format = 'json'
    else if (low.endsWith('.ini') || low.endsWith('.hosts')) state.format = 'ansible'
    else state.format = 'csv'
    const reader = new FileReader()
    reader.onload = () => {
      state.data = String(reader.result || '')
    }
    reader.readAsText(file)
  }
  input.value = ''
}

async function runBulkImport(dryRun: boolean) {
  if (!state.data.trim()) {
    ElMessage.warning('请上传文件、粘贴主机清单，或点击「填入示例」')
    return
  }
  const cleanMap: Record<string, string> = {}
  for (const [k, v] of Object.entries(state.columnMap)) {
    if (v) cleanMap[k] = v
  }
  const body: Record<string, unknown> = {
    format: state.format,
    data: state.data,
    dryRun,
    columnMap: cleanMap,
    defaultCredentialId: state.defaultCredentialId,
    defaultBiz: state.defaultBiz,
    defaultSet: state.defaultSet,
    defaultModule: state.defaultModule,
    defaultRegion: state.defaultRegion,
    defaultSecurityArea: state.defaultSecurityArea,
    defaultZone: state.defaultZone,
    updateExisting: state.updateExisting,
    sanitizeHostname: state.sanitizeHostname,
  }
  if (state.createInlineCred && state.inlineCredSecret.trim()) {
    body.inlineCredential = {
      name: state.inlineCredName.trim() || 'cmdb-batch-ssh',
      kind: state.inlineCredKind,
      plaintext: state.inlineCredSecret,
    }
  }
  state.busy = true
  try {
    const { data } = await apiRequest<{
      result: { rows?: ImportRow[]; errors?: ImportRowError[]; detectedHeaders?: string[] }
      imported: number
      updated?: number
      dryRun: boolean
    }>('POST', `/api/v1/projects/${props.projectId}/hosts:import`, {
      body,
      idempotent: !dryRun,
    })
    state.previewRows = data.result?.rows || []
    state.previewErrors = data.result?.errors || []
    state.detectedHeaders = data.result?.detectedHeaders || []
    state.checked = true
    if (dryRun) {
      if (state.previewErrors.length === 0) {
        ElMessage.success(`Dry-Run 预检通过：识别 ${state.previewRows.length} 台主机，0 行错误`)
      } else {
        ElMessage.warning(`Dry-Run 检测到 ${state.previewErrors.length} 行错误，请按行号修正`)
      }
    } else {
      const total = (data.imported || 0) + (data.updated || 0)
      if (total > 0) {
        ElMessage.success(`入库完成：新增 ${data.imported || 0} 台，更新 ${data.updated || 0} 台`)
        emit('imported')
        if (state.previewErrors.length === 0) {
          open.value = false
          state.inlineCredSecret = ''
        }
      } else {
        ElMessage.warning('未导入任何主机，请检查下方错误行')
      }
    }
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    state.busy = false
  }
}
</script>

<template>
  <KiDrawer
    v-model="open"
    title="CMDB 主机批量导入"
    subtitle="支持中英文 Excel (.xlsx) / CSV / YAML / LiveOps·HCS JSON / Ansible INI，一键下载标准模板并逐行 Dry-Run 预检"
    size="lg"
  >
    <!-- 模板下载条 -->
    <div class="tpl-bar ki-mb-3">
      <div class="tpl-bar__info">
        <FileSpreadsheet :size="16" />
        <span>首次导入？先下载含字段说明与示例行的标准模板（支持中文表头自动识别）：</span>
      </div>
      <div class="ki-flex-row">
        <el-button size="small" :loading="downloadingTpl" @click="downloadTemplate('xlsx')">
          <Download :size="13" class="btn-ico" /> 下载 Excel 模板 (.xlsx)
        </el-button>
        <el-button size="small" :loading="downloadingTpl" @click="downloadTemplate('csv')">
          <Download :size="13" class="btn-ico" /> 下载 CSV 模板
        </el-button>
      </div>
    </div>

    <!-- 格式与文件上传 -->
    <div class="ki-flex-between ki-flex-wrap ki-mb-3">
      <el-radio-group v-model="state.format" size="small">
        <el-radio-button value="csv">CSV (中英文)</el-radio-button>
        <el-radio-button value="xlsx">Excel (.xlsx)</el-radio-button>
        <el-radio-button value="json">LiveOps / HCS JSON</el-radio-button>
        <el-radio-button value="ansible">Ansible INI</el-radio-button>
        <el-radio-button value="yaml">YAML</el-radio-button>
      </el-radio-group>
      <div class="ki-flex-row">
        <el-button size="small" @click="fillSample">填入示例 CSV</el-button>
        <label class="file-upload-btn">
          <Upload :size="13" /> 上传文件
          <input
            type="file"
            accept=".xlsx,.csv,.yaml,.yml,.json,.ini,.hosts"
            class="hidden-file"
            @change="onImportFileSelected"
          />
        </label>
      </div>
    </div>

    <div v-if="state.fileName" class="ki-muted ki-mono ki-mb-3">已加载文件：{{ state.fileName }}</div>
    <el-input
      v-model="state.data"
      type="textarea"
      :rows="6"
      class="ki-mono ki-mb-3"
      placeholder="支持中英文列头，如：主机名,内网IP,SSH端口,业务系统,集群环境,业务模块,区域机房,安全域,故障域"
    />

    <!-- 默认凭据与快捷建凭据 -->
    <div class="sub-panel ki-mb-3">
      <div class="sub-panel__head">
        <span class="sub-panel__title"><KeyRound :size="14" /> 默认 SSH 凭据与覆盖策略</span>
        <div class="ki-flex-row">
          <el-checkbox v-model="state.updateExisting">同名主机自动更新 (Upsert)</el-checkbox>
          <el-checkbox v-model="state.sanitizeHostname">主机名自动规范 (DNS-1123)</el-checkbox>
        </div>
      </div>

      <div class="ki-grid-2 ki-mb-3">
        <div>
          <div class="field-lbl">批量绑定已有凭据（文件内未指定凭据列时生效）</div>
          <el-select
            v-model="state.defaultCredentialId"
            clearable
            :disabled="state.createInlineCred"
            class="ki-w-full"
            placeholder="默认选用项目首个凭据"
          >
            <el-option v-for="c in credentials" :key="c.id" :label="`${c.name} (${c.kind})`" :value="c.id" />
          </el-select>
        </div>
        <div class="inline-cred-toggle">
          <el-switch v-model="state.createInlineCred" />
          <span>导入时同步在保险库新建 SSH 凭据</span>
        </div>
      </div>

      <div v-if="state.createInlineCred" class="ki-grid-3">
        <el-input v-model="state.inlineCredName" size="small" placeholder="新凭据名称，如 his-root-pwd" class="ki-mono" />
        <el-select v-model="state.inlineCredKind" size="small" class="ki-w-full">
          <el-option label="SSH 密码 (ssh_password)" value="ssh_password" />
          <el-option label="SSH 私钥 (ssh_key)" value="ssh_key" />
        </el-select>
        <el-input
          v-model="state.inlineCredSecret"
          size="small"
          type="password"
          show-password
          placeholder="输入 SSH 密码或私钥内容"
        />
      </div>
    </div>

    <!-- 默认 CMDB 拓扑归属 -->
    <div class="sub-panel ki-mb-3">
      <div class="sub-panel__head">
        <span class="sub-panel__title">默认 CMDB 拓扑归属（导入表未填写时自动补齐）</span>
        <el-button link size="small" @click="state.showMapping = !state.showMapping">
          <Settings2 :size="13" class="btn-ico" />
          {{ state.showMapping ? '收起自定义列映射' : '自定义表头列映射' }}
        </el-button>
      </div>
      <div class="ki-grid-3">
        <el-input v-model="state.defaultBiz" size="small" placeholder="业务系统，如 HIS核心门诊" />
        <el-input v-model="state.defaultSet" size="small" placeholder="集群环境，如 prod-交易集群" />
        <el-input v-model="state.defaultModule" size="small" placeholder="业务模块，如 关系数据库" />
        <el-input v-model="state.defaultRegion" size="small" placeholder="区域机房，如 北京主中心" />
        <el-input v-model="state.defaultSecurityArea" size="small" placeholder="安全域，如 核心内网" />
        <el-input v-model="state.defaultZone" size="small" placeholder="默认故障域，如 az-1" />
      </div>

      <div v-if="state.showMapping" class="mapping-box ki-mt-3">
        <div class="field-lbl">
          {{ state.detectedHeaders.length > 0 ? '已识别的表头列（可覆盖默认映射后重新 Dry-Run）：' : '先点击下方「预检 Dry-Run」自动识别文件表头列。' }}
        </div>
        <div v-if="state.detectedHeaders.length > 0" class="ki-grid-3">
          <div v-for="hdr in state.detectedHeaders" :key="hdr" class="map-row">
            <span class="ki-mono map-hdr" :title="hdr">{{ hdr }}</span>
            <el-select v-model="state.columnMap[hdr]" size="small" clearable placeholder="自动识别">
              <el-option v-for="cf in canonicalFields" :key="cf.value" :label="cf.label" :value="cf.value" />
            </el-select>
          </div>
        </div>
      </div>
    </div>

    <!-- 预检结果 -->
    <KiBanner
      v-if="state.previewErrors.length > 0"
      variant="danger"
      :text="`发现 ${state.previewErrors.length} 处字段或冲突校验错误，请根据下方行号修正后重试`"
      class="ki-mb-3"
    />
    <el-table v-if="state.previewErrors.length > 0" :data="state.previewErrors" size="small" class="ki-mb-3">
      <el-table-column prop="rowNumber" label="行号" width="65" />
      <el-table-column prop="field" label="字段" width="96" />
      <el-table-column prop="code" label="错误码" width="136">
        <template #default="{ row }"><span class="ki-mono">{{ row.code }}</span></template>
      </el-table-column>
      <el-table-column prop="message" label="错误说明" />
    </el-table>

    <div v-if="state.previewRows.length > 0">
      <div class="section-title">解析预览<span class="sub">共识别 {{ state.previewRows.length }} 台有效主机</span></div>
      <el-table :data="state.previewRows" size="small" max-height="240">
        <el-table-column prop="rowNumber" label="#" width="52" />
        <el-table-column prop="name" label="主机名" min-width="120">
          <template #default="{ row }"><b class="ki-mono">{{ row.name }}</b></template>
        </el-table-column>
        <el-table-column label="SSH 端点" min-width="145">
          <template #default="{ row }"><span class="ki-mono">{{ row.user }}@{{ row.address }}:{{ row.port }}</span></template>
        </el-table-column>
        <el-table-column label="业务拓扑" min-width="160">
          <template #default="{ row }">{{ [row.biz, row.set, row.module].filter(Boolean).join(' / ') || '—' }}</template>
        </el-table-column>
        <el-table-column label="区域 / 故障域" min-width="130">
          <template #default="{ row }">{{ [row.region, row.securityArea, row.zone].filter(Boolean).join(' · ') || '—' }}</template>
        </el-table-column>
      </el-table>
    </div>

    <template #footer>
      <el-button @click="open = false">关闭</el-button>
      <el-button :loading="state.busy" @click="runBulkImport(true)">预检 Dry-Run</el-button>
      <el-button type="primary" :loading="state.busy" @click="runBulkImport(false)">确认批量入库</el-button>
    </template>
  </KiDrawer>
</template>

<style scoped>
.tpl-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: var(--ki-space-2);
  padding: 10px 12px;
  border-radius: var(--ki-radius-sm);
  border: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-subtle);
}

.tpl-bar__info {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-secondary);
}

.sub-panel {
  padding: 12px;
  border-radius: var(--ki-radius-sm);
  border: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-surface);
}

.sub-panel__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 10px;
}

.sub-panel__title {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.field-lbl {
  margin-bottom: 6px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.inline-cred-toggle {
  display: flex;
  align-items: flex-end;
  gap: 8px;
  padding-bottom: 4px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-secondary);
}

.mapping-box {
  padding-top: 10px;
  border-top: 1px dashed var(--ki-border-subtle);
}

.map-row {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.map-hdr {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.file-upload-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 5px 10px;
  border-radius: var(--ki-radius-sm);
  border: 1px solid var(--ki-border-control);
  background-color: var(--ki-bg-surface);
  color: var(--ki-text-primary);
  font-size: var(--ki-fs-xs);
  cursor: pointer;
}

.hidden-file {
  display: none;
}

.btn-ico {
  margin-right: 4px;
}
</style>
