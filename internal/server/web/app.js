const $ = (selector) => document.querySelector(selector);
const jobsEl = $('#jobs');
const form = $('#job-form');
const files = $('#files');
const dropzone = $('#dropzone');
const count = $('#file-count');
const submit = $('#submit');
const toast = $('#toast');
const baseURL = $('#base-url');
const apiKey = $('#api-key');
const chunkPages = $('#chunk-pages');
const workers = $('#workers');
const modeNote = $('#mode-note');

const modes = {
  free: {
    url: 'https://free-oxr.somark.cn/api/v1', key: 'dummy', pages: 20, workers: 1,
    note: '免费服务未公开上限，默认 20 页/册、单任务排队，遇到繁忙会自动等待。'
  },
  api: {
    url: 'https://somark.ai/api/v1', key: '', pages: 300, workers: 4,
    note: '正式 API 支持单文件 200MB、最多 300 页和 QPS 4；需要有效的 sk- API Key。'
  }
};

$('#today').textContent = new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date());

function showToast(message) {
  toast.textContent = message;
  toast.classList.add('show');
  window.setTimeout(() => toast.classList.remove('show'), 3600);
}

function updateCount() {
  count.textContent = files.files.length ? `已选 ${files.files.length} 份 · ${[...files.files].map(f => f.name).join(' / ')}` : '尚未选稿';
}
files.addEventListener('change', updateCount);
['dragenter', 'dragover'].forEach(name => dropzone.addEventListener(name, event => { event.preventDefault(); dropzone.classList.add('drag'); }));
['dragleave', 'drop'].forEach(name => dropzone.addEventListener(name, event => { event.preventDefault(); dropzone.classList.remove('drag'); }));
dropzone.addEventListener('drop', event => { files.files = event.dataTransfer.files; updateCount(); });

function selectMode(mode) {
  const config = modes[mode];
  baseURL.value = config.url;
  apiKey.value = config.key;
  apiKey.placeholder = mode === 'api' ? 'sk-…' : 'dummy';
  chunkPages.value = config.pages;
  workers.value = config.workers;
  modeNote.textContent = config.note;
}
document.querySelectorAll('input[name="mode"]').forEach(input => input.addEventListener('change', () => selectMode(input.value)));

form.addEventListener('submit', async event => {
  event.preventDefault();
  if (!files.files.length) return showToast('请先选择至少一份 PDF 或图片。');
  if (form.mode.value === 'api' && (!apiKey.value.trim() || apiKey.value.trim() === 'dummy')) return showToast('正式 API 模式需要填写 sk- 开头的 API Key。');
  submit.disabled = true;
  submit.querySelector('span').textContent = '正在收稿…';
  const body = new FormData(form);
  body.set('localize_images', form.localize_images.checked ? 'true' : 'false');
  body.set('keep_header_footer', form.keep_header_footer.checked ? 'true' : 'false');
  try {
    const response = await fetch('/api/jobs', { method: 'POST', body });
    if (!response.ok) throw new Error(await response.text());
    files.value = '';
    updateCount();
    showToast('文档已进入排字队列。');
    await refresh();
  } catch (error) {
    showToast(error.message || '提交失败');
  } finally {
    submit.disabled = false;
    submit.querySelector('span').textContent = '开始排印';
  }
});

function escapeHTML(value) {
  return String(value).replace(/[&<>'"]/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;' })[char]);
}

function renderJob(job, index) {
  const p = job.progress || {};
  const percent = job.status === 'done' ? 100 : p.total ? Math.round((p.done || 0) / p.total * 100) : 2;
  const downloads = job.status === 'done' ? `<div class="downloads">
    <a href="/api/jobs/${job.id}/download?format=md">MD</a>
    <a href="/api/jobs/${job.id}/download?format=json">JSON</a>
    <a href="/api/jobs/${job.id}/download?format=zip">ZIP + 插图</a>
  </div>` : '';
  const detail = job.error || p.message || '等待处理';
  return `<article class="job" style="animation-delay:${index * 45}ms">
    <div class="job-name"><strong>${escapeHTML(job.file_name)}</strong><small>${job.mode === 'api' ? '正式 API' : '免费 OXR'} · ${job.result?.pages ? `${job.result.pages} 页` : `工单 ${job.id.slice(-8)}`}</small></div>
    <div><div class="meter"><i style="width:${percent}%"></i></div><div class="progress-label"><span>${escapeHTML(p.stage || job.status)}</span><span>${percent}%</span></div></div>
    <div class="status"><span class="status-tag ${job.status}">${escapeHTML(job.status)}</span><small>${escapeHTML(detail)}</small>${downloads}</div>
  </article>`;
}

async function refresh() {
  try {
    const response = await fetch('/api/jobs');
    const data = await response.json();
    jobsEl.innerHTML = data.jobs.length ? data.jobs.map(renderJob).join('') : '<div class="empty">等待第一份文档进入排字间。</div>';
  } catch (_) {}
}

refresh();
window.setInterval(refresh, 1800);
