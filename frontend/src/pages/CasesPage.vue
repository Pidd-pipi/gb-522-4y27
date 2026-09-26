<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CalendarClock, CircleCheck, CircleX, ClipboardCheck, GitCompareArrows, Plus, Play, LockKeyhole, Ruler, Timer, Waves } from 'lucide-vue-next'
import PageHeader from '@/components/common/PageHeader.vue'
import ReviewDialog from '@/components/common/ReviewDialog.vue'
import { useCaseStore } from '@/stores/cases'
import { useRouteStore } from '@/stores/routes'
import { useTraceStore } from '@/stores/traces'
import { useAuth } from '@/hooks/useAuth'
import { CASE_STATUSES, caseStatusLabel, type CaseCompatibility, type CompatibilityField, type LocalizationCase } from '@/types/case'

const cases = useCaseStore(); const routes = useRouteStore(); const traces = useTraceStore(); const auth = useAuth(); const status = ref(''); const createOpen = ref(false); const reviewOpen = ref(false); const current = ref<LocalizationCase | null>(null); const busy = ref(false)
const form = reactive({ route_id: 0, baseline_trace_id: 0, current_trace_id: 0, distance_tolerance_m: 25, loss_increase_db: 0.5 })
const routeTraces = computed(() => traces.items.filter((item) => item.route_id === form.route_id))
const pairSelected = computed(() => form.route_id > 0 && form.baseline_trace_id > 0 && form.current_trace_id > 0 && form.baseline_trace_id !== form.current_trace_id)
const compatibility = ref<CaseCompatibility | null>(null); const checking = ref(false); let checkToken = 0
const fieldLabels: Record<CompatibilityField, string> = { wavelength_nm: '波长', pulse_width_ns: '脉宽', sample_interval_ns: '采样间隔', captured_after_baseline: '采集时间顺序' }
const fieldIcons: Record<CompatibilityField, unknown> = { wavelength_nm: Waves, pulse_width_ns: Timer, sample_interval_ns: Ruler, captured_after_baseline: CalendarClock }
async function search() { await cases.fetch({ status: status.value || undefined, page_size: 100 }) }
function chooseRoute() { const route = routes.items.find((item)=>item.id===form.route_id); form.baseline_trace_id = route?.baseline_trace_id ?? 0; form.current_trace_id = routeTraces.value.find((trace)=>trace.id !== form.baseline_trace_id)?.id ?? 0 }
function traceLabel(id: number) { const trace = traces.items.find((item) => item.id === id); return trace ? `#${trace.id} · ${trace.wavelength_nm} nm · 脉宽 ${trace.pulse_width_ns} ns` : `#${id}` }
function traceOptionLabel(id: number) { const trace = traces.items.find((item) => item.id === id); return trace ? `#${trace.id} · ${trace.wavelength_nm} nm · ${trace.pulse_width_ns} ns · 间隔 ${trace.sample_interval_ns} ns` : `#${id}` }
function formatTime(value: string) { return new Date(value).toLocaleString() }
watch([createOpen, () => form.route_id, () => form.baseline_trace_id, () => form.current_trace_id], async () => {
  if (!createOpen.value || !pairSelected.value) { compatibility.value = null; return }
  const token = ++checkToken; checking.value = true
  try {
    const result = await cases.checkCompatibility({ route_id: form.route_id, baseline_trace_id: form.baseline_trace_id, current_trace_id: form.current_trace_id })
    if (token === checkToken) compatibility.value = result
  } catch {
    if (token === checkToken) compatibility.value = null
  } finally {
    if (token === checkToken) checking.value = false
  }
}, { immediate: true })
async function create() {
  if (compatibility.value && !compatibility.value.compatible) { ElMessage.warning('测量条件不满足可比性要求，请按逐项结果更换轨迹'); return }
  busy.value=true; try { await cases.create(form); createOpen.value=false; ElMessage.success('定位案例已建立') } finally { busy.value=false }
}
async function analyze(item: LocalizationCase) { busy.value=true; try { await cases.analyze(item.id, {}); ElMessage.success('基线差异分析已完成') } finally { busy.value=false } }
function confirm(item: LocalizationCase) { current.value=item; reviewOpen.value=true }
async function submitReview(value: Record<string, unknown>) { if(!current.value)return; busy.value=true; try { await cases.confirm(current.value.id,value); reviewOpen.value=false; ElMessage.success('人工结论已确认') } finally { busy.value=false } }
async function close(item: LocalizationCase) { await ElMessageBox.confirm('关闭后案例不可再修改，确认继续？','关闭案例',{type:'warning',confirmButtonText:'确认关闭',cancelButtonText:'取消'}); await cases.close(item.id,item.version); ElMessage.success('案例已关闭') }
onMounted(async()=>{ await Promise.all([routes.fetch({page_size:100}),traces.fetch({page_size:100}),search()]); if(routes.items[0]){form.route_id=routes.items[0].id;chooseRoute()} })
</script>

