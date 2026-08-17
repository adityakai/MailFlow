const App = (() => {
  let currentUser = null;
  let threads = [];
  let activeThread = null;
  let currentFilter = 'all';
  let searchQuery = '';
  let toastTimer = null;
  let refreshTimer = null;

  async function init() {
    initMarketingAnimations();
    const { user } = await api('/api/me').catch(() => ({ user: null }));

    if (!user) {
      show('login-screen');
      hide('app');
      return;
    }

    currentUser = user;
    show('app');
    hide('login-screen');
    document.getElementById('agent-name').textContent = user.name || '';
    await loadThreads();

    refreshTimer = setInterval(async () => {
      await loadThreads();
      if (activeThread) {
        const data = await api(`/api/threads/${activeThread.id}`).catch(() => null);
        if (data) renderDetail(data.thread, data.contact, data.messages);
      }
    }, 30000);
  }

  async function api(path, opts = {}) {
    const res = await fetch(path, {
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      ...opts,
      body: opts.body ? JSON.stringify(opts.body) : undefined,
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({}));
      throw new Error(err.error || `HTTP ${res.status}`);
    }
    return res.json();
  }

  async function loadThreads() {
    try {
      const data = await api('/api/threads');
      threads = data.threads || [];
      updateCounts();
      renderList();
    } catch (e) {
      toast('Could not load threads: ' + e.message);
    }
  }

  function updateCounts() {
    setText('cnt-all', threads.length);
    setText('cnt-ai', threads.filter(t => t.status === 'ai').length);
    setText('cnt-human', threads.filter(t => t.status === 'human').length);
  }

  function renderList() {
    const container = document.getElementById('thread-list');
    if (!container) return;

    let filtered = threads;
    if (currentFilter === 'ai') filtered = threads.filter(t => t.status === 'ai');
    if (currentFilter === 'human') filtered = threads.filter(t => t.status === 'human');
    if (searchQuery) {
      const q = searchQuery.toLowerCase();
      filtered = filtered.filter(t =>
        String(t.contact_name || '').toLowerCase().includes(q) ||
        String(t.subject || '').toLowerCase().includes(q) ||
        String(t.last_message_preview || '').toLowerCase().includes(q)
      );
    }

    if (!filtered.length) {
      container.innerHTML = '';
      show('thread-empty');
      return;
    }

    hide('thread-empty');
    container.innerHTML = filtered.map(t => {
      const isAI = t.status === 'ai';
      const isActive = activeThread?.id === t.id;
      return `
        <div class="thread-item ${isActive ? 'active' : ''} mode-${esc(t.status)}" onclick="App.openThread('${js(t.id)}')">
          <div class="ti-header">
            <div class="ti-dot ${isAI ? '' : 'coral'}"></div>
            <div class="ti-from">${esc(t.contact_name || 'Unknown contact')}</div>
            <div class="ti-time">${timeAgo(t.updated_at)}</div>
            <button class="ti-delete-btn" onclick="event.stopPropagation(); App.deleteThread('${js(t.id)}', '${js(t.contact_name || 'contact')}')" title="Delete thread">x</button>
          </div>
          <div class="ti-subject">${esc(t.subject || 'Untitled thread')}</div>
          <div class="ti-preview">${esc(t.last_message_preview || '-')}</div>
          <div class="ti-tags">
            ${isAI ? '<span class="tag ai">AI</span>' : `<span class="tag human">${esc(t.claimed_by_name || 'Human')}</span>`}
            ${t.message_count > 1 ? `<span class="tag chain">${t.message_count} msgs</span>` : ''}
            ${t.is_primary ? '<span class="tag good">Primary</span>' : '<span class="tag warn">Check inbox</span>'}
          </div>
        </div>
      `;
    }).join('');
  }

  async function openThread(threadId) {
    try {
      const data = await api(`/api/threads/${threadId}`);
      activeThread = data.thread;
      renderList();
      hide('detail-empty');
      show('detail-content');
      renderDetail(data.thread, data.contact, data.messages || []);
    } catch (e) {
      toast(e.message.includes('403') || e.message.includes('Access denied')
        ? 'You can only view threads you started or claimed.'
        : 'Error: ' + e.message);
    }
  }

  function renderDetail(thread, contact, messages) {
    const el = document.getElementById('detail-content');
    if (!el) return;

    const isOwner = thread.owner_id === currentUser.id;
    const isClaimed = thread.claimed_by === currentUser.id;
    const isAiMode = !!thread.ai_mode;
    const isHuman = thread.status === 'human';

    el.innerHTML = `
      <div class="detail-header">
        <div>
          <div class="detail-subject">${esc(thread.subject || 'Untitled thread')}</div>
          <div class="detail-meta">
            <span>${esc(contact.name || 'Contact')} &lt;${esc(contact.email || '')}&gt;</span>
            <span>${messages.length} message${messages.length === 1 ? '' : 's'}</span>
            ${thread.is_primary ? '<span class="good">Lands in Primary</span>' : '<span class="warn">Check deliverability</span>'}
            <span>Score: <strong style="color:var(--green)">${esc(thread.deliv_score || 0)}%</strong></span>
          </div>
        </div>
        <div class="detail-actions">
          ${isOwner && !isHuman ? `<button class="btn btn-sm btn-human" onclick="App.claimThread('${js(thread.id)}')">Take over</button>` : ''}
          ${(isOwner || isClaimed) && isHuman ? `<button class="btn btn-sm btn-ai" onclick="App.releaseThread('${js(thread.id)}')">Return to AI</button>` : ''}
          ${isOwner ? `<button class="btn btn-sm btn-danger" onclick="App.deleteThread('${js(thread.id)}', '${js(contact.name || 'contact')}')">Delete</button>` : ''}
        </div>
      </div>

      ${isOwner ? `
      <div class="mode-toggle">
        <div class="mode-toggle-label"><strong>Reply mode</strong> - toggle who handles the next reply</div>
        <div class="toggle-switch">
          <button class="toggle-opt ${isAiMode ? 'active-ai' : ''}" onclick="App.toggleAiMode('${js(thread.id)}', true)">AI</button>
          <button class="toggle-opt ${!isAiMode ? 'active-human' : ''}" onclick="App.toggleAiMode('${js(thread.id)}', false)">Me</button>
        </div>
      </div>` : ''}

      <div id="escalation-banner" class="escalation-banner hidden"></div>
      <div class="section-title">Conversation chain</div>
      <div id="messages-chain">${messages.map(m => renderMessage(m, contact)).join('')}</div>

      ${(isOwner || isClaimed) ? `
      <div class="section-title">Compose reply</div>
      <div class="composer ${isAiMode ? '' : 'human-mode'}" id="composer">
        <div class="composer-top">
          <span class="clabel">From:</span>
          <strong style="font-size:13px">${esc(currentUser.name || 'Me')}</strong>
          <span class="clabel" style="margin-left:auto">via</span>
          <span class="clabel" style="color:${isAiMode ? 'var(--green)' : 'var(--coral)'}">${isAiMode ? 'AI draft' : 'Manual'}</span>
          ${isAiMode ? `<button class="btn btn-sm btn-ai" onclick="App.generateDraft('${js(thread.id)}')" id="draft-btn">Generate draft</button>` : ''}
        </div>
        <textarea class="composer-textarea" id="composer-text" placeholder="${isAiMode ? 'Generate a draft, then review before sending...' : 'Write your reply here...'}"></textarea>
        <div class="composer-footer">
          <div class="score-widget">
            <span>Send score</span>
            <div class="score-bar"><div class="score-fill" style="width:${esc(thread.deliv_score || 0)}%"></div></div>
            <span style="color:var(--green)">${esc(thread.deliv_score || 0)}%</span>
          </div>
          <span style="flex:1"></span>
          <button class="btn btn-ghost btn-sm" onclick="App.clearDraft()">Clear</button>
          <button class="btn btn-primary btn-sm" onclick="App.sendReply('${js(thread.id)}', ${isAiMode})">Send reply</button>
        </div>
      </div>` : `
      <div class="message-bubble">Only <strong>${esc(thread.owner_name || 'the owner')}</strong> can reply to this thread.</div>`}
    `;
  }

  function renderMessage(msg, contact) {
    const roleMap = {
      'outbound-ai': { cls: 'outbound-ai', avCls: 'ai', avLetter: 'AI', badgeCls: 'ai', label: 'AI sent' },
      'outbound-human': { cls: 'outbound-human', avCls: 'human', avLetter: 'ME', badgeCls: 'human', label: 'Human sent' },
      inbound: { cls: 'inbound', avCls: 'contact', avLetter: (contact.name || '?')[0], badgeCls: 'in', label: 'Received' },
    };
    const r = roleMap[msg.role] || roleMap.inbound;
    const hid = `headers-${msg.id}`;
    return `
      <div class="message-bubble ${r.cls}">
        <div class="msg-header">
          <div class="avatar ${r.avCls}">${esc(r.avLetter)}</div>
          <div class="msg-from">${esc(msg.from_name || '')}</div>
          <span class="msg-role-badge ${r.badgeCls}">${r.label}</span>
          <div class="msg-time">${msg.sent_at ? new Date(msg.sent_at * 1000).toLocaleString() : ''}</div>
        </div>
        <div class="msg-body">${esc(msg.body || '')}</div>
        <button class="show-headers-btn" onclick="toggleHeaders('${hid}')">show thread headers</button>
        <div class="msg-headers" id="${hid}">
          ${msg.message_id_header ? `<code>Message-ID: ${esc(msg.message_id_header)}</code>` : ''}
          ${msg.in_reply_to ? `<code>In-Reply-To: ${esc(msg.in_reply_to)}</code>` : ''}
          ${msg.references_header ? `<code>References: ${esc(msg.references_header)}</code>` : ''}
        </div>
      </div>
    `;
  }

  async function deleteThread(threadId, contactName) {
    if (!confirm(`Delete thread with ${contactName}? This cannot be undone.`)) return;
    try {
      await api(`/api/threads/${threadId}`, { method: 'DELETE' });
      if (activeThread?.id === threadId) {
        activeThread = null;
        hide('detail-content');
        show('detail-empty');
      }
      toast('Thread deleted');
      await loadThreads();
    } catch (e) {
      toast('Error: ' + e.message);
    }
  }

  function openBulkSend() {
    setValue('bulk-contacts', '');
    setValue('bulk-subject', '');
    setValue('bulk-body', '');
    const preview = document.getElementById('bulk-preview');
    if (preview) preview.innerHTML = '';
    document.getElementById('bulk-progress')?.classList.add('hidden');
    show('bulk-send-modal');
  }

  function previewBulkContacts() {
    const raw = value('bulk-contacts');
    const preview = document.getElementById('bulk-preview');
    if (!preview) return;
    if (!raw) {
      preview.innerHTML = '';
      return;
    }

    const contacts = parseBulkContacts(raw);
    if (!contacts.length) {
      preview.innerHTML = '<div style="color:#ef8c8c">Could not parse contacts. Use one email per line or CSV format.</div>';
      return;
    }
    if (contacts.length > 100) {
      preview.innerHTML = `<div style="color:#ef8c8c">Max 100 contacts. You have ${contacts.length}.</div>`;
      return;
    }

    preview.innerHTML = `
      <div style="color:var(--green);margin-bottom:6px">${contacts.length} contact${contacts.length === 1 ? '' : 's'} ready</div>
      <div style="max-height:120px;overflow-y:auto">
        ${contacts.slice(0, 10).map(c => `<div>${esc(c.name || '-')} &lt;${esc(c.email)}&gt; ${c.company ? '- ' + esc(c.company) : ''}</div>`).join('')}
        ${contacts.length > 10 ? `<div>and ${contacts.length - 10} more</div>` : ''}
      </div>
    `;
  }

  function parseBulkContacts(raw) {
    return raw.split('\n').map(line => line.trim()).filter(Boolean).map(line => {
      if (line.includes(',')) {
        const [email, name = '', company = ''] = line.split(',').map(part => part.trim());
        return email.includes('@') ? { email, name, company } : null;
      }
      return line.includes('@') ? { email: line, name: '', company: '' } : null;
    }).filter(Boolean);
  }

  async function submitBulkSend() {
    const contacts = parseBulkContacts(value('bulk-contacts'));
    const subject = value('bulk-subject');
    const body = value('bulk-body');
    if (!contacts.length || !subject || !body) {
      toast('Contacts, subject, and message are all required');
      return;
    }
    if (contacts.length > 100) {
      toast('Max 100 contacts per send');
      return;
    }

    const progress = document.getElementById('bulk-progress');
    const sendBtn = document.getElementById('bulk-send-btn');
    if (progress) {
      progress.classList.remove('hidden');
      progress.textContent = `Sending to ${contacts.length} contacts...`;
    }
    if (sendBtn) {
      sendBtn.disabled = true;
      sendBtn.textContent = 'Sending...';
    }

    try {
      const result = await api('/api/bulk-send', {
        method: 'POST',
        body: { contacts, subject, body, delayMs: 1500 },
      });
      if (progress) progress.textContent = `Sent ${result.sent} of ${result.total} emails`;
      toast(`Bulk send complete: ${result.sent} emails sent`);
      await loadThreads();
      setTimeout(() => closeModal('bulk-send-modal'), 1600);
    } catch (e) {
      if (progress) progress.textContent = 'Error: ' + e.message;
      toast('Bulk send error: ' + e.message);
    } finally {
      if (sendBtn) {
        sendBtn.disabled = false;
        sendBtn.textContent = 'Send to all';
      }
    }
  }

  async function claimThread(threadId) {
    try {
      await api(`/api/threads/${threadId}/claim`, { method: 'POST' });
      toast('Thread claimed. You are now handling replies.');
      await openThread(threadId);
      await loadThreads();
    } catch (e) {
      toast('Error: ' + e.message);
    }
  }

  async function releaseThread(threadId) {
    try {
      await api(`/api/threads/${threadId}/release`, { method: 'POST' });
      toast('Returned to AI management');
      await openThread(threadId);
      await loadThreads();
    } catch (e) {
      toast('Error: ' + e.message);
    }
  }

  async function toggleAiMode(threadId, aiMode) {
    try {
      await api(`/api/threads/${threadId}/toggle-ai`, { method: 'POST', body: { aiMode } });
      toast(aiMode ? 'AI will handle the next reply' : 'You will handle the next reply');
      await openThread(threadId);
      await loadThreads();
    } catch (e) {
      toast('Error: ' + e.message);
    }
  }

  async function generateDraft(threadId) {
    const btn = document.getElementById('draft-btn');
    const ta = document.getElementById('composer-text');
    if (!btn || !ta) return;

    btn.textContent = 'Generating...';
    btn.disabled = true;
    ta.value = '';
    ta.placeholder = 'AI is reading the full thread chain...';

    try {
      const { draft, escalation } = await api(`/api/threads/${threadId}/draft`, { method: 'POST' });
      ta.value = draft || '';
      if (escalation?.escalate) {
        const banner = document.getElementById('escalation-banner');
        banner.innerHTML = `AI suggests human review: ${esc(escalation.reason)}`;
        banner.classList.remove('hidden');
      }
      toast('AI draft ready. Review before sending.');
    } catch (e) {
      toast('Draft error: ' + e.message);
    } finally {
      btn.textContent = 'Generate draft';
      btn.disabled = false;
    }
  }

  async function sendReply(threadId, isAiMode) {
    const ta = document.getElementById('composer-text');
    const body = ta?.value?.trim();
    if (!body) {
      toast('Write something first');
      return;
    }
    try {
      await api(`/api/threads/${threadId}/reply`, {
        method: 'POST',
        body: { body, role: isAiMode ? 'outbound-ai' : 'outbound-human' },
      });
      toast('Reply sent. Thread chain updated.');
      ta.value = '';
      await openThread(threadId);
      await loadThreads();
    } catch (e) {
      toast('Send error: ' + e.message);
    }
  }

  function clearDraft() {
    const ta = document.getElementById('composer-text');
    if (ta) ta.value = '';
  }

  function newThread() {
    show('new-thread-modal');
  }

  async function submitNewThread() {
    const contactEmail = value('nt-email');
    const contactName = value('nt-name');
    const contactCompany = value('nt-company');
    const subject = value('nt-subject');
    const body = value('nt-body');

    if (!contactEmail || !subject || !body) {
      toast('Email, subject, and message are required');
      return;
    }

    try {
      const { threadId } = await api('/api/threads', {
        method: 'POST',
        body: { contactEmail, contactName, contactCompany, subject, body },
      });
      closeModal('new-thread-modal');
      toast('Thread started. First email sent.');
      await loadThreads();
      await openThread(threadId);
    } catch (e) {
      toast('Error: ' + e.message);
    }
  }

  function setFilter(filter, el) {
    currentFilter = filter;
    document.querySelectorAll('.rail-item').forEach(n => n.classList.remove('active'));
    el?.classList.add('active');
    renderList();
  }

  function search(q) {
    searchQuery = q || '';
    renderList();
  }

  async function logout() {
    clearInterval(refreshTimer);
    await api('/api/logout', { method: 'POST' });
    location.reload();
  }

  function initMarketingAnimations() {
    document.querySelectorAll('.reveal-words').forEach(el => {
      const text = el.dataset.text || el.textContent || '';
      const withAsterisk = el.dataset.asterisk === 'true';
      el.innerHTML = text.split(' ').map((word, index) => {
        const last = withAsterisk && index === text.split(' ').length - 1;
        return `<span class="word" style="animation-delay:${index * 0.08}s">${esc(word)}${last ? '<span class="asterisk">*</span>' : ''}</span>`;
      }).join(' ');
    });

    document.querySelectorAll('.reveal-multi').forEach(el => {
      const words = Array.from(el.childNodes).flatMap(node => {
        const tag = node.nodeName === 'EM' ? 'em' : 'span';
        return String(node.textContent || '').trim().split(/\s+/).filter(Boolean).map(word => ({ word, tag }));
      });
      el.innerHTML = words.map(({ word, tag }, index) => `<${tag} class="word" style="animation-delay:${index * 0.08}s">${esc(word)}&nbsp;</${tag}>`).join('');
    });

    document.querySelectorAll('.letter-reveal').forEach(el => {
      const text = el.dataset.letterText || '';
      el.innerHTML = [...text].map(ch => `<span>${ch === ' ' ? '&nbsp;' : esc(ch)}</span>`).join('');
    });

    const observer = new IntersectionObserver(entries => {
      entries.forEach(entry => {
        if (entry.isIntersecting) entry.target.classList.add('in-view');
      });
    }, { threshold: 0.18 });

    document.querySelectorAll('.scroll-reveal, .motion-card').forEach(el => observer.observe(el));

    const updateLetters = () => {
      document.querySelectorAll('.letter-reveal').forEach(el => {
        const rect = el.getBoundingClientRect();
        const progress = clamp((window.innerHeight * 0.82 - rect.top) / (window.innerHeight * 0.45), 0, 1);
        const letters = el.querySelectorAll('span');
        letters.forEach((letter, index) => {
          const local = index / Math.max(letters.length - 1, 1);
          letter.style.opacity = progress > local - 0.08 ? String(clamp((progress - local + 0.08) / 0.12, 0.22, 1)) : '0.22';
        });
      });
    };
    window.addEventListener('scroll', updateLetters, { passive: true });
    updateLetters();
  }

  function startImmersiveCanvas() {
    const canvas = document.getElementById('immersive-canvas');
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    const particles = Array.from({ length: 70 }, () => ({
      x: Math.random(),
      y: Math.random(),
      z: Math.random(),
      speed: 0.00035 + Math.random() * 0.0007,
      size: 0.8 + Math.random() * 1.9,
    }));

    function resize() {
      canvas.width = window.innerWidth * devicePixelRatio;
      canvas.height = window.innerHeight * devicePixelRatio;
      canvas.style.width = window.innerWidth + 'px';
      canvas.style.height = window.innerHeight + 'px';
      ctx.setTransform(devicePixelRatio, 0, 0, devicePixelRatio, 0, 0);
    }

    function draw(time) {
      const w = window.innerWidth;
      const h = window.innerHeight;
      ctx.clearRect(0, 0, w, h);

      const glow = ctx.createRadialGradient(w * 0.54, h * 0.35, 0, w * 0.54, h * 0.35, Math.max(w, h) * 0.72);
      glow.addColorStop(0, 'rgba(225,224,204,0.13)');
      glow.addColorStop(0.38, 'rgba(217,134,105,0.05)');
      glow.addColorStop(1, 'rgba(0,0,0,0)');
      ctx.fillStyle = glow;
      ctx.fillRect(0, 0, w, h);

      ctx.strokeStyle = 'rgba(225,224,204,0.09)';
      for (let i = 0; i < 5; i++) {
        ctx.beginPath();
        ctx.ellipse(w * 0.55, h * 0.5, 180 + i * 95, 42 + i * 21, time * 0.00008 + i, 0, Math.PI * 2);
        ctx.stroke();
      }

      particles.forEach(p => {
        p.y -= p.speed * (16 + p.z * 34);
        p.x += Math.sin(time * 0.0005 + p.z * 9) * 0.00018;
        if (p.y < -0.05) {
          p.y = 1.05;
          p.x = Math.random();
        }
        ctx.beginPath();
        ctx.fillStyle = `rgba(225,224,204,${0.18 + p.z * 0.48})`;
        ctx.arc(p.x * w, p.y * h, p.size, 0, Math.PI * 2);
        ctx.fill();
      });

      requestAnimationFrame(draw);
    }

    resize();
    window.addEventListener('resize', resize);
    requestAnimationFrame(draw);
  }

  function show(id) { document.getElementById(id)?.classList.remove('hidden'); }
  function hide(id) { document.getElementById(id)?.classList.add('hidden'); }
  function value(id) { return document.getElementById(id)?.value?.trim() || ''; }
  function setValue(id, val) { const el = document.getElementById(id); if (el) el.value = val; }
  function setText(id, val) { const el = document.getElementById(id); if (el) el.textContent = val; }
  function closeModal(id) { document.getElementById(id)?.classList.add('hidden'); }
  function esc(s) { return String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;'); }
  function js(s) { return esc(String(s ?? '').replace(/\\/g, '\\\\').replace(/'/g, "\\'").replace(/\r?\n/g, ' ')); }
  function clamp(value, min, max) { return Math.min(max, Math.max(min, value)); }

  function timeAgo(unixTs) {
    const ts = Number(unixTs) || Math.floor(Date.now() / 1000);
    const diff = Math.floor(Date.now() / 1000) - ts;
    if (diff < 60) return 'just now';
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
    if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
    return `${Math.floor(diff / 86400)}d ago`;
  }

  function toast(msg) {
    const el = document.getElementById('toast');
    if (!el) return;
    el.textContent = msg;
    el.classList.add('show');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => el.classList.remove('show'), 3500);
  }

  return {
    init,
    openThread,
    claimThread,
    releaseThread,
    toggleAiMode,
    generateDraft,
    sendReply,
    clearDraft,
    deleteThread,
    openBulkSend,
    previewBulkContacts,
    submitBulkSend,
    newThread,
    submitNewThread,
    closeModal,
    setFilter,
    search,
    logout,
  };
})();

function toggleHeaders(id) {
  document.getElementById(id)?.classList.toggle('open');
}

document.addEventListener('DOMContentLoaded', App.init);
