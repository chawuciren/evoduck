// ===== Diagnostics Page =====

function fetchDiagnostics() {
    if (!sendWSRequest('get_capabilities')) renderDiagnostics();
}

function refreshDiagnostics() {
    addLog('info', 'Refreshing diagnostics...');
    fetchDiagnostics();
}

function renderDiagnostics() {
    renderDiagnosticsStatus();
    renderDiagnosticsOverview();
    renderDiagnosticsColumns();
}

function renderDiagnosticsStatus() {
    var pill = document.getElementById('diagnosticsStatusPill');
    var text = document.getElementById('diagnosticsStatusText');
    if (!pill || !text) return;

    if (!capabilitiesData) {
        pill.className = 'diagnostics-status-pill pending';
        pill.textContent = 'Loading';
        text.textContent = 'Waiting for runtime diagnostics';
        return;
    }

    var status = String(capabilitiesData.status || 'unknown').toLowerCase();
    pill.className = 'diagnostics-status-pill ' + status;
    pill.textContent = status.toUpperCase();

    var warningCount = (capabilitiesData.warnings || []).length;
    var errorCount = (capabilitiesData.errors || []).length;
    var summary = [];
    summary.push((capabilitiesData.summary && capabilitiesData.summary.agent_count || 0) + ' agents');
    summary.push((capabilitiesData.summary && capabilitiesData.summary.provider_count || 0) + ' providers');
    summary.push(warningCount + ' warnings');
    summary.push(errorCount + ' errors');
    text.textContent = summary.join(' · ');
}

function renderDiagnosticsOverview() {
    var grid = document.getElementById('diagnosticsOverviewGrid');
    if (!grid) return;
    if (!capabilitiesData) {
        grid.innerHTML = '';
        return;
    }

    var summary = capabilitiesData.summary || {};
    var gateway = capabilitiesData.gateway || {};
    var cards = [
        { label: 'Agents', value: summary.agent_count || 0, meta: 'registered runtimes' },
        { label: 'Providers', value: summary.provider_count || 0, meta: 'LLM backends' },
        { label: 'Channels', value: summary.configured_channel_count || 0, meta: (summary.registered_bridge_count || 0) + ' bridges active' },
        { label: 'Plugins', value: capabilitiesData.plugins && capabilitiesData.plugins.connected_plugin_count || 0, meta: 'connected extensions' },
        { label: 'MCP Tools', value: capabilitiesData.mcp && capabilitiesData.mcp.tool_count || 0, meta: ((capabilitiesData.mcp && capabilitiesData.mcp.client_count) || 0) + ' clients' },
        { label: 'Uptime', value: gateway.uptime || '—', meta: gateway.resolved_agent || 'no default agent' }
    ];

    grid.innerHTML = cards.map(function(card) {
        return '<div class="diagnostics-kpi-card">'
            + '<div class="diagnostics-kpi-label">' + escapeHtml(card.label) + '</div>'
            + '<div class="diagnostics-kpi-value">' + escapeHtml(card.value) + '</div>'
            + '<div class="diagnostics-kpi-meta">' + escapeHtml(card.meta) + '</div>'
            + '</div>';
    }).join('');
}

function renderDiagnosticsColumns() {
    var main = document.getElementById('diagnosticsMainColumn');
    var side = document.getElementById('diagnosticsSideColumn');
    if (!main || !side) return;

    if (!capabilitiesData) {
        main.innerHTML = '<div class="settings-section"><div class="settings-section-title">Diagnostics</div><div class="settings-empty-inline">No diagnostics data loaded.</div></div>';
        side.innerHTML = '';
        return;
    }

    main.innerHTML = [
        renderGatewaySection(capabilitiesData.gateway || {}),
        renderLLMSection(capabilitiesData.llm || {}),
        renderAgentsSection(capabilitiesData.agents || []),
        renderChannelsSection(capabilitiesData.channels || [])
    ].join('');

    side.innerHTML = [
        renderPluginsSection(capabilitiesData.plugins || {}),
        renderMCPSection(capabilitiesData.mcp || {}),
        renderMemorySection(capabilitiesData.memory || {}),
        renderSchedulerSection(capabilitiesData.scheduler || {}),
        renderIssuesSection('Warnings', capabilitiesData.warnings || [], 'warning'),
        renderIssuesSection('Errors', capabilitiesData.errors || [], 'error')
    ].join('');
}

