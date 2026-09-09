const $ = (selector, root = document) => root.querySelector(selector);
const state = { system: null, profiles: {}, jobs: [] };
let refreshTimer = null;

async function api(path, options = {}) {
  const response = await fetch(path, options);
  if (!response.ok) throw new Error((await response.text()).trim() || `${response.status} ${response.statusText}`);
  if (response.status === 204) return null;
  return response.json();
}

function message(text = '') { $('#globalMessage').textContent = text; }
function pct(value) { return `${Math.round((value || 0) * 100)}%`; }

function uploadForm(url, fields, fileFields, onProgress) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    const form = new FormData();
    for (const [k, v] of Object.entries(fields || {})) form.append(k, v);
    for (const [k, files] of Object.entries(fileFields || {})) for (const file of files) form.append(k, file);
    xhr.open('POST', url);
    xhr.upload.onprogress = event => {
      if (event.lengthComputable && onProgress) onProgress(event.loaded / event.total);
    };
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try { resolve(xhr.responseText ? JSON.parse(xhr.responseText) : null); }
        catch { resolve(null); }
      } else reject(new Error(xhr.responseText || `upload failed: ${xhr.status}`));
    };
    xhr.onerror = () => reject(new Error('network error during upload'));
    xhr.send(form);
  });
}

function hasPendingFileSelection() {
  return [...document.querySelectorAll('input[type="file"]')]
    .some(input => input.files && input.files.length > 0);
}

async function refresh() {
  try {
    const [system, jobs] = await Promise.all([api('/api/system'), api('/api/jobs')]);
    state.system = system; state.profiles = system.profiles || {}; state.jobs = jobs;
    renderSystem();
    // Replacing a file input clears its browser-owned FileList. Keep the job DOM
    // stable while any upload is staged; successful upload handlers clear their
    // input and the normal SSE/poll refresh resumes immediately afterward.
    if (!hasPendingFileSelection()) renderJobs();
    message('');
  } catch (error) { message(error.message); }
}

function scheduleRefresh(delay = 120) {
  clearTimeout(refreshTimer); refreshTimer = setTimeout(refresh, delay);
}

function renderSystem() {
  const badge = $('#workerBadge');
  const worker = state.system?.worker || {};
  if (worker.online && worker.healthy) {
    badge.className = 'badge online';
    const gpu = worker.gpu?.name ? ` · ${worker.gpu.name}` : '';
    badge.textContent = `GPU worker online${gpu}`;
  } else if (worker.online) {
    badge.className = 'badge warning'; badge.textContent = `Worker unhealthy · ${worker.error || 'check logs'}`;
  } else {
    badge.className = 'badge offline'; badge.textContent = 'GPU worker offline';
  }
}

function bind(root, selector, fn) {
  const el = $(selector, root);
  el.onclick = async () => {
    el.disabled = true;
    try { await fn(); scheduleRefresh(50); }
    catch (error) { alert(error.message); }
    finally { el.disabled = false; }
  };
}

function profileOptions(select, selected = 'balanced') {
  select.replaceChildren(...Object.values(state.profiles).map(profile => {
    const option = document.createElement('option'); option.value = profile.name;
    option.textContent = `${profile.name} · ≥${profile.frames} adaptive frames · ${profile.iterations} iters`;
    option.title = profile.description; option.selected = profile.name === selected; return option;
  }));
}

function renderAttempts(root, job) {
  const box = $('.attempts', root);
  if (!job.attempts?.length) { box.textContent = 'No attempts yet.'; return; }
  box.replaceChildren(...job.attempts.slice().reverse().map(a => {
    const row = document.createElement('div'); row.className = 'rowItem';
    const left = document.createElement('span');
    const active = a.number === job.activeAttempt ? ' · ACTIVE' : '';
    const metrics = [a.gaussianCount ? `${a.gaussianCount.toLocaleString()} Gaussians` : '', a.registrationRatio ? `${Math.round(a.registrationRatio * 100)}% registered` : ''].filter(Boolean).join(' · ');
    left.innerHTML = `Attempt ${a.number}${active} · <b>${a.state}</b> · ${a.profile || 'external PLY'}${metrics ? ` · ${metrics}` : ''}${a.error ? ` · ${a.error}` : ''}`;
    row.append(left);
    if (a.number !== job.activeAttempt && a.splatPath) {
      const button = document.createElement('button'); button.textContent = 'Activate'; button.className = 'subtle';
      button.onclick = async () => {
        button.disabled = true;
        try { await api(`/api/jobs/${job.id}/attempts/${a.number}/activate`, {method:'POST'}); await refresh(); }
        catch (error) { alert(error.message); }
        finally { button.disabled = false; }
      };
      row.append(button);
    }
    return row;
  }));
}

async function renderTasks(root, job) {
  const box = $('.tasks', root);
  try {
    const tasks = await api(`/api/jobs/${job.id}/tasks`);
    if (!tasks.length) { box.textContent = 'No processing tasks yet.'; return; }
    box.replaceChildren(...tasks.slice().reverse().map(t => {
      const row = document.createElement('div'); row.className = 'rowItem';
      row.innerHTML = `<span>${t.type} · <b>${t.state}</b> · ${pct(t.progress)} · delivery ${t.attemptCount}/${t.maxAttempts}</span><a target="_blank" href="/api/tasks/${t.id}/log">log</a>`;
      return row;
    }));
  } catch (e) { box.textContent = `Unable to load tasks: ${e.message}`; }
}

