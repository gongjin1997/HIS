<script setup lang="ts">
import { ref } from 'vue'
import { Server, Box, ShieldCheck, AlertTriangle } from 'lucide-vue-next'
import { useWorkspaceStore } from '@/stores/workspace'
import StatCard from '@/components/StatCard.vue'
import {
  KiButton,
  KiInput,
  KiSecretInput,
  KiStatusBadge,
  KiDomainTag,
  KiEnvBadge,
  KiPanel,
  KiMonoText,
  KiBanner,
  KiEmptyState,
} from '@/components/ki'
import {
  KiDangerConfirmModal,
  KiMediaSourceCard,
  KiAuditHashBadge,
  KiProblemBanner,
} from '@/components/domain'

const ws = useWorkspaceStore()
const sampleText = ref('node-kunpeng-01')
const sampleSecret = ref('Kinstall#2026!')
const showDanger = ref(false)
</script>

<template>
  <div class="ki-flex-col">
    <div class="ki-card">
      <div class="ki-card__title">
        <span>kinstall Design System v3.0 组件陈列室 (/__design)<span class="sub">浅色优先企业云运维体系 · 双主题 / 双密度实时切换</span></span>
        <div class="ki-flex-row">
          <KiButton variant="secondary" size="sm" @click="ws.toggleTheme">
            当前主题：{{ ws.theme }}
          </KiButton>
          <KiButton variant="secondary" size="sm" @click="ws.toggleDensity">
            当前密度：{{ ws.density }}
          </KiButton>
        </div>
      </div>

      <div class="ki-grid-4">
        <StatCard title="CMDB 主机" :value="128" unit="台" sub="含 64K 页 16 台" :icon="Server" tone="brand" clickable />
        <StatCard title="在线实例" :value="42" unit="套" sub="12 套插件全覆盖" :icon="Box" tone="ok" clickable />
        <StatCard title="漂移待审" :value="3" unit="单" sub="四眼原则守卫" :icon="AlertTriangle" tone="warn" clickable />
        <StatCard title="审计防篡改" value="100%" sub="SHA-256 + SM3" :icon="ShieldCheck" tone="ok" />
      </div>
    </div>

    <div class="ki-grid-2">
      <KiPanel title="按钮、状态徽标与领域标签">
        <div class="ki-flex-col">
          <div class="ki-flex-row ki-flex-wrap">
            <KiButton variant="primary">主要操作</KiButton>
            <KiButton variant="secondary">次要操作</KiButton>
            <KiButton variant="ghost">幽灵按钮</KiButton>
            <KiButton variant="danger" @click="showDanger = true">高危弹窗演示</KiButton>
            <KiButton variant="primary" loading>加载中</KiButton>
          </div>
          <div class="ki-flex-row ki-flex-wrap">
            <KiStatusBadge status="succeeded" />
            <KiStatusBadge status="running" />
            <KiStatusBadge status="awaiting_approval" />
            <KiStatusBadge status="failed" />
            <KiStatusBadge status="pending" />
          </div>
          <div class="ki-flex-row ki-flex-wrap">
            <KiEnvBadge env="prod" />
            <KiEnvBadge env="staging" />
            <KiEnvBadge env="dev" />
            <KiDomainTag kind="target" value="host-binary" />
            <KiDomainTag kind="target" value="host-container" />
            <KiDomainTag kind="target" value="kubernetes" />
            <KiDomainTag kind="arch" value="arm64" :page-size="65536" />
            <KiDomainTag kind="license" value="Apache-2.0" />
            <KiDomainTag kind="license" value="SSPL-1.0" />
          </div>
          <div class="ki-flex-row">
            <span class="ki-muted">哈希截断复制：</span>
            <KiMonoText value="sha256:4c27a2f190be54217a9c8e3d6f0b11223344556677889900aabbcc9a5fd3" />
          </div>
        </div>
      </KiPanel>

      <KiPanel title="输入控件与加密凭据框">
        <div class="ki-flex-col">
          <KiInput v-model="sampleText" label="主机名 (DNS-1123)" required mono hint="仅允许小写字母、数字与连字符" />
          <KiSecretInput v-model="sampleSecret" label="SSH 密码 / 私钥口令" credential-ref="cred_01J9XK2P3Q7R8S9T" required />
        </div>
      </KiPanel>
    </div>

    <div class="ki-flex-col">
      <KiBanner variant="offline-lock" text="物理离线保护中：所有静态资产均来自本地 go:embed，零外部 CDN。" />
      <KiAuditHashBadge :valid="true" :last-seq="2048" :total-checked="2048" :sm3-enabled="true" latest-hash="sha256:8f4e2a10c9d3b7e5f1a2c3d4e5f60718293a4b5c6d7e8f90" />
      <KiMediaSourceCard
        :source="{ id: 'obs', type: 'obs', name: '华为云 OBS 内网桶', endpoint: 'https://obs.cn-north-4.myhuaweicloud.com/kinstall', enabled: true, probeStatus: 'ok', latencyMs: 11 }"
        :index="0"
        :total="2"
      />
      <KiProblemBanner
        :problem="{ code: 'KI-SRC-0005', title: '物理离线策略拦截外网拉取', detail: 'offlineStrict=true 禁止访问公网在线制品源', suggestion: '请先使用离线同步包导入本地仓库或切换到内网 OBS 桶。', traceId: 'trc_01J9XK2P3Q7R8S9T0U1V' }"
      />
      <KiPanel title="四态空状态示例">
        <KiEmptyState variant="no-result" title="没有符合筛选条件的记录" description="尝试清空关键词或切换其他环境上下文。" />
      </KiPanel>
    </div>

    <KiDangerConfirmModal
      v-model="showDanger"
      env="prod"
      title="确认销毁生产实例"
      resource-name="valkey-prod-01"
      risk-summary="此操作将停止集群并清理节点上的持久化数据目录，不可撤销。"
      confirm-label="确认销毁"
      @confirm="showDanger = false"
    />
  </div>
</template>