function renderGatewaySection(gateway) {
    var rows = [
        diagnosticsRow('Default Agent', gateway.default_agent || '—'),
        diagnosticsRow('Resolved Agent', gateway.resolved_agent || '—'),
        diagnosticsRow('Resolved OK', gateway.resolved_agent_ok ? 'Yes' : 'No'),
        diagnosticsRow('Channels Started', gateway.channels_started ? 'Yes' : 'No'),
        diagnosticsRow('Slash Commands', gateway.slash_commands ? 'Ready' : 'Not initialized'),
        diagnosticsRow('WebChat Mode', gateway.webchat_gateway ? 'Gateway web layer' : 'Unknown'),
        diagnosticsRow('Media Store', gateway.media_store_enabled ? 'Enabled' : 'Disabled'),
        diagnosticsRow('Config Path', gateway.config_path || '—')
    ].join('');
    return diagnosticsSection('Gateway', rows);
}

function renderLLMSection(llm) {
    var providers = llm.providers || [];
    var body = '<div class="diagnostics-table-shell"><table class="diagnostics-table"><thead><tr><th>Provider</th><th>Status</th><th>Models</th><th>Notes</th></tr></thead><tbody>';
    if (!providers.length) {
        body += '<tr><td colspan="4" class="diagnostics-empty-cell">No providers registered</td></tr>';
    } else {
        providers.forEach(function(provider) {
            var notes = provider.error || (provider.is_default ? 'default provider' : 'ready');
            body += '<tr>'
                + '<td>' + escapeHtml(provider.name || '') + (provider.is_default ? ' <span class="diagnostics-inline-chip">default</span>' : '') + '</td>'
                + '<td><span class="diagnostics-badge ' + escapeHtml(String(provider.status || 'unknown').toLowerCase()) + '">' + escapeHtml(provider.status || 'unknown') + '</span></td>'
                + '<td>' + escapeHtml(provider.model_count || 0) + '</td>'
                + '<td>' + escapeHtml(notes) + '</td>'
                + '</tr>';
        });
    }
    body += '</tbody></table></div>';
    body += '<div class="diagnostics-subcopy">Default: ' + escapeHtml(llm.default_provider || '—') + ' / ' + escapeHtml(llm.default_model || '—') + '</div>';
    return diagnosticsSection('LLM', body);
}

function renderAgentsSection(agents) {
    var content = '<div class="diagnostics-agent-list">';
    if (!agents.length) {
        content += '<div class="diagnostics-empty-block">No agents registered</div>';
    } else {
        agents.forEach(function(agent) {
            var toolPreview = (agent.tools || []).slice(0, 10).map(function(tool) {
                return '<span class="diagnostics-tool-chip ' + escapeHtml(tool.source || 'builtin') + '">' + escapeHtml(tool.name || '') + '</span>';
            }).join('');
            var extraTools = (agent.tools || []).length - Math.min((agent.tools || []).length, 10);
            if (extraTools > 0) {
                toolPreview += '<span class="diagnostics-tool-chip more">+' + extraTools + ' more</span>';
            }
            content += '<div class="diagnostics-agent-card">'
                + '<div class="diagnostics-agent-header">'
                + '<div><div class="diagnostics-agent-title">' + escapeHtml(agent.id || '') + '</div><div class="diagnostics-agent-meta">' + escapeHtml(agent.role || '') + ' · ' + escapeHtml(agent.provider || '') + ' / ' + escapeHtml(agent.model || '') + '</div></div>'
                + '<span class="diagnostics-badge ' + escapeHtml(String(agent.status || 'unknown').toLowerCase()) + '">' + escapeHtml(agent.status || 'unknown') + '</span>'
                + '</div>'
                + '<div class="diagnostics-agent-stats">'
                + '<span>tools: ' + escapeHtml(agent.tool_count || 0) + '</span>'
                + '<span>skills: ' + escapeHtml(agent.skill_count || 0) + '</span>'
                + '<span>memory: ' + (agent.memory_manager_ready ? 'ready' : 'missing') + '</span>'
                + '<span>runtime: ' + (agent.runtime_ready ? 'ready' : 'missing') + '</span>'
                + '</div>'
                + '<div class="diagnostics-tool-cloud">' + toolPreview + '</div>'
                + renderInlineIssues(agent.warnings || [], 'warning')
                + '</div>';
        });
    }
    content += '</div>';
    return diagnosticsSection('Agents', content);
}

