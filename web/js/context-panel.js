// ===== Context Panel =====

function renderTaskPanel() {
    var list = document.getElementById('rpTaskList');
    var empty = document.getElementById('rpEmptyState');
    if (!list) return;

    var subTasks = (currentPlan && currentPlan.sub_tasks) || (currentPlan && currentPlan.subTasks);

    if (!subTasks || subTasks.length === 0) {
        list.innerHTML = '';
        if (empty) empty.style.display = 'block';
        return;
    }

    if (empty) empty.style.display = 'none';

    var html = '';
    subTasks.forEach(function(task) {
        var status = task.status || 'pending';
        var iconHtml = getStatusIcon(status);
        html += '<div class="rp-task-item ' + status + '">'
            + '<div class="rp-status-icon ' + status + '">' + iconHtml + '</div>'
            + '<div class="rp-task-text">' + escapeHtml(task.name || 'Unnamed') + '</div>'
            + '</div>';
    });
    list.innerHTML = html;
}

function getStatusIcon(status) {
    switch (status) {
        case 'done': return '\u2713';
        case 'running': return '\u27F3';
        case 'skipped': return '\u2717';
        default: return '\u25CB';
    }
}

function hideTaskPanel() {
    currentPlan = null;
    currentIteration = 0;
    renderTaskPanel();
    resetContextInfo();
}

function updateContextInfo(plan, iteration) {
    var el = document.getElementById('rpContextInfo');
    if (!el) return;
    el.innerHTML = '<div style="margin-bottom:8px;">'
        + '<span style="color:rgba(148,163,184,0.35);">Iter</span><br>'
        + '<span style="color:#CBD5E1;font-family:Consolas,monospace;">' + iteration + '</span>'
        + '</div>'
        + '<div style="margin-bottom:8px;">'
        + '<span style="color:rgba(148,163,184,0.35);">Intent</span><br>'
        + '<span style="color:#94A3B8;font-size:12px;">' + escapeHtml(plan.intent || '\u2014') + '</span>'
        + '</div>';
}

function resetContextInfo() {
    var el = document.getElementById('rpContextInfo');
    if (el) el.innerHTML = '\u2014';
    var statsEl = document.getElementById('contextStats');
    if (statsEl) statsEl.style.display = 'none';
}

function renderRightPanelSummaries() {
    renderSystemSummary();
    renderRuntimeSummary();
}

function renderSystemSummary() {
    var el = document.getElementById('rpSystemSummary');
    if (!el) return;
    if (!capabilitiesData) {
        el.innerHTML = '<div class="rp-empty-state">Waiting for diagnostics</div>';
        return;
    }

    var gateway = capabilitiesData.gateway || {};
    el.innerHTML = [
        summaryStatusItem('System Status', String(capabilitiesData.status || 'unknown').toUpperCase(), String(capabilitiesData.status || 'unknown').toLowerCase()),
        summaryMetricItem('Warnings', (capabilitiesData.warnings || []).length),
        summaryMetricItem('Errors', (capabilitiesData.errors || []).length),
        summaryMetricItem('Uptime', gateway.uptime || '—'),
        summaryMetricItem('Current Agent', currentAgentId || gateway.resolved_agent || gateway.default_agent || 'default')
    ].join('');
}

function renderRuntimeSummary() {
    var el = document.getElementById('rpRuntimeSummary');
    if (!el) return;
    if (!capabilitiesData) {
        el.innerHTML = '<div class="rp-empty-state">Waiting for diagnostics</div>';
        return;
    }

    var agent = getCurrentAgentDiagnostics();
    var providerModel = '—';
    if (agent) {
        providerModel = (agent.provider || '—') + ' / ' + (agent.model || '—');
    }
    var plugins = capabilitiesData.plugins || {};
    var mcp = capabilitiesData.mcp || {};
    var summary = capabilitiesData.summary || {};

    el.innerHTML = [
        summaryMetricItem('Provider / Model', providerModel),
        summaryMetricItem('Plugins', (plugins.connected_plugin_count || 0) + ' connected'),
        summaryMetricItem('MCP Tools', (mcp.tool_count || 0) + ' tools'),
        summaryMetricItem('Channels Active', summary.registered_bridge_count || 0)
    ].join('');
}

function getCurrentAgentDiagnostics() {
    if (!capabilitiesData || !capabilitiesData.agents || !capabilitiesData.agents.length) return null;

    var targetId = currentAgentId || '';
    if (targetId) {
        for (var i = 0; i < capabilitiesData.agents.length; i++) {
            if (capabilitiesData.agents[i].id === targetId) return capabilitiesData.agents[i];
        }
    }

    var resolved = capabilitiesData.gateway && capabilitiesData.gateway.resolved_agent;
    if (resolved) {
        for (var j = 0; j < capabilitiesData.agents.length; j++) {
            if (capabilitiesData.agents[j].id === resolved) return capabilitiesData.agents[j];
        }
    }

    return capabilitiesData.agents[0] || null;
}

function summaryMetricItem(label, value) {
    return '<div class="rp-summary-item"><div class="rp-summary-label">' + escapeHtml(String(label || '')) + '</div><div class="rp-summary-value">' + escapeHtml(String(value || '—')) + '</div></div>';
}

function summaryStatusItem(label, value, status) {
    return '<div class="rp-summary-item"><div class="rp-summary-label">' + escapeHtml(String(label || '')) + '</div><div class="rp-summary-value rp-summary-status ' + escapeHtml(String(status || 'unknown')) + '">' + escapeHtml(String(value || 'UNKNOWN')) + '</div></div>';
}

