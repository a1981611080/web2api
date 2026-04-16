(function () {
  const page = document.body.dataset.page || '';

  function escapeHTML(value) {
    return String(value ?? '')
      .replaceAll('&', '&amp;')
      .replaceAll('<', '&lt;')
      .replaceAll('>', '&gt;')
      .replaceAll('"', '&quot;')
      .replaceAll("'", '&#39;');
  }

  function adminLayout(title, subtitle, content) {
    document.body.innerHTML = `
      <div class="layout">
        <aside class="sidebar">
          <div class="brand">
            <h1>web2api Admin</h1>
            <p>插件账号驱动的 Web 转 API 管理台</p>
          </div>
          <nav class="nav">
            <a href="/admin" data-nav="overview">总览</a>
            <a href="/admin/plugins" data-nav="plugins">插件</a>
            <a href="/admin/accounts" data-nav="accounts">账号</a>
            <a href="/admin/clients" data-nav="clients">客户端</a>
            <a href="/admin/test" data-nav="test">测试</a>
            <a href="/admin/status" data-nav="status">运行状态</a>
          </nav>
        </aside>
        <main class="content">
          <div class="header">
            <div>
              <h2>${escapeHTML(title)}</h2>
              <p class="muted">${escapeHTML(subtitle)}</p>
            </div>
          </div>
          ${content}
        </main>
      </div>
    `;
    const active = document.querySelector(`[data-nav="${page}"]`);
    if (active) active.classList.add('active');
  }

  async function getJSON(url, options) {
    const res = await fetch(url, options);
    return res.json();
  }

  function sourceBadges(prefixes) {
    return (prefixes || []).map((value) => `<span class="pill">${escapeHTML(value)}</span>`).join('');
  }

  function pluginByID(items, id) {
    return (items || []).find((item) => item.manifest && item.manifest.id === id);
  }

  function renderModelCheckboxes(pluginItem, selected) {
    const models = pluginItem && pluginItem.manifest ? (pluginItem.manifest.models || []) : [];
    if (!models.length) {
      return '<div class="muted">当前插件没有声明模型。</div>';
    }
    return models.map((model) => `
      <label><input type="checkbox" name="selected_models" value="${escapeHTML(model.id)}" ${selected.includes(model.id) ? 'checked' : ''}> ${escapeHTML(model.name || model.id)}</label>
    `).join('');
  }

  async function renderOverview() {
    const status = await getJSON('/api/admin/status');
    adminLayout('后台总览', '查看当前插件、账号和操作入口。', `
      <div class="grid">
        <section class="card"><div class="muted">已就绪插件</div><div class="stat">${status.plugins.items.filter((p) => p.status === 'ready').length}</div></section>
        <section class="card"><div class="muted">内部源总数</div><div class="stat">${status.sources.total}</div></section>
        <section class="card"><div class="muted">启用内部源</div><div class="stat">${status.sources.enabled}</div></section>
        <section class="card"><div class="muted">可用账号</div><div class="stat">${status.accounts.active}</div></section>
        <section class="card"><div class="muted">客户端账号</div><div class="stat">${status.consumers.total}</div></section>
        <section class="card"><div class="muted">模型目录</div><div class="stat">${status.catalog_models.total}</div></section>
      </div>
      <div class="grid" style="margin-top:20px;">
        <section class="card">
          <h3>快速入口</h3>
          <div class="nav">
            <a href="/admin/plugins">管理插件</a>
            <a href="/admin/clients">管理客户端账号</a>
            <a href="/admin/test">测试转换接口</a>
            <a href="/admin/status">查看运行状态</a>
          </div>
        </section>
        <section class="card">
          <h3>当前内部源</h3>
          <div class="list">
            ${status.sources.items.map((item) => `<div class="item"><h3>${escapeHTML(item.name)} <span class="pill">${item.enabled ? 'enabled' : 'disabled'}</span></h3><div class="muted">plugin: ${escapeHTML(item.plugin_id || '(none)')}</div><div>${sourceBadges(item.models || [])}</div></div>`).join('') || '<div class="muted">还没有内部源。</div>'}
          </div>
        </section>
      </div>
    `);
  }

  async function renderPlugins() {
    const payload = await getJSON('/api/admin/plugins');
    adminLayout('插件管理', '扫描 plugins 目录，查看插件 manifest 与错误信息。', `
      <section class="card">
        <div class="toolbar">
          <button id="scan-plugins">重新扫描 plugins</button>
        </div>
        <div id="plugin-list" class="list"></div>
      </section>
    `);

    const list = document.getElementById('plugin-list');
    const draw = (items) => {
      list.innerHTML = items.map((item) => `
        <div class="item">
          <h3>${escapeHTML(item.manifest.name || item.path)} <span class="pill">${escapeHTML(item.status || 'unknown')}</span></h3>
          <div class="muted">id: ${escapeHTML(item.manifest.id || '(missing)')} | version: ${escapeHTML(item.manifest.version || '-')} | path: ${escapeHTML(item.path || '-')}</div>
          <div style="margin-top:8px;"><strong>能力:</strong> ${(item.manifest.capabilities || []).map((cap) => `<span class="pill">${escapeHTML(cap)}</span>`).join('') || '<span class="muted">无能力声明</span>'}</div>
          <div style="margin-top:8px;"><strong>模型:</strong> ${(item.manifest.models || []).map((model) => `<span class="pill">${escapeHTML(model.id)}</span>`).join('') || '<span class="muted">无模型声明</span>'}</div>
          <div style="margin-top:8px;"><strong>账号字段:</strong> ${(item.manifest.account_fields || []).map((field) => `<span class="pill">${escapeHTML(field.key)}${field.required ? ' *' : ''}</span>`).join('') || '<span class="muted">无账号字段声明</span>'}</div>
          ${item.manifest.description ? `<div class="muted" style="margin-top:8px;">${escapeHTML(item.manifest.description)}</div>` : ''}
          ${item.error ? `<div class="muted" style="margin-top:8px;color:#fecaca;">error: ${escapeHTML(item.error)}</div>` : ''}
        </div>
      `).join('') || '<div class="muted">plugins 目录下没有插件。</div>';
    };
    draw(payload.data || []);

    document.getElementById('scan-plugins').addEventListener('click', async () => {
      const scanned = await getJSON('/api/admin/plugins/scan', { method: 'POST' });
      draw(scanned.data || []);
    });
  }

  async function renderAccounts() {
    const [accountsPayload, pluginsPayload] = await Promise.all([
      getJSON('/api/admin/accounts'),
      getJSON('/api/admin/plugins')
    ]);
    adminLayout('账号管理', '管理插件账号、可用模型和连通性验证。', `
      <div class="grid">
        <section class="card">
          <h3>新增或更新账号</h3>
          <form id="account-form">
            <label>账号 ID<input name="id" placeholder="例如 grok-001"></label>
            <label>所属插件<select name="plugin_id"></select></label>
            <label>显示名称<input name="name" placeholder="例如 主账号"></label>
            <div>
              <div class="muted" style="margin-bottom:8px;">该账号启用模型</div>
              <div id="account-models"></div>
            </div>
            <label>验证消息<input name="validation_message" placeholder="例如 你好，请回复ok"></label>
            <div id="account-fields"></div>
            <label>状态<select name="status"><option value="active">active</option><option value="cooling">cooling</option><option value="disabled">disabled</option></select></label>
            <label>优先级<input name="priority" type="number" value="0"></label>
            <label>额度上限<input name="max_requests" type="number" value="0"></label>
            <div class="toolbar">
              <button type="submit">保存账号</button>
              <button type="button" class="secondary" id="account-form-reset">清空表单</button>
            </div>
          </form>
        </section>
        <section class="card">
          <h3>账号列表</h3>
          <div id="account-list" class="list"></div>
        </section>
      </div>
    `);

    const pluginSelect = document.querySelector('select[name="plugin_id"]');
    (pluginsPayload.data || []).forEach((item) => {
      const option = document.createElement('option');
      option.value = item.manifest.id;
      option.textContent = `${item.manifest.name} (${item.manifest.id})`;
      pluginSelect.appendChild(option);
    });
    const modelBox = document.getElementById('account-models');
    const selectedModels = [];
    const drawAccountModels = () => {
      modelBox.innerHTML = renderModelCheckboxes(pluginByID(pluginsPayload.data || [], pluginSelect.value), selectedModels);
    };
    pluginSelect.addEventListener('change', () => {
      selectedModels.length = 0;
      drawAccountModels();
      drawAccountFields();
    });

    const fieldBox = document.getElementById('account-fields');
    const drawAccountFields = () => {
      const pluginItem = pluginByID(pluginsPayload.data || [], pluginSelect.value);
      const fields = pluginItem && pluginItem.manifest ? (pluginItem.manifest.account_fields || []) : [];
      fieldBox.innerHTML = fields.map((field) => `<label>${escapeHTML(field.label || field.key)}<input name="account_field_${escapeHTML(field.key)}" type="text" placeholder="${escapeHTML(field.placeholder || '')}"></label>${field.help ? `<div class="muted" style="margin-top:-8px;margin-bottom:10px;">${escapeHTML(field.help)}</div>` : ''}`).join('') || '<div class="muted">当前来源插件没有声明账号字段。</div>';
    };
    drawAccountModels();
    drawAccountFields();

    const list = document.getElementById('account-list');
    const draw = (items) => {
      list.innerHTML = items.map((item) => `
        <div class="item">
          <h3>${escapeHTML(item.name)} <span class="pill">${escapeHTML(item.status)}</span></h3>
          <div class="muted">id: ${escapeHTML(item.id)} | plugin: ${escapeHTML(item.plugin_id || '-')} | priority: ${escapeHTML(item.priority || 0)}</div>
          <div style="margin:8px 0;">${sourceBadges(item.models || [])}</div>
          <div class="muted">validation_message: ${escapeHTML(item.validation_message || '-')}</div>
          <div class="muted">fields: ${escapeHTML(Object.keys(item.fields || {}).join(', ') || '-')}</div>
          <div class="muted">used/max: ${escapeHTML(item.used_requests || 0)} / ${escapeHTML(item.max_requests || 0)}</div>
          <div class="muted">last error: ${escapeHTML(item.last_error || '-')}</div>
          <div class="toolbar" style="margin-top:10px;">
            <button data-account-validate="${escapeHTML(item.id)}">验证</button>
            <button data-success="${escapeHTML(item.id)}">标记成功</button>
            <button class="secondary" data-failure="${escapeHTML(item.id)}">标记失败并冷却</button>
            <button data-account-edit="${escapeHTML(item.id)}">编辑</button>
            <button class="secondary" data-account-delete="${escapeHTML(item.id)}">删除</button>
          </div>
        </div>
      `).join('') || '<div class="muted">还没有账号。</div>';

      list.querySelectorAll('[data-success]').forEach((button) => {
        button.addEventListener('click', async () => {
          await getJSON(`/api/admin/accounts/${button.dataset.success}/success`, { method: 'POST' });
          const next = await getJSON('/api/admin/accounts');
          draw(next.data || []);
        });
      });
      list.querySelectorAll('[data-failure]').forEach((button) => {
        button.addEventListener('click', async () => {
          await getJSON(`/api/admin/accounts/${button.dataset.failure}/failure`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ error: 'manual failure feedback', cooldown_seconds: 300 })
          });
          const next = await getJSON('/api/admin/accounts');
          draw(next.data || []);
        });
      });
      list.querySelectorAll('[data-account-edit]').forEach((button) => {
        button.addEventListener('click', () => {
          const accountItem = items.find((it) => it.id === button.dataset.accountEdit);
          if (!accountItem) return;
          document.querySelector('input[name="id"]').value = accountItem.id || '';
          pluginSelect.value = accountItem.plugin_id || '';
          selectedModels.length = 0;
          (accountItem.models || []).forEach((m) => selectedModels.push(m));
          drawAccountModels();
          drawAccountFields();
          document.querySelector('input[name="name"]').value = accountItem.name || '';
          document.querySelector('input[name="validation_message"]').value = accountItem.validation_message || '';
          document.querySelector('select[name="status"]').value = accountItem.status || 'active';
          document.querySelector('input[name="priority"]').value = accountItem.priority || 0;
          document.querySelector('input[name="max_requests"]').value = accountItem.max_requests || 0;
          Object.entries(accountItem.fields || {}).forEach(([key, value]) => {
            const input = document.querySelector(`input[name="account_field_${key}"]`);
            if (input) input.value = value;
          });
        });
      });
      list.querySelectorAll('[data-account-validate]').forEach((button) => {
        button.addEventListener('click', async () => {
          const accountItem = items.find((it) => it.id === button.dataset.accountValidate);
          const payload = {
            model: (accountItem && accountItem.models && accountItem.models[0]) || '',
            message: (accountItem && accountItem.validation_message) || ''
          };
          const res = await fetch(`/api/admin/accounts/${button.dataset.accountValidate}/validate`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
          });
          const data = await res.json();
          if (!res.ok) {
            alert(`验证失败: ${data.error && data.error.message ? data.error.message : 'unknown error'}`);
            return;
          }
          alert(`验证成功\n模型: ${data.model}\n账号: ${data.account_id || '-'}\n预览: ${data.preview || ''}`);
        });
      });
      list.querySelectorAll('[data-account-delete]').forEach((button) => {
        button.addEventListener('click', async () => {
          await fetch(`/api/admin/accounts/${button.dataset.accountDelete}`, { method: 'DELETE' });
          const next = await getJSON('/api/admin/accounts');
          draw(next.data || []);
        });
      });
    };
    draw(accountsPayload.data || []);

    document.getElementById('account-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      const form = new FormData(event.target);
      const payload = {
        id: form.get('id'),
        plugin_id: form.get('plugin_id'),
        name: form.get('name'),
        models: Array.from(document.querySelectorAll('input[name="selected_models"]:checked')).map((item) => item.value),
        validation_message: form.get('validation_message'),
        fields: Object.fromEntries(Array.from(form.keys()).filter((key) => key.startsWith('account_field_')).map((key) => [key.replace('account_field_', ''), String(form.get(key) || '')]).filter((item) => item[1] !== '')),
        status: form.get('status'),
        priority: Number(form.get('priority') || 0),
        max_requests: Number(form.get('max_requests') || 0)
      };
      await getJSON('/api/admin/accounts', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      const next = await getJSON('/api/admin/accounts');
      draw(next.data || []);
      event.target.reset();
      selectedModels.length = 0;
      drawAccountModels();
      drawAccountFields();
    });

    document.getElementById('account-form-reset').addEventListener('click', () => {
      document.getElementById('account-form').reset();
      selectedModels.length = 0;
      drawAccountModels();
      drawAccountFields();
    });
  }

  async function renderClients() {
    const [catalogPayload, clientsPayload] = await Promise.all([
      getJSON('/api/admin/catalog/models'),
      getJSON('/api/admin/consumers')
    ]);
    adminLayout('客户端账号', '管理给客户端下发的 API Key 与可见模型权限。', `
      <div class="grid">
        <section class="card">
          <h3>新增或更新客户端</h3>
          <form id="client-form">
            <label>客户端 ID<input name="id" placeholder="例如 app-1"></label>
            <label>显示名称<input name="name" placeholder="例如 内部应用A"></label>
            <label>API Key<input name="api_key" placeholder="可留空自动生成"></label>
            <button type="button" class="secondary" id="client-generate-key">生成 API Key</button>
            <label>启用状态<select name="enabled"><option value="true">启用</option><option value="false">禁用</option></select></label>
            <div><div class="muted" style="margin-bottom:8px;">允许模型（留空=全部）</div><div id="client-model-perms"></div></div>
            <div class="toolbar"><button type="submit">保存客户端</button><button type="button" class="secondary" id="client-form-reset">清空表单</button></div>
          </form>
        </section>
        <section class="card"><h3>客户端列表</h3><div id="client-list" class="list"></div></section>
      </div>
    `);

    const permsBox = document.getElementById('client-model-perms');
    const catalog = catalogPayload.data || [];
    const groups = {};
    catalog.forEach((item) => {
      const key = item.plugin_id || 'unknown-plugin';
      if (!groups[key]) groups[key] = [];
      groups[key].push(item);
    });
    const groupedModels = Object.keys(groups).sort().map((pluginID) => ({ pluginID, models: groups[pluginID] }));

    const drawPerms = (selected = []) => {
      permsBox.innerHTML = groupedModels.map((group) => `
        <div class="item" style="margin-bottom:10px;">
          <h3>${escapeHTML(group.pluginID)} <span class="pill">plugin</span></h3>
          <div>${group.models.map((model) => `<label><input type="checkbox" name="client_model_perm" value="${escapeHTML(model.id)}" ${selected.includes(model.id) ? 'checked' : ''}> ${escapeHTML(model.id)}</label>`).join('')}</div>
        </div>
      `).join('') || '<div class="muted">没有可用模型。</div>';
    };
    drawPerms();

    const list = document.getElementById('client-list');
    const draw = (items) => {
      list.innerHTML = items.map((item) => `<div class="item"><h3>${escapeHTML(item.name)} <span class="pill">${item.enabled ? 'enabled' : 'disabled'}</span></h3><div class="muted">id: ${escapeHTML(item.id)} | api_key: ${escapeHTML(item.api_key)}</div><div style="margin-top:8px;">${sourceBadges(item.allowed_models || [])}</div><div class="toolbar" style="margin-top:10px;"><button data-client-edit="${escapeHTML(item.id)}">编辑</button><button class="secondary" data-client-delete="${escapeHTML(item.id)}">删除</button></div></div>`).join('') || '<div class="muted">还没有客户端账号。</div>';
      list.querySelectorAll('[data-client-edit]').forEach((button) => {
        button.addEventListener('click', () => {
          const item = items.find((it) => it.id === button.dataset.clientEdit);
          if (!item) return;
          document.querySelector('input[name="id"]').value = item.id || '';
          document.querySelector('input[name="name"]').value = item.name || '';
          document.querySelector('input[name="api_key"]').value = item.api_key || '';
          document.querySelector('select[name="enabled"]').value = item.enabled ? 'true' : 'false';
          drawPerms(item.allowed_models || []);
        });
      });
      list.querySelectorAll('[data-client-delete]').forEach((button) => {
        button.addEventListener('click', async () => {
          await fetch(`/api/admin/consumers/${button.dataset.clientDelete}`, { method: 'DELETE' });
          const next = await getJSON('/api/admin/consumers');
          draw(next.data || []);
        });
      });
    };
    draw(clientsPayload.data || []);

    document.getElementById('client-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      const form = new FormData(event.target);
      const payload = { id: form.get('id'), name: form.get('name'), api_key: form.get('api_key'), enabled: String(form.get('enabled')) === 'true', allowed_models: Array.from(document.querySelectorAll('input[name="client_model_perm"]:checked')).map((it) => it.value) };
      await getJSON('/api/admin/consumers', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
      const next = await getJSON('/api/admin/consumers');
      draw(next.data || []);
      event.target.reset();
      drawPerms();
    });
    document.getElementById('client-generate-key').addEventListener('click', () => {
      document.querySelector('input[name="api_key"]').value = `sk-web2api-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
    });
    document.getElementById('client-form-reset').addEventListener('click', () => {
      document.getElementById('client-form').reset();
      drawPerms();
    });
  }


  async function renderTest() {
    const clients = await getJSON('/api/admin/consumers');
    adminLayout('接口测试', '直接从后台测试转换后的 OpenAI 兼容接口。', `
      <div class="grid">
        <section class="card">
          <label>客户端<select id="client-select"><option value="">无（仅无鉴权时可用）</option></select></label>
          <label>模型<select id="model-select"></select></label>
          <label>消息<textarea id="message" rows="7">你好，介绍一下你自己</textarea></label>
          <label><input type="checkbox" id="stream" checked> 流式返回</label>
          <label><input type="checkbox" id="thinking"> thinking 输出</label>
          <div class="toolbar">
            <button id="send">发送测试请求</button>
            <a href="/webui/test"><button type="button" class="secondary">打开用户测试页</button></a>
          </div>
        </section>
        <section class="card">
          <h3>返回内容</h3>
          <pre id="output"></pre>
        </section>
      </div>
    `);

    const modelSelect = document.getElementById('model-select');
    const clientSelect = document.getElementById('client-select');

    (clients.data || []).forEach((item) => {
      const option = document.createElement('option');
      option.value = item.api_key;
      option.textContent = `${item.name} (${item.id})`;
      clientSelect.appendChild(option);
    });

    let allModels = [];
    const loadModels = async () => {
      const headers = clientSelect.value ? { Authorization: `Bearer ${clientSelect.value}` } : {};
      const res = await fetch('/v1/models', { headers });
      const payload = await res.json();
      allModels = (payload.data || []).map((item) => ({ id: item.id }));
      modelSelect.innerHTML = allModels.map((item) => `<option value="${escapeHTML(item.id)}">${escapeHTML(item.id)}</option>`).join('');
    };
    clientSelect.addEventListener('change', async () => {
      await loadModels();
    });
    await loadModels();

    document.getElementById('send').addEventListener('click', async () => {
      const output = document.getElementById('output');
      output.textContent = '';
      const payload = {
        model: modelSelect.value,
        stream: document.getElementById('stream').checked,
        thinking: document.getElementById('thinking').checked,
        messages: [{ role: 'user', content: document.getElementById('message').value }]
      };
      const res = await fetch('/v1/chat/completions', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(clientSelect.value ? { Authorization: `Bearer ${clientSelect.value}` } : {})
        },
        body: JSON.stringify(payload)
      });
      if (!payload.stream) {
        output.textContent = JSON.stringify(await res.json(), null, 2);
        return;
      }
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        output.textContent += decoder.decode(value, { stream: true });
      }
    });
  }

  async function renderStatus() {
    const status = await getJSON('/api/admin/status');
    const routeCards = Object.entries(status.routes || {}).map(([key, value]) => `
      <div class="item"><h3>${escapeHTML(key)}</h3><div class="muted">${escapeHTML(value)}</div></div>
    `).join('') || '<div class="muted">无路由信息。</div>';
    const pluginCards = (status.plugins.items || []).map((item) => `
      <div class="item">
        <h3>${escapeHTML(item.manifest.name || item.path)} <span class="pill">${escapeHTML(item.status || 'unknown')}</span></h3>
        <div class="muted">id: ${escapeHTML(item.manifest.id || '(missing)')} | version: ${escapeHTML(item.manifest.version || '-')}</div>
        <div style="margin-top:8px;">${(item.manifest.models || []).map((model) => `<span class="pill">${escapeHTML(model.id)}</span>`).join('') || '<span class="muted">无模型</span>'}</div>
        ${item.error ? `<div class="muted" style="margin-top:8px;color:#fecaca;">error: ${escapeHTML(item.error)}</div>` : ''}
      </div>
    `).join('') || '<div class="muted">无插件。</div>';
    const sourceCards = (status.sources.items || []).map((item) => `
      <div class="item">
        <h3>${escapeHTML(item.name)} <span class="pill">${item.enabled ? 'enabled' : 'disabled'}</span></h3>
        <div class="muted">id: ${escapeHTML(item.id)} | plugin: ${escapeHTML(item.plugin_id || '(none)')}</div>
        <div style="margin-top:8px;">${sourceBadges(item.models || [])}</div>
      </div>
    `).join('') || '<div class="muted">无内部源。</div>';
    const accountCards = (status.accounts.items || []).map((item) => `
      <div class="item">
        <h3>${escapeHTML(item.name)} <span class="pill">${escapeHTML(item.status || 'active')}</span></h3>
        <div class="muted">id: ${escapeHTML(item.id)} | source: ${escapeHTML(item.source_id)}</div>
        <div class="muted">used/max: ${escapeHTML(item.used_requests || 0)} / ${escapeHTML(item.max_requests || 0)}</div>
      </div>
    `).join('') || '<div class="muted">无账号。</div>';
    const consumerCards = (status.consumers.items || []).map((item) => `
      <div class="item"><h3>${escapeHTML(item.name)} <span class="pill">${item.enabled ? 'enabled' : 'disabled'}</span></h3><div class="muted">id: ${escapeHTML(item.id)}</div></div>
    `).join('') || '<div class="muted">无客户端账号。</div>';
    const modelRouteCards = (status.catalog_models.items || []).map((item) => `
      <div class="item"><h3>${escapeHTML(item.id)}</h3><div class="muted">plugin: ${escapeHTML(item.plugin_id)} | source_model: ${escapeHTML(item.source_model)}</div></div>
    `).join('') || '<div class="muted">无模型路由。</div>';
    adminLayout('运行状态', '查看当前加载结果和管理台入口。', `
      <div class="grid">
        <section class="card"><h3>服务状态</h3><div class="item"><h3>${escapeHTML((status.service || {}).name || 'web2api')} <span class="pill">${escapeHTML((status.service || {}).status || 'unknown')}</span></h3></div></section>
        <section class="card"><h3>管理路由</h3><div class="list">${routeCards}</div></section>
      </div>
      <div class="grid" style="margin-top:20px;">
        <section class="card"><h3>插件详情</h3><div class="list">${pluginCards}</div></section>
        <section class="card"><h3>内部源详情</h3><div class="list">${sourceCards}</div></section>
        <section class="card"><h3>账号详情</h3><div class="list">${accountCards}</div></section>
        <section class="card"><h3>客户端详情</h3><div class="list">${consumerCards}</div></section>
        <section class="card"><h3>模型路由详情</h3><div class="list">${modelRouteCards}</div></section>
      </div>
    `);
  }

  const routes = {
    overview: renderOverview,
    plugins: renderPlugins,
    accounts: renderAccounts,
    clients: renderClients,
    test: renderTest,
    status: renderStatus
  };

  const run = routes[page];
  if (run) {
    run().catch((error) => {
      adminLayout('后台错误', '页面初始化失败。', `<section class="card"><pre>${escapeHTML(String(error && error.stack || error))}</pre></section>`);
    });
  }
})();