<template>
  <PageHeader title="定位案例" eyebrow="LOCALIZATION CASES" description="基线差异仅作分析参考，由复核人员形成最终结论。"><el-button v-if="auth.canAnalyze()" type="primary" @click="createOpen=true"><Plus :size="16" />新建案例</el-button></PageHeader>
  <section class="content-band">
    <div class="case-toolbar"><div class="state-track"><span v-for="(value,index) in CASE_STATUSES" :key="value"><i>{{ index+1 }}</i>{{ caseStatusLabel[value] }}</span></div><el-select v-model="status" clearable placeholder="全部状态" style="width:150px" @change="search"><el-option v-for="value in CASE_STATUSES" :key="value" :label="caseStatusLabel[value]" :value="value" /></el-select></div>
    <div class="data-surface">
      <el-table v-loading="cases.loading" :data="cases.items" row-key="id">
        <el-table-column prop="id" label="案例" width="80"><template #default="scope"><strong>#{{ scope.row.id }}</strong></template></el-table-column>
        <el-table-column label="线路" width="125"><template #default="scope">{{ routes.items.find(r=>r.id===scope.row.route_id)?.route_code ?? `#${scope.row.route_id}` }}</template></el-table-column>
        <el-table-column label="对比轨迹" min-width="150"><template #default="scope"><span class="trace-pair">#{{ scope.row.baseline_trace_id }} <GitCompareArrows :size="14" /> #{{ scope.row.current_trace_id }}</span></template></el-table-column>
        <el-table-column label="状态" width="115"><template #default="scope"><span class="status-pill" :class="scope.row.case_status">{{ caseStatusLabel[scope.row.case_status as LocalizationCase['case_status']] }}</span></template></el-table-column>
        <el-table-column label="估算位置" width="150"><template #default="scope"><template v-if="scope.row.estimated_distance_m != null"><strong>{{ scope.row.estimated_distance_m.toFixed(2) }} m</strong><small class="uncertainty">± {{ scope.row.uncertainty_m?.toFixed(2) }} m</small></template><span v-else class="muted">待分析</span></template></el-table-column>
        <el-table-column prop="conclusion" label="复核结论" min-width="230"><template #default="scope"><span v-if="scope.row.conclusion" class="conclusion">{{ scope.row.conclusion }}</span><span v-else class="muted">尚未确认</span></template></el-table-column>
        <el-table-column label="操作" width="190" fixed="right"><template #default="scope"><el-button v-if="scope.row.case_status==='draft' && auth.canAnalyze()" link type="primary" :loading="busy" @click="analyze(scope.row)"><Play :size="14" />分析</el-button><el-button v-if="scope.row.case_status==='pending_review' && auth.canReview()" link type="primary" @click="confirm(scope.row)"><ClipboardCheck :size="14" />确认</el-button><el-button v-if="scope.row.case_status==='confirmed' && auth.canReview()" link type="danger" @click="close(scope.row)"><LockKeyhole :size="14" />关闭</el-button><span v-if="scope.row.case_status==='closed'" class="muted">已归档</span></template></el-table-column>
        <template #empty><div class="empty-state"><div><ClipboardCheck :size="34" /><strong>尚无定位案例</strong><span>选择同线路、同测量条件的基线与当前轨迹建立对比。</span></div></div></template>
      </el-table>
    </div>
  </section>
  <el-dialog v-model="createOpen" title="建立基线对比案例" width="min(680px, calc(100vw - 28px))"><el-form label-position="top"><el-form-item label="线路"><el-select v-model="form.route_id" style="width:100%" @change="chooseRoute"><el-option v-for="route in routes.items" :key="route.id" :label="`${route.route_code} / ${route.name}`" :value="route.id" /></el-select></el-form-item><div class="case-form-grid"><el-form-item label="基线轨迹"><el-select v-model="form.baseline_trace_id" style="width:100%"><el-option v-for="trace in routeTraces" :key="trace.id" :label="traceOptionLabel(trace.id)" :value="trace.id" /></el-select></el-form-item><el-form-item label="当前轨迹"><el-select v-model="form.current_trace_id" style="width:100%"><el-option v-for="trace in routeTraces" :key="trace.id" :label="traceOptionLabel(trace.id)" :value="trace.id" /></el-select></el-form-item><el-form-item label="距离容差 m"><el-input-number v-model="form.distance_tolerance_m" :min="0.1" :max="1000" style="width:100%" /></el-form-item><el-form-item label="损耗增量阈值 dB"><el-input-number v-model="form.loss_increase_db" :min="0.1" :max="20" :step="0.1" style="width:100%" /></el-form-item></div></el-form>
  <div v-if="pairSelected" class="compat-panel" v-loading="checking">
    <div class="compat-head" :class="{ ok: compatibility?.compatible, bad: compatibility && !compatibility.compatible }">
      <component :is="compatibility?.compatible ? CircleCheck : CircleX" v-if="compatibility" :size="18" />
      <strong v-if="!compatibility">正在核对两条轨迹的测量条件…</strong>
      <strong v-else-if="compatibility.compatible">测量条件一致且当前轨迹晚于基线，可建立对比案例</strong>
      <strong v-else>测量条件不满足可比性要求，案例将被拒绝建立</strong>
    </div>
    <template v-if="compatibility">
      <div class="compat-traces">
        <div><small>基线轨迹</small><strong>{{ traceLabel(compatibility.baseline.trace_id) }}</strong><span>{{ formatTime(compatibility.baseline.captured_at) }}</span></div>
        <GitCompareArrows :size="16" class="compat-arrow" />
        <div><small>当前轨迹</small><strong>{{ traceLabel(compatibility.current.trace_id) }}</strong><span>{{ formatTime(compatibility.current.captured_at) }}</span></div>
      </div>
      <ul class="compat-checks">
        <li v-for="check in compatibility.checks" :key="check.field" :class="{ pass: check.passed, fail: !check.passed }">
          <component :is="fieldIcons[check.field as CompatibilityField] ?? Waves" :size="15" class="check-icon" />
          <div class="check-body"><strong>{{ fieldLabels[check.field as CompatibilityField] ?? check.field }}</strong><span>{{ check.detail }}</span><small>要求：{{ check.expected }} ｜ 实际：{{ check.actual }}</small></div>
          <CircleCheck v-if="check.passed" :size="17" class="pass-mark" /><CircleX v-else :size="17" class="fail-mark" />
        </li>
      </ul>
      <el-alert v-if="!compatibility.compatible" type="error" :closable="false" show-icon title="波长、脉宽或采样间隔不同会使背向散射、事件宽度与距离网格不可比；当前轨迹还必须晚于基线采集。请更换轨迹后再建立案例。" />
    </template>
  </div>
  <template #footer><el-button @click="createOpen=false">取消</el-button><el-button type="primary" :loading="busy" :disabled="!pairSelected || checking || !compatibility?.compatible" @click="create">建立案例</el-button></template></el-dialog>
  <ReviewDialog v-model="reviewOpen" mode="case" :case-item="current" :loading="busy" @submit="submitReview" />
