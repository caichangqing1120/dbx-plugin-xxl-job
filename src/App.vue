<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue';
import { Workflow, ListTodo, Server, ScrollText, RefreshCw, Search, Plus, Pencil, Trash2, Play, Pause, FileText, ChevronLeft, ChevronRight, X, Lock, AlertTriangle, Check, Plug } from '@lucide/vue';
import { invoke, type Context } from './bridge';
import { type Job, type Group, type JobOption, type LogRow, type Info, type Page, type Form, type LogChunk, newJob, newGroup, jobForm, groupForm, jobStatus, schedules, timeLabel, triggerMillis, filterTime, advanceLog } from './domain';
import JobEditor from './JobEditor.vue';
import GroupEditor from './GroupEditor.vue';

type View = 'jobs' | 'groups' | 'logs';
const navigation = [{ key: 'jobs', label: '任务管理', icon: ListTodo }, { key: 'groups', label: '执行器', icon: Server }, { key: 'logs', label: '调度日志', icon: ScrollText }] as const;
const visibleNavigation = computed(() => navigation.filter(item => item.key !== 'groups' || info.value?.admin));
const view = ref<View>('jobs');
const connectionId = ref('');
const info = ref<Info>();
const jobs = ref<Job[]>([]), groups = ref<Group[]>([]), logs = ref<LogRow[]>([]), taskOptions = ref<JobOption[]>([]);
const optionsBusy = ref(false), authExpired = ref(false);
const loading = ref(false), working = ref(false), error = ref(''), groupsError = ref(''), notice = ref('');
const page = ref(1), pageSize = ref(25), total = ref(0);
const freshFilters = () => ({ jobGroup: -1, jobDesc: '', executorHandler: '', author: '', triggerStatus: -1, jobId: '', logStatus: -1, from: '', to: '', groupSearch: '' });
const filters = ref(freshFilters());
const filteredGroups = computed(() => groups.value.filter(g => `${g.appname} ${g.title} ${g.addressList}`.toLowerCase().includes(filters.value.groupSearch.toLowerCase())));
const label = computed(() => navigation.find(n => n.key === view.value)!.label);
const canWrite = computed(() => !!info.value && !info.value.readOnly && !authExpired.value && !working.value && !loading.value);
const canManageGroups = computed(() => canWrite.value && info.value?.admin);
const groupLabel = (id: number) => groups.value.find(g => g.id === id)?.title || `#${id}`;
const runningOnPage = computed(() => jobs.value.filter(j => j.triggerStatus === 1).length);
const scheduleLabel = (type: string) => schedules.find(([key]) => key === type)?.[1] || type;
let epoch = 0, listRequest = 0, groupRequest = 0, logRequest = 0, optionRequest = 0;
let unsubscribe: (() => void) | undefined;
let logTimer: ReturnType<typeof setTimeout> | undefined;
const jobEditor = ref<Job>(), groupEditor = ref<Group>();
const jobEditorRef = ref<InstanceType<typeof JobEditor>>(), groupEditorRef = ref<InstanceType<typeof GroupEditor>>();
type Confirmation = { method: string; title: string; target: string; form: Form; changes?: { key: string; before: string; after: string }[]; remove?: boolean; id?: number };
const confirmation = ref<Confirmation>(), deletionId = ref(''), discard = ref(false), detail = ref<LogRow>();
const execution = ref<Job>(), executionParam = ref('');
type LogState = { row: LogRow; text: string; next: number; end: boolean; busy: boolean; error: string; follow: boolean };
const logState = ref<LogState>();
const message = (e: unknown) => { const value = e instanceof Error ? e.message : String(e); if (value.includes('登录已失效')) authExpired.value = true; return value; };

