<script setup lang="ts">
import { ref, computed } from 'vue'
import { AlertTriangle, ShieldCheck } from 'lucide-vue-next'

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

const decision = ref<'abort' | 'adopt' | 'force-override'>('abort')
const confirmName = ref('')

const canSubmit = computed(() => {
  if (decision.value === 'force-override') {
    return confirmName.value.trim() === props.instanceName
  }
  return true
})
</script>

<template>
  <div class="ki-panel">
    <div class="ki-flex-between" style="margin-bottom: 12px">
      <div class="ki-flex-row">
        <AlertTriangle v-if="hasDrift" :size="16" style="color: var(--ki-status-warn)" />
        <ShieldCheck v-else :size="16" style="color: var(--ki-status-ok)" />
        <span style="font-weight: 600">3-Way Diff 审查</span>
        <span class="ki-mono ki-secondary">{{ path }}</span>
      </div>
      <span
        class="ki-badge"
        :style="hasDrift
          ? { background: 'var(--ki-status-warn-subtle)', color: 'var(--ki-status-warn-text)' }
          : { background: 'var(--ki-status-ok-subtle)', color: 'var(--ki-status-ok-text)' }"
      >
        {{ hasDrift ? '检测到外部手工漂移 (ADR-070)' : '无外部漂移' }}
      </span>
    </div>

    <div style="display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px">
      <div class="ki-surface" style="overflow: hidden">
        <div style="padding: 6px 10px; background: var(--ki-bg-subtle); font-size: 11px; font-weight: 600; color: var(--ki-text-secondary)">
          1. 上次下发基线 (lastApplied)
        </div>
        <pre class="ki-mono" style="margin: 0; padding: 10px; background: var(--ki-bg-terminal); color: var(--ki-term-fg); overflow: auto; max-height: 240px">{{ baseText }}</pre>
      </div>
      <div class="ki-surface" style="overflow: hidden">
        <div style="padding: 6px 10px; background: var(--ki-status-warn-subtle); font-size: 11px; font-weight: 600; color: var(--ki-status-warn-text)">
          2. 远端现状 (liveObserved)
        </div>
        <pre class="ki-mono" style="margin: 0; padding: 10px; background: var(--ki-bg-terminal); color: var(--ki-term-fg); overflow: auto; max-height: 240px">{{ liveText }}</pre>
      </div>
      <div class="ki-surface" style="overflow: hidden">
        <div style="padding: 6px 10px; background: var(--ki-brand-subtle); font-size: 11px; font-weight: 600; color: var(--ki-brand-hover)">
          3. 本次期望 (desired)
        </div>
        <pre class="ki-mono" style="margin: 0; padding: 10px; background: var(--ki-bg-terminal); color: var(--ki-term-fg); overflow: auto; max-height: 240px">{{ desiredText }}</pre>
      </div>
    </div>

    <div v-if="hasDrift" style="margin-top: 14px; padding-top: 12px; border-top: 1px solid var(--ki-border-subtle)">
      <el-radio-group v-model="decision">
        <el-radio value="abort">中止并人工核对（默认安全）</el-radio>
        <el-radio value="adopt">采纳远端改动 (--adopt)</el-radio>
        <el-radio value="force-override">强行覆盖远端 (--force-override · 触发四眼审批)</el-radio>
      </el-radio-group>

      <div v-if="decision === 'force-override'" style="margin-top: 10px; max-width: 360px">
        <div style="font-size: 12px; color: var(--ki-status-danger-text); margin-bottom: 4px">
          高危确认：请输入实例名 <strong class="ki-mono">{{ instanceName }}</strong> 以解锁强制覆盖
        </div>
        <el-input v-model="confirmName" :placeholder="instanceName" class="ki-mono" />
      </div>

      <div style="margin-top: 12px">
        <el-button
          :type="decision === 'force-override' ? 'danger' : 'primary'"
          :disabled="!canSubmit"
          @click="emit('decision', decision)"
        >
          确认漂移决策
        </el-button>
      </div>
    </div>
  </div>
</template>
