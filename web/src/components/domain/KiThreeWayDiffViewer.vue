<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { EditorState } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { MergeView } from '@codemirror/merge'
import { yaml } from '@codemirror/lang-yaml'
import { AlertTriangle, ShieldCheck, GitCompare } from 'lucide-vue-next'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiButton from '@/components/ki/KiButton.vue'
import KiDangerConfirmModal from './KiDangerConfirmModal.vue'
import { useWorkspaceStore } from '@/stores/workspace'

const props = defineProps<{
  instanceName: string
  path: string
  baseText: string
  liveText: string
  desiredText: string
  hasDrift: boolean
}>()

const emit = defineEmits<{
  (e: 'decision', mode: 'abort' | 'adopt' | 'force-override'): void
}>()

const ws = useWorkspaceStore()
const mergeEl = ref<HTMLElement | null>(null)
const decision = ref<'abort' | 'adopt' | 'force-override'>('abort')
const showDangerConfirm = ref(false)
let mergeView: MergeView | null = null

function mountMergeView() {
  if (!mergeEl.value) return
  mergeView?.destroy()
  mergeEl.value.innerHTML = ''
  const readOnlyExt = [
    yaml(),
    EditorState.readOnly.of(true),
    EditorView.editable.of(false),
    EditorView.lineWrapping,
  ]
  mergeView = new MergeView({
    a: { doc: props.liveText || '# (empty live config)', extensions: readOnlyExt },
    b: { doc: props.desiredText || '# (empty desired config)', extensions: readOnlyExt },
    parent: mergeEl.value,
    highlightChanges: true,
    gutter: true,
    collapseUnchanged: { margin: 3, minSize: 4 },
  })
}

onMounted(mountMergeView)
watch(() => [props.liveText, props.desiredText], mountMergeView)
onUnmounted(() => mergeView?.destroy())

function submitDecision() {
  if (decision.value === 'force-override') {
    showDangerConfirm.value = true
    return
  }
  emit('decision', decision.value)
}

function confirmForceOverride() {
  showDangerConfirm.value = false
  emit('decision', 'force-override')
}
</script>

<template>
  <div class="ki-card ki-diff-wrap">
    <div class="ki-card__title">
      <div class="ki-flex-row">
        <AlertTriangle v-if="hasDrift" :size="16" class="ki-text-warn" aria-hidden="true" />
        <ShieldCheck v-else :size="16" class="ki-text-ok" aria-hidden="true" />
        <span>3-Way Diff 漂移审查</span>
        <span class="ki-mono ki-secondary">{{ path }}</span>
      </div>
      <KiStatusBadge :status="hasDrift ? 'drift' : 'ok'" :label="hasDrift ? '检测到外部手工漂移 (ADR-070)' : '无外部漂移'" />
    </div>

    <!-- Last Applied Base reference -->
    <details class="ki-diff-base">
      <summary class="ki-diff-base__summary">
        <GitCompare :size="13" aria-hidden="true" />
        查看 1. 上次下发基线 (lastApplied)
      </summary>
      <pre class="ki-mono ki-diff-pre">{{ baseText || '# (首次下发无基线)' }}</pre>
    </details>

    <!-- Side-by-side real @codemirror/merge Live vs Desired -->
    <div class="ki-diff-headers">
      <div class="ki-diff-col-head ki-diff-col-head--live">
        <span>− 2. 远端现状 (liveObserved)</span>
      </div>
      <div class="ki-diff-col-head ki-diff-col-head--desired">
        <span>+ 3. 本次期望 (desired)</span>
      </div>
    </div>
    <div ref="mergeEl" class="ki-merge-host" />

    <div v-if="hasDrift" class="ki-diff-actions">
      <div class="section-title">漂移处置策略（默认安全中止）</div>
      <el-radio-group v-model="decision" class="ki-diff-radios">
        <el-radio value="abort">中止下发并人工核对（默认安全）</el-radio>
        <el-radio value="adopt">采纳远端手工改动 (--adopt)</el-radio>
        <el-radio value="force-override">强行覆盖远端 (--force-override · 触发四眼审批)</el-radio>
      </el-radio-group>
      <div class="ki-mt-3">
        <KiButton :variant="decision === 'force-override' ? 'danger' : 'primary'" @click="submitDecision">
          确认漂移决策
        </KiButton>
      </div>
    </div>

    <KiDangerConfirmModal
      v-model="showDangerConfirm"
      :env="ws.currentEnv"
      title="确认强行覆盖远端配置漂移"
      :resource-name="instanceName"
      :risk-summary="`即将使用 --force-override 覆盖 ${path} 上的手工修改，并在生产环境触发四眼审批单。`"
      confirm-label="解锁并提交覆盖"
      @confirm="confirmForceOverride"
    />
  </div>
</template>

<style scoped>
.ki-diff-base {
  margin-bottom: var(--ki-space-3);
  border: 1px solid var(--ki-border-subtle);
  border-radius: var(--ki-radius-sm);
  overflow: hidden;
}

.ki-diff-base__summary {
  display: flex;
  align-items: center;
  gap: var(--ki-space-2);
  padding: var(--ki-space-2) var(--ki-space-3);
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-medium);
  cursor: pointer;
}

.ki-diff-pre {
  margin: 0;
  padding: var(--ki-space-3);
  background-color: var(--ki-bg-terminal);
  color: var(--ki-term-fg);
  overflow: auto;
  max-height: 180px;
}

.ki-diff-headers {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  border: 1px solid var(--ki-border-default);
  border-bottom: 0;
  border-radius: var(--ki-radius-sm) var(--ki-radius-sm) 0 0;
  overflow: hidden;
}

.ki-diff-col-head {
  padding: 6px 12px;
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-semibold);
}

.ki-diff-col-head--live {
  background-color: var(--ki-status-warn-subtle);
  color: var(--ki-status-warn-text);
  border-right: 1px solid var(--ki-border-default);
}

.ki-diff-col-head--desired {
  background-color: var(--ki-brand-subtle);
  color: var(--ki-brand);
}

.ki-merge-host {
  border: 1px solid var(--ki-border-default);
  border-radius: 0 0 var(--ki-radius-sm) var(--ki-radius-sm);
  overflow: hidden;
  max-height: 360px;
  overflow-y: auto;
  background-color: var(--ki-bg-surface);
}

.ki-diff-actions {
  margin-top: var(--ki-space-4);
  padding-top: var(--ki-space-3);
  border-top: 1px solid var(--ki-border-subtle);
}

.ki-diff-radios {
  display: flex;
  flex-wrap: wrap;
  gap: var(--ki-space-3);
}
</style>