async function focusDialog() { await nextTick(); const dialogs = document.querySelectorAll<HTMLElement>('[role="dialog"]'); const dialog = dialogs[dialogs.length - 1]; dialog?.querySelector<HTMLElement>('input:not([disabled]):not([readonly]), select:not([disabled]), button:not([disabled]), textarea:not([disabled])')?.focus(); }
async function loadGroups() {
  const token = ++groupRequest, current = epoch;
  try { const data = await invoke<Group[]>(connectionId.value, 'xxljob/groups'); if (current === epoch && token === groupRequest) { groups.value = data; groupsError.value = ''; } }
  catch (e) { if (current === epoch && token === groupRequest) { groups.value = []; groupsError.value = message(e); } }
}
async function loadTaskOptions(): Promise<boolean> {
  const token = ++optionRequest, current = epoch, group = filters.value.jobGroup;
  taskOptions.value = []; optionsBusy.value = group > 0;
  if (group < 1) { filters.value.jobId = ''; return true; }
  try {
    const data = await invoke<JobOption[]>(connectionId.value, 'xxljob/jobsByGroup', { form: { jobGroup: group } });
    if (current !== epoch || token !== optionRequest) return false;
    taskOptions.value = data;
    if (!data.some(job => String(job.id) === filters.value.jobId)) filters.value.jobId = '';
    return true;
  } catch (e) { if (current === epoch && token === optionRequest) { filters.value.jobId = ''; logs.value = []; total.value = 0; error.value = message(e); } return false; }
  finally { if (current === epoch && token === optionRequest) optionsBusy.value = false; }
}
async function changeGroup() { page.value = 1; if (view.value === 'logs') { filters.value.jobId = ''; if (!await loadTaskOptions()) return; } await load(); }
async function showJobLogs(job: Job) { filters.value.jobGroup = job.jobGroup; filters.value.jobId = String(job.id); await changeView('logs'); }
async function load() {
  if (!info.value || authExpired.value) return;
  const token = ++listRequest, current = epoch, targetView = view.value;
  loading.value = true; error.value = '';
  try {
    if (targetView === 'groups') { await loadGroups(); return; }
    const form: Form = { start: (page.value - 1) * pageSize.value, length: pageSize.value };
    if (targetView === 'jobs') Object.assign(form, { jobGroup: filters.value.jobGroup, triggerStatus: filters.value.triggerStatus, jobDesc: filters.value.jobDesc, executorHandler: filters.value.executorHandler, author: filters.value.author });
    if (targetView === 'logs') {
      if (filters.value.jobId && (!/^\d+$/.test(filters.value.jobId) || Number(filters.value.jobId) < 1)) throw new Error('任务 ID 必须是正整数');
      Object.assign(form, { jobGroup: filters.value.jobGroup > 0 ? filters.value.jobGroup : 0, jobId: filters.value.jobId || 0, logStatus: filters.value.logStatus, filterTime: filterTime(filters.value.from, filters.value.to) });
    }
    const data = await invoke<Page<Job | LogRow>>(connectionId.value, `xxljob/${targetView}`, { form });
    if (current !== epoch || token !== listRequest) return;
    total.value = data.recordsFiltered;
    if (targetView === 'jobs') jobs.value = data.data as Job[];
    if (targetView === 'logs') logs.value = data.data as LogRow[];
    if (page.value > 1 && !data.data.length && total.value > 0) { page.value = Math.max(1, Math.ceil(total.value / pageSize.value)); await load(); }
  } catch (e) {
    if (current === epoch && token === listRequest) { error.value = message(e); jobs.value = []; logs.value = []; total.value = 0; }
  } finally { if (current === epoch && token === listRequest) loading.value = false; }
}
function clearOverlays() { jobEditor.value = undefined; groupEditor.value = undefined; confirmation.value = undefined; execution.value = undefined; discard.value = false; detail.value = undefined; closeLog(); }
async function contextChanged(context: Context) {
  const current = ++epoch; ++listRequest; ++groupRequest; ++optionRequest;
  connectionId.value = context.connectionId || ''; info.value = undefined; groups.value = []; jobs.value = []; logs.value = []; taskOptions.value = []; optionsBusy.value = false; authExpired.value = false; filters.value = freshFilters(); view.value = 'jobs'; notice.value = ''; error.value = ''; groupsError.value = ''; loading.value = false; working.value = false; total.value = 0; page.value = 1; clearOverlays();
  if (!connectionId.value) return;
  loading.value = true;
  try { const data = await invoke<Info>(connectionId.value, 'xxljob/info'); if (current !== epoch) return; info.value = data; await loadGroups(); if (current !== epoch) return; if (!data.admin) filters.value.jobGroup = groups.value[0]?.id || 0; await load(); }
  catch (e) { if (current === epoch) error.value = message(e); }
  finally { if (current === epoch) loading.value = false; }
}
async function changeView(value: View) { if (working.value || jobEditor.value || groupEditor.value || (value === 'groups' && !info.value?.admin)) return; clearOverlays(); view.value = value; page.value = 1; total.value = 0; if (value === 'logs' && !await loadTaskOptions()) return; await load(); }
function search() { if (loading.value || working.value) return; page.value = 1; void load(); }
function searchOnEnter(event: KeyboardEvent) { if ((event.target as HTMLElement).tagName === 'INPUT') { event.preventDefault(); search(); } }
async function refresh() {
  const current = epoch;
  try { const data = await invoke<Info>(connectionId.value, 'xxljob/info'); if (current !== epoch) return; info.value = data; await loadGroups(); if (current !== epoch) return; if (!data.admin && !groups.value.some(g => g.id === filters.value.jobGroup)) filters.value.jobGroup = groups.value[0]?.id || 0; if (!data.admin && view.value === 'groups') view.value = 'jobs'; if (view.value === 'logs') await loadTaskOptions(); await load(); }
  catch (e) { if (current === epoch) error.value = message(e); }
}
function movePage(delta: number) { page.value += delta; void load(); }
function editJob(job?: Job) { if (!canWrite.value) return; jobEditor.value = job ? { ...newJob(job.jobGroup), ...job } : newJob(groups.value[0]?.id || 0); void focusDialog(); }
function editGroup(group?: Group) { if (!canManageGroups.value) return; groupEditor.value = group ? { ...group } : newGroup(); void focusDialog(); }
function closeEditor() { if (working.value) return; if (jobEditorRef.value?.isDirty || groupEditorRef.value?.isDirty) { discard.value = true; void focusDialog(); } else { jobEditor.value = undefined; groupEditor.value = undefined; } }
function diff(before: Form, after: Form) { return Object.entries(after).filter(([key, value]) => before[key] !== value).map(([key, value]) => ({ key, before: String(before[key] ?? '-'), after: String(value === '' ? '（空）' : value) })); }
function prepareSaveJob(job: Job) { const form = jobForm(job); confirmation.value = { method: 'xxljob/saveJob', title: job.id ? '确认更新任务' : '确认新增任务', target: `${job.jobDesc} · ${groupLabel(job.jobGroup)}`, form, changes: diff(job.id && jobEditor.value ? jobForm(jobEditor.value) : {}, form) }; void focusDialog(); }
function prepareSaveGroup(group: Group) { const form = groupForm(group); confirmation.value = { method: 'xxljob/saveGroup', title: group.id ? '确认更新执行器' : '确认新增执行器', target: `${group.title} · ${group.appname}`, form, changes: diff(group.id && groupEditor.value ? groupForm(groupEditor.value) : {}, form) }; void focusDialog(); }
function prepareAction(method: 'start' | 'stop' | 'removeJob' | 'removeGroup', row: Job | Group) {
  if (!canWrite.value || !row.id) return;
  const titles = { start: '确认启动任务', stop: '确认停止调度', removeJob: '确认删除任务', removeGroup: '确认删除执行器' };
  deletionId.value = ''; confirmation.value = { method: `xxljob/${method}`, title: titles[method], target: `${'jobDesc' in row ? row.jobDesc : row.title} · #${row.id}`, form: { id: row.id }, id: row.id, remove: method.startsWith('remove') }; void focusDialog();
}
function execute(job: Job) { if (!canWrite.value) return; execution.value = job; executionParam.value = job.executorParam || ''; void focusDialog(); }
function prepareExecution() { if (!execution.value?.id) return; confirmation.value = { method: 'xxljob/trigger', title: '确认执行一次', target: `${execution.value.jobDesc} · #${execution.value.id}`, form: { id: execution.value.id, executorParam: executionParam.value }, changes: [{ key: 'executorParam', before: execution.value.executorParam || '（空）', after: executionParam.value || '（空）' }] }; execution.value = undefined; void focusDialog(); }
async function commit() {
  if (!confirmation.value || !canWrite.value || (confirmation.value.remove && deletionId.value !== String(confirmation.value.id))) return;
  const pending = confirmation.value, current = epoch;
  working.value = true; error.value = ''; notice.value = '';
  try {
    await invoke(connectionId.value, pending.method, { form: { ...pending.form }, confirmed: true });
    if (current !== epoch) return;
    notice.value = pending.method === 'xxljob/trigger' ? '执行请求已提交，请在调度日志中确认执行结果。' : '操作成功。';
    confirmation.value = undefined; jobEditor.value = undefined; groupEditor.value = undefined;
    await Promise.all([loadGroups(), load()]);
  } catch (e) { if (current === epoch) { error.value = message(e); confirmation.value = undefined; } }
  finally { if (current === epoch) working.value = false; }
}
function closeLog() { ++logRequest; if (logTimer) clearTimeout(logTimer); logTimer = undefined; logState.value = undefined; }
function openLog(row: LogRow) { closeLog(); logState.value = { row, text: '', next: 1, end: false, busy: false, error: '', follow: false }; void readLog(); void focusDialog(); }
function scheduleLog() { if (logTimer) clearTimeout(logTimer); logTimer = undefined; if (logState.value?.follow && !logState.value.end && !logState.value.error) logTimer = setTimeout(() => void readLog(), 2000); }
async function readLog() {
  const state = logState.value; if (!state || state.busy || state.end) return;
  if (logTimer) clearTimeout(logTimer); logTimer = undefined;
  const token = ++logRequest, current = epoch; state.busy = true; state.error = '';
  try {
    const form = (info.value?.adminVersion === '3.4' || info.value?.adminVersion === '3.0')
      ? { logId: state.row.id, fromLineNum: state.next }
      : { executorAddress: state.row.executorAddress, triggerTime: triggerMillis(state.row.triggerTime), logId: state.row.id, fromLineNum: state.next };
    const chunk = await invoke<LogChunk>(connectionId.value, 'xxljob/logContent', { form });
    if (current !== epoch || token !== logRequest || logState.value !== state) return;
    const next = advanceLog(state.next, chunk);
    if (state.text.length + next.text.length > 8 * 1024 * 1024) throw new Error('日志显示达到 8 MiB 上限，请在执行器侧查看后续内容');
    state.text += next.text; state.next = next.next; state.end = next.end;
  } catch (e) { if (current === epoch && token === logRequest && logState.value === state) { state.error = message(e); state.follow = false; } }
  finally { if (current === epoch && token === logRequest && logState.value === state) { state.busy = false; scheduleLog(); } }
}
function keyboard(event: KeyboardEvent) {
  const dialogs = document.querySelectorAll<HTMLElement>('[role="dialog"]'); const top = dialogs[dialogs.length - 1]; if (!top) return;
  if (event.key === 'Escape' && !working.value) { event.preventDefault(); if (confirmation.value) confirmation.value = undefined; else if (discard.value) discard.value = false; else if (execution.value) execution.value = undefined; else if (detail.value) detail.value = undefined; else if (logState.value) closeLog(); else closeEditor(); }
  if (event.key === 'Tab') { const elements = [...top.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex="0"]')]; const first = elements[0], last = elements[elements.length - 1]; if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); } else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); } }
}
onMounted(async () => {
  document.addEventListener('keydown', keyboard);
  if (!window.dbxPlugin) return;
  try { await window.dbxPlugin.ready; unsubscribe = window.dbxPlugin.onContext(context => void contextChanged(context)); await contextChanged(window.dbxPlugin.context); }
  catch (e) { error.value = message(e); }
});
onBeforeUnmount(() => { ++epoch; unsubscribe?.(); closeLog(); document.removeEventListener('keydown', keyboard); });
</script>

<template>
  <div class="shell">
    <aside class="sidebar"><div class="brand"><Workflow :size="26" /><div><strong>XXL-JOB</strong><small>Admin {{ info?.adminVersion || '2.3 / 3.0 / 3.4' }}.x</small></div></div>
      <nav aria-label="主导航"><button v-for="item in visibleNavigation" :key="item.key" :class="{ active: view === item.key }" :disabled="working || !!jobEditor || !!groupEditor || optionsBusy" :title="item.label" @click="changeView(item.key)"><component :is="item.icon" :size="19" /><span>{{ item.label }}</span></button></nav>
      <div class="sidebar-bottom"><small>PRIVATE WORKBENCH</small><span>v0.1.1</span></div>
    </aside>
    <main :inert="!!jobEditor || !!groupEditor || !!confirmation || !!discard || !!execution || !!detail || !!logState">
      <header class="topbar"><div class="connection-title"><span class="connection-dot" :class="{ connected: !!info && !authExpired }" /><strong>{{ info?.name || '未连接' }}</strong><span v-if="info?.environment" class="environment">{{ info.environment }}</span><span v-if="info" class="muted">{{ info.username }} · {{ info.admin ? '管理员' : '普通用户' }}</span><span v-if="info?.readOnly" class="badge warning"><Lock :size="12" />只读</span></div><button class="icon" aria-label="刷新" title="刷新" :disabled="!info || authExpired || loading || working || optionsBusy" @click="refresh"><RefreshCw :size="18" :class="{ spinning: loading }" /></button></header>
      <div class="page-heading"><div><h1>{{ label }}</h1><code v-if="info" class="endpoint">{{ info.baseUrl }}</code></div><div v-if="info" class="page-summary"><strong>{{ view === 'groups' ? filteredGroups.length : total }}</strong><span>{{ view === 'groups' ? '个执行器' : '条记录' }}</span><span v-if="view === 'jobs'" class="muted">本页运行中 {{ runningOnPage }}</span></div></div>
      <div v-if="error" class="banner error" role="alert"><AlertTriangle :size="17" /><span>{{ error }}</span></div>
      <div v-if="notice" class="banner success" role="status"><Check :size="17" /><span>{{ notice }}</span><button class="icon" title="关闭提示" aria-label="关闭提示" @click="notice = ''"><X :size="16" /></button></div>
      <div v-if="groupsError" class="banner warning" role="alert"><AlertTriangle :size="17" /><span>执行器列表：{{ groupsError }}</span><button :disabled="loading || working" @click="loadGroups">重试</button></div>
      <section v-if="!info" class="empty-state"><Plug :size="38" /><h2>{{ loading ? '正在连接' : '未连接调度中心' }}</h2><p v-if="!loading">请从 DBX 的 XXL-JOB 连接打开工作台。</p><button v-if="connectionId && !loading" @click="contextChanged({ connectionId })"><RefreshCw :size="16" />重试</button></section>
      <template v-else>
        <div class="filters" @keydown.enter="searchOnEnter">
          <template v-if="view === 'jobs' || view === 'logs'"><select v-model.number="filters.jobGroup" aria-label="筛选执行器" :disabled="loading || working || optionsBusy || authExpired" @change="changeGroup"><option v-if="info.admin" :value="-1">全部执行器</option><option v-for="g in groups" :key="g.id" :value="g.id">{{ g.title }}</option></select></template>
          <template v-if="view === 'jobs'"><select v-model.number="filters.triggerStatus" aria-label="任务状态筛选" :disabled="loading || working || authExpired" @change="search"><option :value="-1">全部状态</option><option :value="1">运行中</option><option :value="0">已停止</option></select><input v-model="filters.jobDesc" aria-label="任务描述筛选" placeholder="任务描述" /><input v-model="filters.executorHandler" aria-label="Handler 筛选" placeholder="Handler" /><input v-model="filters.author" aria-label="负责人筛选" placeholder="负责人" /></template>
          <template v-if="view === 'groups'"><input v-model="filters.groupSearch" aria-label="查询执行器" placeholder="名称、AppName 或地址" /></template>
          <template v-if="view === 'logs'"><select v-model="filters.jobId" aria-label="筛选具体任务" :disabled="loading || working || optionsBusy || filters.jobGroup < 1 || authExpired" @change="search"><option value="">{{ optionsBusy ? '任务加载中' : '全部任务' }}</option><option v-for="job in taskOptions" :key="job.id" :value="String(job.id)">#{{ job.id }} · {{ job.jobDesc }}</option></select><select v-model.number="filters.logStatus" aria-label="日志状态" :disabled="loading || working || optionsBusy || authExpired" @change="search"><option :value="-1">全部状态</option><option :value="1">执行成功</option><option :value="2">执行失败</option><option :value="3">执行中</option></select></template>
          <template v-if="view === 'logs'"><input v-model="filters.from" type="datetime-local" aria-label="开始时间" /><input v-model="filters.to" type="datetime-local" aria-label="结束时间" /></template>
          <button type="button" :disabled="loading || working || optionsBusy || authExpired" @click="search"><Search :size="16" />查询</button><div class="spacer" /><button v-if="view === 'jobs'" type="button" class="primary" :disabled="!canWrite || !groups.length || !!groupsError" @click="editJob()"><Plus :size="16" />新增任务</button><button v-if="view === 'groups'" type="button" class="primary" :disabled="!canManageGroups" @click="editGroup()"><Plus :size="16" />新增执行器</button>
        </div>
        <div class="table-wrap" :aria-busy="loading">
          <table v-if="view === 'jobs'">
            <thead><tr><th>ID / 任务</th><th>执行器 / Handler</th><th>调度配置</th><th>负责人</th><th>状态</th><th class="actions">操作</th></tr></thead>
            <tbody><tr v-for="job in jobs" :key="job.id">
              <td><span class="muted mono">#{{ job.id }}</span><strong class="cell-title">{{ job.jobDesc }}</strong></td>
              <td><span>{{ groupLabel(job.jobGroup) }}</span><code class="cell-sub">{{ job.executorHandler || job.glueType }}</code></td>
              <td><span class="muted">{{ scheduleLabel(job.scheduleType) }}</span><code class="cell-sub">{{ job.scheduleType === 'NONE' ? '-' : job.scheduleConf }}{{ job.scheduleType === 'FIX_RATE' ? ' 秒' : '' }}</code></td>
              <td>{{ job.author }}</td><td><span class="badge" :class="jobStatus(job.triggerStatus).tone">{{ jobStatus(job.triggerStatus).label }}</span></td>
              <td class="actions">
                <button class="icon" aria-label="编辑任务" :title="`编辑任务 #${job.id}`" :disabled="!canWrite" @click="editJob(job)"><Pencil :size="16" /></button>
                <button class="icon" aria-label="执行一次" title="执行一次" :disabled="!canWrite" @click="execute(job)"><Play :size="16" /></button>
                <button class="icon" :aria-label="job.triggerStatus === 1 ? '停止调度' : '启动任务'" :title="job.triggerStatus === 1 ? '停止调度' : '启动任务'" :disabled="!canWrite" @click="prepareAction(job.triggerStatus === 1 ? 'stop' : 'start', job)"><Pause v-if="job.triggerStatus === 1" :size="16" /><Workflow v-else :size="16" /></button>
                <button class="icon" aria-label="查看任务日志" title="查看任务日志" :disabled="loading || working || authExpired" @click="showJobLogs(job)"><FileText :size="16" /></button>
                <button class="icon danger-text" aria-label="删除任务" title="删除任务" :disabled="!canWrite" @click="prepareAction('removeJob', job)"><Trash2 :size="16" /></button>
              </td>
            </tr></tbody>
          </table>
          <table v-if="view === 'groups'"><thead><tr><th>ID / 执行器</th><th>AppName</th><th>注册方式</th><th>执行器地址</th><th class="actions">操作</th></tr></thead><tbody><tr v-for="g in filteredGroups" :key="g.id"><td><span class="muted mono">#{{ g.id }}</span><strong class="cell-title">{{ g.title }}</strong></td><td><code>{{ g.appname }}</code></td><td><span class="badge neutral">{{ g.addressType === 0 ? '自动注册' : '手动录入' }}</span></td><td class="address-cell"><code v-for="address in (g.addressList || '').split(',').filter(Boolean)" :key="address" class="cell-sub">{{ address }}</code><span v-if="!g.addressList" class="muted">无注册地址</span></td><td class="actions"><button class="icon" aria-label="编辑执行器" title="编辑执行器" :disabled="!canManageGroups" @click="editGroup(g)"><Pencil :size="16" /></button><button class="icon danger-text" aria-label="删除执行器" title="删除执行器" :disabled="!canManageGroups" @click="prepareAction('removeGroup', g)"><Trash2 :size="16" /></button></td></tr></tbody></table>
          <table v-if="view === 'logs'"><thead><tr><th>日志 / 任务 ID</th><th>触发时间</th><th>执行器地址</th><th>调度结果</th><th>执行结果</th><th class="actions">操作</th></tr></thead><tbody><tr v-for="row in logs" :key="row.id"><td><strong class="mono">#{{ row.id }}</strong><span class="cell-sub muted">任务 #{{ row.jobId }}</span></td><td>{{ timeLabel(row.triggerTime) }}</td><td class="address-cell"><code>{{ row.executorAddress || '-' }}</code><span class="cell-sub muted">{{ row.executorHandler }}</span></td><td><span class="badge" :class="row.triggerCode === 200 ? 'success' : row.triggerCode ? 'danger' : 'neutral'">{{ row.triggerCode === 200 ? '成功' : row.triggerCode ? '失败' : '待调度' }}</span></td><td><span class="badge" :class="row.handleCode === 200 ? 'success' : row.handleCode ? 'danger' : 'warning'">{{ row.handleCode === 200 ? '成功' : row.handleCode ? '失败' : '未完成' }}</span></td><td class="actions"><button class="icon" aria-label="查看日志详情" title="查看详情" @click="detail = row; focusDialog()"><Search :size="16" /></button><button class="icon" aria-label="读取执行日志" title="读取执行日志" :disabled="!row.executorAddress || row.triggerCode !== 200" @click="openLog(row)"><FileText :size="16" /></button></td></tr></tbody></table>
          <div v-if="loading" class="table-state"><RefreshCw class="spinning" :size="20" /><span>加载中</span></div>
          <div v-else-if="!error && (view === 'groups' ? !filteredGroups.length && !groupsError : !total)" class="table-state"><component :is="view === 'groups' ? Server : ListTodo" :size="28" /><span>没有匹配的{{ label }}</span></div>
        </div>
        <footer v-if="view !== 'groups'" class="pagination"><span>共 {{ total }} 条</span><div class="spacer" /><select v-model.number="pageSize" aria-label="每页条数" :disabled="loading || working" @change="search"><option :value="10">10 条 / 页</option><option :value="25">25 条 / 页</option><option :value="50">50 条 / 页</option><option :value="100">100 条 / 页</option></select><button class="icon" aria-label="上一页" title="上一页" :disabled="page <= 1 || loading || working" @click="movePage(-1)"><ChevronLeft :size="17" /></button><span>{{ page }} / {{ Math.max(1, Math.ceil(total / pageSize)) }}</span><button class="icon" aria-label="下一页" title="下一页" :disabled="page * pageSize >= total || loading || working" @click="movePage(1)"><ChevronRight :size="17" /></button></footer>
      </template>
    </main>
    <div v-if="jobEditor || groupEditor" class="overlay" :inert="!!confirmation || discard"><JobEditor v-if="jobEditor" ref="jobEditorRef" :job="jobEditor" :groups="groups" :busy="working" :server-error="error" @save="prepareSaveJob" @close="closeEditor" /><GroupEditor v-if="groupEditor" ref="groupEditorRef" :group="groupEditor" :busy="working" :server-error="error" @save="prepareSaveGroup" @close="closeEditor" /></div>
    <div v-if="confirmation" class="overlay modal-layer"><section class="modal" role="dialog" aria-modal="true" aria-label="确认操作"><header><div><small>{{ info?.environment || '未标记环境' }} · {{ info?.name }}</small><h2>{{ confirmation.title }}</h2></div><button class="icon" aria-label="取消确认" title="取消" :disabled="working" @click="confirmation = undefined"><X :size="18" /></button></header><div class="modal-body"><p class="target">{{ confirmation.target }}</p><code class="endpoint">{{ info?.baseUrl }}</code><p v-if="confirmation.remove" class="banner warning"><AlertTriangle :size="17" />删除后无法撤销。</p><p v-if="confirmation.method === 'xxljob/trigger'" class="banner warning"><AlertTriangle :size="17" />本次参数将覆盖保存的参数，空白表示传入空字符串。</p><p v-if="confirmation.method === 'xxljob/stop'" class="banner warning"><AlertTriangle :size="17" />只停止后续调度，不终止已经运行的任务。</p><div v-if="confirmation.changes?.length" class="changes"><div v-for="change in confirmation.changes" :key="change.key"><code>{{ change.key }}</code><pre class="before">{{ change.before }}</pre><pre>{{ change.after }}</pre></div></div><label v-if="confirmation.remove">输入 ID {{ confirmation.id }}<input v-model="deletionId" autocomplete="off" inputmode="numeric" aria-label="删除确认 ID" /></label></div><footer><button :disabled="working" @click="confirmation = undefined">取消</button><button :class="confirmation.remove ? 'danger' : 'primary'" :disabled="working || (confirmation.remove && deletionId !== String(confirmation.id))" @click="commit"><Check :size="16" />{{ working ? '提交中' : '确认' }}</button></footer></section></div>
    <div v-if="discard" class="overlay modal-layer"><section class="modal compact" role="dialog" aria-modal="true" aria-label="未保存的修改"><header><h2>未保存的修改</h2></header><div class="modal-body">关闭将丢弃当前修改。</div><footer><button @click="discard = false">继续编辑</button><button class="danger" @click="jobEditor = undefined; groupEditor = undefined; discard = false">放弃修改</button></footer></section></div>
    <div v-if="execution" class="overlay"><section class="modal" role="dialog" aria-modal="true" aria-label="执行一次"><header><div><small>任务 #{{ execution.id }}</small><h2>执行一次</h2></div><button class="icon" aria-label="取消执行" title="取消" @click="execution = undefined"><X :size="18" /></button></header><div class="modal-form"><div class="modal-body"><p class="target">{{ execution.jobDesc }}</p><label>本次执行参数<textarea v-model="executionParam" rows="6" aria-label="本次执行参数" spellcheck="false" /></label></div><footer><button type="button" @click="execution = undefined">取消</button><button type="button" class="primary" @click="prepareExecution"><Play :size="16" />执行一次</button></footer></div></section></div>
    <div v-if="logState" class="overlay"><section class="drawer log-drawer" role="dialog" aria-modal="true" aria-label="执行日志"><header><div><small>任务 #{{ logState.row.jobId }} · 日志 #{{ logState.row.id }}</small><h2>执行日志</h2></div><button class="icon" aria-label="关闭执行日志" title="关闭" @click="closeLog"><X :size="18" /></button></header><div class="log-meta"><code>{{ logState.row.executorAddress }}</code><span>{{ timeLabel(logState.row.triggerTime) }}</span></div><p v-if="logState.error" class="banner error" role="alert">{{ logState.error }}</p><pre class="log-content" tabindex="0">{{ logState.text || (logState.busy ? '读取中' : '暂无日志内容') }}</pre><footer><span class="badge" :class="logState.end ? 'success' : 'neutral'">{{ logState.end ? '日志已结束' : `下一行 ${logState.next}` }}</span><label class="inline"><input v-model="logState.follow" type="checkbox" :disabled="logState.end" @change="scheduleLog" />跟随</label><button :disabled="logState.busy || logState.end" @click="readLog"><RefreshCw :size="16" />{{ logState.busy ? '读取中' : '读取后续' }}</button></footer></section></div>
    <div v-if="detail" class="overlay"><section class="drawer" role="dialog" aria-modal="true" aria-label="记录详情"><header><h2>记录 #{{ detail.id }}</h2><button class="icon" aria-label="关闭记录详情" title="关闭" @click="detail = undefined"><X :size="18" /></button></header><div class="detail-body"><dl><template v-for="(value, key) in detail" :key="key"><dt>{{ key }}</dt><dd><pre>{{ value ?? '-' }}</pre></dd></template></dl></div></section></div>
  </div>
</template>