function renderPreviews(root, job) {
  const box = $('.previewGrid', root); box.replaceChildren();
  if (!['RENDER_READY','DATASET_BUILDING','COMPLETED'].includes(job.state)) return;
  const names = ['eye_00.png','eye_02.png','eye_04.png','eye_06.png','upper_00.png','upper_02.png','upper_04.png','upper_06.png'];
  for (const name of names) {
    const img = document.createElement('img'); img.loading = 'lazy'; img.alt = name; img.src = `/api/jobs/${job.id}/renders/${name}?r=${job.revision}`; box.append(img);
  }
}

function jobNode(job) {
  const root = $('#jobTemplate').content.firstElementChild.cloneNode(true);
  $('.name',root).textContent = job.name; $('.id',root).textContent = job.id; $('.state',root).textContent = job.state;
  $('.progressText',root).textContent = pct(job.progress); $('.progress-bar',root).style.width = pct(job.progress);
  $('.message',root).textContent = job.message || ''; $('.error',root).textContent = job.error || '';
  const profile = $('.profile',root); profileOptions(profile, job.attempts?.at(-1)?.profile || 'balanced');

  bind(root,'.refreshJob',refresh);
  bind(root,'.cancel',()=>api(`/api/jobs/${job.id}/cancel`,{method:'POST'}));
  $('.cancel',root).style.display = job.currentTaskId ? '' : 'none';

  bind(root,'.uploadVideo',async()=>{
    const input=$('.video',root); const file=input.files[0]; if(!file) throw new Error('Choose a video first.');
    const status=$('.uploadStatus',root);
    await uploadForm(`/api/jobs/${job.id}/video`,{profile:profile.value},{file:[file]},p=>status.textContent=`Uploading ${pct(p)}…`);
    input.value='';
    status.textContent='Upload complete; reconstruction queued.';
  });
  bind(root,'.uploadPly',async()=>{
    const input=$('.ply',root); const file=input.files[0]; if(!file)throw new Error('Choose a Gaussian-splat PLY.');
    await uploadForm(`/api/jobs/${job.id}/ply`,{}, {file:[file]}); input.value='';
  });
  bind(root,'.openEditor',async()=>{
    const active=job.attempts?.find(a=>a.number===job.activeAttempt);
    if(!active?.splatPath && !job.cleanedPath)throw new Error('No active splat is ready.');
    const load=job.cleanedPath?`/api/jobs/${job.id}/cleaned`:`/api/jobs/${job.id}/splat`;
    window.open(`/editor/?job=${encodeURIComponent(job.id)}&load=${encodeURIComponent(load)}`,'_blank','noopener');
  });
  bind(root,'.uploadCleaned',async()=>{
    const input=$('.cleaned',root); const file=input.files[0]; if(!file)throw new Error('Choose a cleaned Gaussian PLY.');
    await api(`/api/jobs/${job.id}/cleaned`,{method:'POST',headers:{'Content-Type':'application/octet-stream'},body:file}); input.value='';
  });
  bind(root,'.render',()=>api(`/api/jobs/${job.id}/render`,{method:'POST'}));
  bind(root,'.uploadBackgrounds',async()=>{
    const input=$('.backgrounds',root); const files=[...input.files]; if(!files.length)throw new Error('Choose at least one background image.');
    await uploadForm(`/api/jobs/${job.id}/backgrounds`,{}, {files}); input.value='';
  });
  bind(root,'.dataset',()=>api(`/api/jobs/${job.id}/dataset`,{method:'POST'}));

  const dl=$('.download',root);dl.href=`/api/jobs/${job.id}/dataset.zip`;dl.style.display=job.datasetZip?'block':'none';
  renderAttempts(root,job); renderPreviews(root,job); void renderTasks(root,job);
  return root;
}

function renderJobs() {
  $('#jobs').replaceChildren(...state.jobs.map(jobNode));
}

const createJobButton = $('#createJob');
const jobNameInput = $('#jobName');
if (!createJobButton || !jobNameInput) {
  throw new Error('ProdSplat dashboard markup is incomplete: createJob/jobName element missing. Hard-refresh /app/.');
}
createJobButton.onclick = async () => {
  const name = jobNameInput.value.trim();
  if (!name) { message('Enter a product/job name first.'); jobNameInput.focus(); return; }
  createJobButton.disabled = true;
  try {
    await api('/api/jobs',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name})});
    jobNameInput.value = '';
    await refresh();
  } catch(error) {
    message(error.message);
  } finally {
    createJobButton.disabled = false;
  }
};

const stream = new EventSource('/api/events');
stream.addEventListener('job', () => scheduleRefresh());
stream.onerror = () => { /* Browser reconnects automatically; polling below is fallback. */ };

refresh();
setInterval(refresh, 10000);