</template>

<style scoped>
.case-toolbar{display:flex;align-items:center;justify-content:space-between;gap:20px;margin-bottom:14px}.state-track{display:flex;align-items:center;overflow:auto}.state-track span{display:flex;align-items:center;gap:6px;color:var(--text-muted);font-size:11px;font-weight:700;white-space:nowrap}.state-track span:not(:last-child)::after{content:'';width:24px;height:1px;margin:0 7px;background:var(--line-strong)}.state-track i{width:20px;height:20px;display:grid;place-items:center;border:1px solid var(--line-strong);border-radius:50%;font-style:normal}.trace-pair{display:inline-flex;align-items:center;gap:6px}.uncertainty{display:block;margin-top:2px;color:var(--text-muted)}.conclusion{display:-webkit-box;overflow:hidden;-webkit-line-clamp:2;-webkit-box-orient:vertical}.muted{color:var(--text-muted);font-size:12px}.case-form-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:0 14px}
.compat-panel{margin:4px 0 12px;border:1px solid var(--line-strong);border-radius:6px;padding:12px;background:var(--surface-strong)}.compat-head{display:flex;align-items:center;gap:8px;font-size:13px;margin-bottom:10px;color:var(--text-muted)}.compat-head.ok{color:var(--accent)}.compat-head.bad{color:var(--danger,#c0392b)}.compat-traces{display:flex;align-items:stretch;gap:10px;margin-bottom:10px}.compat-traces>div{flex:1;display:flex;flex-direction:column;gap:2px;padding:8px 10px;border:1px solid var(--line-strong);border-radius:5px;background:var(--el-bg-color,#fff)}.compat-traces small{color:var(--text-muted);font-size:11px;font-weight:700}.compat-traces strong{font-size:13px}.compat-traces span{font-size:11px;color:var(--text-muted)}.compat-arrow{align-self:center;color:var(--text-muted)}.compat-checks{list-style:none;margin:0 0 10px;padding:0;display:flex;flex-direction:column;gap:7px}.compat-checks li{display:flex;align-items:flex-start;gap:9px;padding:8px 10px;border:1px solid var(--line-strong);border-left-width:3px;border-radius:4px;background:var(--el-bg-color,#fff)}.compat-checks li.pass{border-left-color:var(--accent)}.compat-checks li.fail{border-left-color:var(--danger,#c0392b)}.check-icon{margin-top:2px;color:var(--text-muted)}.check-body{flex:1;display:flex;flex-direction:column;gap:2px}.check-body strong{font-size:12px}.check-body span{font-size:12px}.check-body small{font-size:11px;color:var(--text-muted);word-break:break-word}.pass-mark{color:var(--accent)}.fail-mark{color:var(--danger,#c0392b)}
@media(max-width:700px){.case-toolbar{align-items:stretch;flex-direction:column}.case-form-grid{grid-template-columns:1fr}.compat-traces{flex-direction:column}.compat-arrow{transform:rotate(90deg)}}
</style>