function fetchContextStats() {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify({
        action: 'get_context_stats',
        agent_id: currentAgentId,
        user_id: WEBCAT_USER_ID,
        session: getSessionKey()
    }));
}

function renderContextStats(stats) {
    if (!stats) return;

    var container = document.getElementById('contextStats');
    if (!container) return;

    container.style.display = 'block';

    var usageEl = document.getElementById('tokenUsage');
    if (usageEl) {
        usageEl.textContent = formatTokenNumber(stats.used_tokens) + ' / ' + formatTokenNumber(stats.max_tokens);
    }

    var percentEl = document.getElementById('usagePercent');
    if (percentEl) {
        var percent = stats.usage_percent || 0;
        percentEl.textContent = percent.toFixed(1) + '%';
        percentEl.className = 'usage-percent ' + stats.status;
    }

    var barFill = document.getElementById('contextBarFill');
    if (barFill) {
        barFill.style.width = Math.min(stats.usage_percent || 0, 100) + '%';
        barFill.className = 'context-bar-fill ' + stats.status;
    }

    var detailEl = document.getElementById('contextStatsDetail');
    if (detailEl && stats.layers) {
        var detailHtml = '';
        stats.layers.forEach(function(layer) {
            var tokens = getLayerTokenCount(layer);
            var layerLabel = layer.display_name || layer.displayName || layer.name || 'unknown';
            detailHtml += escapeHtml(layerLabel) + ': ' + formatTokenNumber(tokens) + ' tokens<br>';
        });
        detailHtml += '<span style="color:rgba(148,163,184,0.52)">remaining: ' + escapeHtml(formatTokenNumber(stats.remaining || 0)) + ' tokens</span><br>';
        detailHtml += '<span style="color:rgba(148,163,184,0.52)">hint: ' + escapeHtml(contextHintText(stats.status)) + '</span>';
        detailEl.innerHTML = detailHtml;
    }

    var oldInfo = document.getElementById('rpContextInfo');
    if (oldInfo) {
        oldInfo.innerHTML = '<div style="margin-bottom:8px;">'
            + '<span style="color:rgba(148,163,184,0.35);">Status</span><br>'
            + '<span class="rp-context-status ' + escapeHtml(String(stats.status || 'unknown')) + '">' + escapeHtml(String(stats.status || 'unknown').toUpperCase()) + '</span>'
            + '</div>'
            + '<div style="margin-bottom:8px;">'
            + '<span style="color:rgba(148,163,184,0.35);">Remaining</span><br>'
            + '<span style="color:#CBD5E1;font-family:Consolas,monospace;">' + formatTokenNumber(stats.remaining || 0) + '</span>'
            + '</div>'
            + '<div style="margin-bottom:8px;">'
            + '<span style="color:rgba(148,163,184,0.35);">Hint</span><br>'
            + '<span style="color:#94A3B8;font-size:12px;">' + escapeHtml(contextHintText(stats.status)) + '</span>'
            + '</div>'
            + '<div>'
            + '<span style="color:rgba(148,163,184,0.35);">Tokens</span><br>'
            + '<span style="color:#CBD5E1;font-family:Consolas,monospace;">' + formatTokenNumber(stats.used_tokens) + ' / ' + formatTokenNumber(stats.max_tokens) + '</span>'
            + '</div>';
    }
}

function contextHintText(status) {
    if (status === 'critical') return 'Compress recommended';
    if (status === 'warning') return 'Monitor usage';
    return 'Context healthy';
}

function formatTokenNumber(num) {
    if (!Number.isFinite(num)) {
        return '0';
    }
    if (num >= 1000) {
        return (num / 1000).toFixed(1) + 'k';
    }
    return num.toString();
}

function getLayerTokenCount(layer) {
    if (!layer || typeof layer !== 'object') {
        return 0;
    }
    if (Number.isFinite(layer.est_tokens)) {
        return layer.est_tokens;
    }
    if (Number.isFinite(layer.estTokens)) {
        return layer.estTokens;
    }
    if (Number.isFinite(layer.size_chars)) {
        return Math.round(layer.size_chars / 3);
    }
    if (Number.isFinite(layer.sizeChars)) {
        return Math.round(layer.sizeChars / 3);
    }
    if (Number.isFinite(layer.size)) {
        return Math.round(layer.size / 3);
    }
    return 0;
}

function compressContext() {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
        addLog('warn', 'WebSocket not connected');
        return;
    }

    if (isAgentRunning) {
        addLog('warn', 'Cannot compress while agent is running');
        return;
    }

    var btn = document.querySelector('.compress-btn');
    if (btn) {
        btn.disabled = true;
        btn.textContent = 'Compressing...';
    }

    ws.send(JSON.stringify({
        action: 'compress_context',
        agent_id: currentAgentId,
        user_id: WEBCAT_USER_ID,
        session: getSessionKey(),
        message: 'session'
    }));

    addLog('info', 'Context compression requested');
}

function handleCompressResult(result) {
    var btn = document.querySelector('.compress-btn');
    if (btn) {
        btn.disabled = false;
        btn.textContent = 'Compress';
    }

    if (!result) return;

    if (result.success) {
        var freed = result.freed_tokens || 0;
        addSystemMessage('Context compressed: freed ' + formatTokenNumber(freed) + ' tokens');
        addLog('info', 'Context compressed successfully, freed ' + freed + ' tokens');
        setTimeout(fetchContextStats, 500);
    } else {
        addSystemMessage('Compression failed: ' + (result.error || 'Unknown error'));
        addLog('error', 'Context compression failed: ' + (result.error || 'Unknown error'));
    }
}

setInterval(function() {
    if (ws && ws.readyState === WebSocket.OPEN && currentPage === 'chat') {
        fetchContextStats();
    }
}, 30000);