function renderChannelsSection(channels) {
    var body = '<div class="diagnostics-table-shell"><table class="diagnostics-table"><thead><tr><th>Channel</th><th>Kind</th><th>Status</th><th>Agent</th><th>Notes</th></tr></thead><tbody>';
    if (!channels.length) {
        body += '<tr><td colspan="5" class="diagnostics-empty-cell">No channels configured</td></tr>';
    } else {
        channels.forEach(function(channel) {
            body += '<tr>'
                + '<td>' + escapeHtml(channel.id || '') + '<div class="diagnostics-cell-meta">' + escapeHtml(channel.type || '') + '</div></td>'
                + '<td>' + escapeHtml(channel.kind || '') + '</td>'
                + '<td><span class="diagnostics-badge ' + escapeHtml(String(channel.status || 'unknown').toLowerCase()) + '">' + escapeHtml(channel.status || 'unknown') + '</span></td>'
                + '<td>' + escapeHtml(channel.agent || '—') + '</td>'
                + '<td>' + escapeHtml(channel.message || (channel.registered ? 'bridge registered' : 'not registered')) + '</td>'
                + '</tr>';
        });
    }
    body += '</tbody></table></div>';
    return diagnosticsSection('Channels', body);
}

function renderPluginsSection(plugins) {
    var rows = [
        diagnosticsRow('Enabled', plugins.enabled ? 'Yes' : 'No'),
        diagnosticsRow('Connected Plugins', plugins.connected_plugin_count || 0),
        diagnosticsRow('Tool Adapters', plugins.tool_adapter_count || 0),
        diagnosticsRow('Provider Adapters', plugins.provider_adapter_count || 0),
        diagnosticsRow('Channel Bridges', plugins.channel_bridge_count || 0),
        diagnosticsRow('Hook Events', plugins.hook_event_count || 0)
    ].join('');
    if ((plugins.items || []).length) {
        rows += '<div class="diagnostics-mini-list">' + plugins.items.map(function(item) {
            return '<div class="diagnostics-mini-item"><span>' + escapeHtml(item.plugin_id || '') + '</span><span class="diagnostics-badge ' + escapeHtml(String(item.status || 'unknown').toLowerCase()) + '">' + escapeHtml(item.status || 'unknown') + '</span></div>';
        }).join('') + '</div>';
    }
    return diagnosticsSection('Plugins', rows);
}

function renderMCPSection(mcp) {
    var rows = [
        diagnosticsRow('Initialized', mcp.initialized ? 'Yes' : 'No'),
        diagnosticsRow('Clients', mcp.client_count || 0),
        diagnosticsRow('Tools', mcp.tool_count || 0)
    ].join('');
    if ((mcp.clients || []).length) {
        rows += '<div class="diagnostics-mini-list">' + mcp.clients.map(function(client) {
            return '<div class="diagnostics-mini-item"><span>' + escapeHtml(client.name || '') + ' · ' + escapeHtml(client.server || 'server') + '</span><span>' + escapeHtml(client.tool_count || 0) + ' tools</span></div>';
        }).join('') + '</div>';
    }
    return diagnosticsSection('MCP', rows);
}

function renderMemorySection(memory) {
    return diagnosticsSection('Memory', [
        diagnosticsRow('Session Flusher', memory.flusher_ready ? 'Ready' : 'Missing')
    ].join(''));
}

function renderSchedulerSection(scheduler) {
    return diagnosticsSection('Scheduler', [
        diagnosticsRow('Cron', scheduler.cron_ready ? 'Ready' : 'Missing'),
        diagnosticsRow('Service', scheduler.service_ready ? 'Ready' : 'Missing'),
        diagnosticsRow('Registered Jobs', scheduler.registered_job_count || 0)
    ].join(''));
}

function renderIssuesSection(title, issues, kind) {
    if (!issues || !issues.length) {
        return diagnosticsSection(title, '<div class="diagnostics-empty-block">No ' + escapeHtml(title.toLowerCase()) + '</div>');
    }
    return diagnosticsSection(title, '<div class="diagnostics-issue-list ' + escapeHtml(kind) + '">' + issues.map(function(issue) {
        return '<div class="diagnostics-issue-item">' + escapeHtml(issue) + '</div>';
    }).join('') + '</div>');
}

function renderInlineIssues(issues, kind) {
    if (!issues || !issues.length) return '';
    return '<div class="diagnostics-inline-issues ' + escapeHtml(kind || 'warning') + '">' + issues.map(function(issue) {
        return '<div class="diagnostics-inline-issue">' + escapeHtml(issue) + '</div>';
    }).join('') + '</div>';
}

function diagnosticsSection(title, body) {
    return '<div class="settings-section diagnostics-section">'
        + '<div class="settings-section-title">' + escapeHtml(title) + '</div>'
        + body
        + '</div>';
}

function diagnosticsRow(label, value) {
    return '<div class="diagnostics-row"><span class="diagnostics-row-label">' + escapeHtml(label) + '</span><span class="diagnostics-row-value">' + escapeHtml(value) + '</span></div>';
}
