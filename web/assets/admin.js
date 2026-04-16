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
            <p>多来源 Web 转 API 管理台</p>
          </div>
          <nav class="nav">
            <a href="/admin" data-nav="overview">总览</a>
            <a href="/admin/plugins" data-nav="plugins">插件</a>
            <a href="/admin/sources" data-nav="sources">来源</a>
            <a href="/admin/accounts" data-nav="accounts">账号</a>
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
    adminLayout('后台总览', '查看当前插件、来源和操作入口。', `
      <div class="grid">
        <section class="card"><div class="muted">已就绪插件</div><div class="stat">${status.plugins.items.filter((p) => p.status === 'ready').length}</div></section>
        <section class="card"><div class="muted">来源总数</div><div class="stat">${status.sources.total}</div></section>
        <section class="card"><div class="muted">启用来源</div><div class="stat">${status.sources.enabled}</div></section>
        <section class="card"><div class="muted">可用账号</div><div class="stat">${status.accounts.active}</div></section>
      </div>
      <div class="grid" style="margin-top:20px;">
        <section class="card">
          <h3>快速入口</h3>
          <div class="nav">
            <a href="/admin/plugins">管理插件</a>
            <a href="/admin/sources">配置来源</a>
            <a href="/admin/test">测试转换接口</a>
            <a href="/admin/status">查看运行状态</a>
          </div>
        </section>
        <section class="card">
          <h3>当前来源</h3>
          <div class="list">
            ${status.sources.items.map((item) => `<div class="item"><h3>${escapeHTML(item.name)} <span class="pill">${item.enabled ? 'enabled' : 'disabled'}</span></h3><div class="muted">plugin: ${escapeHTML(item.plugin_id || '(none)')}</div><div>${sourceBadges(item.models || [])}</div></div>`).join('') || '<div class="muted">还没有配置来源。</div>'}
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

  async function renderSources() {
    const [pluginsPayload, sourcesPayload] = await Promise.all([
      getJSON('/api/admin/plugins'),
      getJSON('/api/admin/sources')
    ]);

    adminLayout('来源管理', '为不同 Web 来源绑定插件、模型前缀和基础参数。', `
      <div class="grid">
        <section class="card">
          <h3>新增或更新来源</h3>
          <form id="source-form">
            <label>来源 ID<input name="id" placeholder="例如 grok"></label>
            <label>显示名称<input name="name" placeholder="例如 Grok"></label>
            <label>插件<select name="plugin_id"><option value="">不绑定插件</option></select></label>
            <div>
              <div class="muted" style="margin-bottom:8px;">启用模型</div>
              <div id="source-models"></div>
            </div>
            <label>模型前缀<input name="model_prefixes" placeholder="兼容旧规则时可填写，例如 grok-"></label>
            <label>基础 URL<input name="base_url" placeholder="https://example.com"></label>
            <label>Mock 回复前缀<input name="mock_reply_prefix" placeholder="仅调试时使用"></label>
            <label>启用状态<select name="enabled"><option value="true">启用</option><option value="false">禁用</option></select></label>
            <div class="toolbar">
              <button type="submit">保存来源</button>
              <button type="button" class="secondary" id="source-form-reset">清空表单</button>
            </div>
          </form>
        </section>
        <section class="card">
          <h3>已配置来源</h3>
          <div id="source-list" class="list"></div>
        </section>
      </div>
    `);

    const select = document.querySelector('select[name="plugin_id"]');
    (pluginsPayload.data || []).forEach((item) => {
      const option = document.createElement('option');
      option.value = item.manifest.id;
      option.textContent = `${item.manifest.name} (${item.manifest.id})`;
      select.appendChild(option);
    });

    const list = document.getElementById('source-list');
    const modelBox = document.getElementById('source-models');
    const selectedModels = [];
    const renderModels = () => {
      modelBox.innerHTML = renderModelCheckboxes(pluginByID(pluginsPayload.data || [], select.value), selectedModels);
    };
    select.addEventListener('change', () => {
      selectedModels.length = 0;
      renderModels();
    });
    renderModels();

    const draw = (items) => {
      list.innerHTML = items.map((item) => `
        <div class="item">
          <h3>${escapeHTML(item.name)} <span class="pill">${item.enabled ? 'enabled' : 'disabled'}</span></h3>
          <div class="muted">id: ${escapeHTML(item.id)} | plugin: ${escapeHTML(item.plugin_id || '(none)')}</div>
          <div style="margin:8px 0;">${sourceBadges(item.models || item.model_prefixes)}</div>
          <div class="muted">base_url: ${escapeHTML(item.base_url || '-')}</div>
          <div class="toolbar" style="margin-top:10px;">
            <button data-source-edit="${escapeHTML(item.id)}">编辑</button>
            <button class="secondary" data-source-delete="${escapeHTML(item.id)}">删除</button>
          </div>
        </div>
      `).join('') || '<div class="muted">还没有来源配置。</div>';

      list.querySelectorAll('[data-source-edit]').forEach((button) => {
        button.addEventListener('click', () => {
          const sourceItem = items.find((it) => it.id === button.dataset.sourceEdit);
          if (!sourceItem) return;
          document.querySelector('input[name="id"]').value = sourceItem.id || '';
          document.querySelector('input[name="name"]').value = sourceItem.name || '';
          select.value = sourceItem.plugin_id || '';
          selectedModels.length = 0;
          (sourceItem.models || []).forEach((model) => selectedModels.push(model));
          renderModels();
          document.querySelector('input[name="model_prefixes"]').value = (sourceItem.model_prefixes || []).join(',');
          document.querySelector('input[name="base_url"]').value = sourceItem.base_url || '';
          document.querySelector('input[name="mock_reply_prefix"]').value = sourceItem.mock_reply_prefix || '';
          document.querySelector('select[name="enabled"]').value = sourceItem.enabled ? 'true' : 'false';
        });
      });
      list.querySelectorAll('[data-source-delete]').forEach((button) => {
        button.addEventListener('click', async () => {
          await fetch(`/api/admin/sources/${button.dataset.sourceDelete}`, { method: 'DELETE' });
          const next = await getJSON('/api/admin/sources');
          draw(next.data || []);
        });
      });
    };
    draw(sourcesPayload.data || []);

    document.getElementById('source-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      const form = new FormData(event.target);
      const payload = {
        id: form.get('id'),
        name: form.get('name'),
        plugin_id: form.get('plugin_id'),
        enabled: String(form.get('enabled')) === 'true',
        models: Array.from(document.querySelectorAll('input[name="selected_models"]:checked')).map((item) => item.value),
        model_prefixes: String(form.get('model_prefixes') || '').split(',').map((v) => v.trim()).filter(Boolean),
        base_url: form.get('base_url'),
        mock_reply_prefix: form.get('mock_reply_prefix')
      };
      await getJSON('/api/admin/sources', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      const next = await getJSON('/api/admin/sources');
      draw(next.data || []);
      event.target.reset();
      selectedModels.length = 0;
      renderModels();
    });

    document.getElementById('source-form-reset').addEventListener('click', () => {
      document.getElementById('source-form').reset();
      selectedModels.length = 0;
      renderModels();
    });
  }

  async function renderAccounts() {
    const [sourcesPayload, accountsPayload, pluginsPayload] = await Promise.all([
      getJSON('/api/admin/sources'),
      getJSON('/api/admin/accounts'),
      getJSON('/api/admin/plugins')
    ]);
    adminLayout('账号管理', '管理来源账号、冷却状态和手工反馈。', `
      <div class="grid">
        <section class="card">
          <h3>新增或更新账号</h3>
          <form id="account-form">
            <label>账号 ID<input name="id" placeholder="例如 grok-001"></label>
            <label>所属来源<select name="source_id"></select></label>
            <label>显示名称<input name="name" placeholder="例如 主账号"></label>
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

    const sourceSelect = document.querySelector('select[name="source_id"]');
    (sourcesPayload.data || []).forEach((item) => {
      const option = document.createElement('option');
      option.value = item.id;
      option.textContent = `${item.name} (${item.id})`;
      sourceSelect.appendChild(option);
    });
    const fieldBox = document.getElementById('account-fields');
    const drawAccountFields = () => {
      const sourceItem = (sourcesPayload.data || []).find((item) => item.id === sourceSelect.value);
      const pluginItem = sourceItem ? pluginByID(pluginsPayload.data || [], sourceItem.plugin_id) : null;
      const fields = pluginItem && pluginItem.manifest ? (pluginItem.manifest.account_fields || []) : [];
      fieldBox.innerHTML = fields.map((field) => `<label>${escapeHTML(field.label || field.key)}<input name="account_field_${escapeHTML(field.key)}" type="${field.secret ? 'password' : 'text'}" placeholder="${escapeHTML(field.placeholder || '')}"></label>${field.help ? `<div class="muted" style="margin-top:-8px;margin-bottom:10px;">${escapeHTML(field.help)}</div>` : ''}`).join('') || '<div class="muted">当前来源插件没有声明账号字段。</div>';
    };
    sourceSelect.addEventListener('change', drawAccountFields);
    drawAccountFields();

    const list = document.getElementById('account-list');
    const draw = (items) => {
      list.innerHTML = items.map((item) => `
        <div class="item">
          <h3>${escapeHTML(item.name)} <span class="pill">${escapeHTML(item.status)}</span></h3>
          <div class="muted">id: ${escapeHTML(item.id)} | source: ${escapeHTML(item.source_id)} | priority: ${escapeHTML(item.priority || 0)}</div>
          <div class="muted">fields: ${escapeHTML(Object.keys(item.fields || {}).join(', ') || '-')}</div>
          <div class="muted">used/max: ${escapeHTML(item.used_requests || 0)} / ${escapeHTML(item.max_requests || 0)}</div>
          <div class="muted">last error: ${escapeHTML(item.last_error || '-')}</div>
          <div class="toolbar" style="margin-top:10px;">
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
          sourceSelect.value = accountItem.source_id || '';
          drawAccountFields();
          document.querySelector('input[name="name"]').value = accountItem.name || '';
          document.querySelector('select[name="status"]').value = accountItem.status || 'active';
          document.querySelector('input[name="priority"]').value = accountItem.priority || 0;
          document.querySelector('input[name="max_requests"]').value = accountItem.max_requests || 0;
          Object.entries(accountItem.fields || {}).forEach(([key, value]) => {
            const input = document.querySelector(`input[name="account_field_${key}"]`);
            if (input) input.value = value;
          });
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
        source_id: form.get('source_id'),
        name: form.get('name'),
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
      drawAccountFields();
    });

    document.getElementById('account-form-reset').addEventListener('click', () => {
      document.getElementById('account-form').reset();
      drawAccountFields();
    });
  }

  async function renderTest() {
    const [sources, models] = await Promise.all([
      getJSON('/api/admin/sources'),
      getJSON('/v1/models')
    ]);
    adminLayout('接口测试', '直接从后台测试转换后的 OpenAI 兼容接口。', `
      <div class="grid">
        <section class="card">
          <label>来源参考<select id="source-select"><option value="">自动按模型前缀匹配</option></select></label>
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

    const select = document.getElementById('source-select');
    const modelSelect = document.getElementById('model-select');
    (sources.data || []).forEach((item) => {
      const option = document.createElement('option');
      option.value = item.id;
      option.textContent = `${item.name} (${(item.models || []).join(', ') || 'no-model'})`;
      select.appendChild(option);
    });

    const allModels = (models.data || []).map((item) => ({
      id: item.id,
      sourceIDs: (item.web2api && item.web2api.source_ids) || []
    }));
    const renderModelOptions = () => {
      const sourceID = select.value;
      const filtered = sourceID ? allModels.filter((m) => m.sourceIDs.includes(sourceID)) : allModels;
      modelSelect.innerHTML = (filtered.length ? filtered : allModels).map((item) => `<option value="${escapeHTML(item.id)}">${escapeHTML(item.id)}</option>`).join('');
    };
    select.addEventListener('change', renderModelOptions);
    renderModelOptions();

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
        headers: { 'Content-Type': 'application/json' },
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
    `).join('') || '<div class="muted">无来源。</div>';
    const accountCards = (status.accounts.items || []).map((item) => `
      <div class="item">
        <h3>${escapeHTML(item.name)} <span class="pill">${escapeHTML(item.status || 'active')}</span></h3>
        <div class="muted">id: ${escapeHTML(item.id)} | source: ${escapeHTML(item.source_id)}</div>
        <div class="muted">used/max: ${escapeHTML(item.used_requests || 0)} / ${escapeHTML(item.max_requests || 0)}</div>
      </div>
    `).join('') || '<div class="muted">无账号。</div>';
    adminLayout('运行状态', '查看当前加载结果和管理台入口。', `
      <div class="grid">
        <section class="card"><h3>服务状态</h3><div class="item"><h3>${escapeHTML((status.service || {}).name || 'web2api')} <span class="pill">${escapeHTML((status.service || {}).status || 'unknown')}</span></h3></div></section>
        <section class="card"><h3>管理路由</h3><div class="list">${routeCards}</div></section>
      </div>
      <div class="grid" style="margin-top:20px;">
        <section class="card"><h3>插件详情</h3><div class="list">${pluginCards}</div></section>
        <section class="card"><h3>来源详情</h3><div class="list">${sourceCards}</div></section>
        <section class="card"><h3>账号详情</h3><div class="list">${accountCards}</div></section>
      </div>
    `);
  }

  const routes = {
    overview: renderOverview,
    plugins: renderPlugins,
    sources: renderSources,
    accounts: renderAccounts,
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
