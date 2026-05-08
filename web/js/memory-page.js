// ===== Memory Page =====

var memoryData = [];
var selectedMemoryId = '';

function fetchMemory(query) {
    query = query || '';
    if (!sendWSRequest('get_memory', {
        message: query,
        agent_id: currentAgentId,
        user_id: WEBCAT_USER_ID
    })) renderMemory();
}

function renderMemory() {
    var container = document.getElementById('memoryList');
    if (!container) return;
    if (!selectedMemoryId || !(memoryData || []).some(function(entry) { return entry.id === selectedMemoryId; })) {
        selectedMemoryId = memoryData && memoryData.length ? memoryData[0].id : '';
    }
    if (!memoryData || memoryData.length === 0) {
        container.innerHTML = '<div class="empty-state"><div class="empty-state-icon">🧠</div><div class="empty-state-text">No memory entries found</div><div class="empty-state-detail">Create agent, user, or medium memory to populate this view.</div></div>';
        renderMemoryPreview('');
        return;
    }

    container.innerHTML = memoryData.map(function(entry) {
        return renderMemoryItem(entry);
    }).join('');
    renderMemoryPreview(selectedMemoryId);
}

function renderMemoryItem(entry) {
    var summary = entry.description || (entry.content ? entry.content.substring(0, 150) : '');
    if (summary && entry.content && summary !== entry.description && entry.content.length > 150) {
        summary += '...';
    }
    var chips = [];
    if (entry.category === 'medium_memory' && entry.user_id) {
        chips.push('user: ' + entry.user_id);
    }
    if ((entry.category === 'agent_memory' || entry.category === 'user_memory') && entry.agent_id) {
        chips.push('agent: ' + entry.agent_id);
    }
    return '<button class="memory-item' + (entry.id === selectedMemoryId ? ' active' : '') + '" type="button" onclick="selectMemory(\'' + escapeJs(entry.id) + '\')">'
        + '<div class="memory-item-title">' + escapeHtml(getMemoryDisplayName(entry)) + '</div>'
        + '<div class="memory-item-summary">' + escapeHtml(summary || 'No preview available.') + '</div>'
        + '<div class="memory-item-meta">'
        + chips.map(function(chip) { return '<span class="memory-meta-chip">' + escapeHtml(chip) + '</span>'; }).join('')
        + '</div></button>';
}

function selectMemory(id) {
    selectedMemoryId = id || '';
    renderMemory();
}

function renderMemoryPreview(id) {
    var panel = document.getElementById('memoryPreviewPanel');
    if (!panel) return;

    if (!id) {
        panel.innerHTML = '<div class="memory-preview-empty"><div class="empty-state-icon">🔎</div><div class="empty-state-text">Select a memory file</div><div class="empty-state-detail">Choose a file from the list to inspect its content and source path.</div></div>';
        return;
    }

    var entry = (memoryData || []).find(function(item) { return item.id === id; });
    if (!entry) {
        panel.innerHTML = '<div class="memory-preview-empty"><div class="empty-state-icon">🧠</div><div class="empty-state-text">Memory entry unavailable</div><div class="empty-state-detail">Refresh the page to load the latest memory content.</div></div>';
        return;
    }

    var metaRows = [];
    if (entry.agent_id) metaRows.push('<div><strong>Agent:</strong> ' + escapeHtml(entry.agent_id) + '</div>');
    if (entry.user_id) metaRows.push('<div><strong>User:</strong> ' + escapeHtml(entry.user_id) + '</div>');
    if (entry.path) metaRows.push('<div><strong>Path:</strong> ' + escapeHtml(entry.path) + '</div>');
    if (entry.directory) metaRows.push('<div><strong>Directory:</strong> ' + escapeHtml(entry.directory) + '</div>');
    if (entry.tags && entry.tags.length) metaRows.push('<div><strong>Tags:</strong> ' + escapeHtml(entry.tags.join(', ')) + '</div>');
    if (entry.updated_at) metaRows.push('<div><strong>Updated:</strong> ' + escapeHtml(entry.updated_at) + '</div>');

    panel.innerHTML = '<div class="memory-preview-shell">'
        + '<div class="memory-preview-kicker">Memory File</div>'
        + '<div class="memory-preview-title">' + escapeHtml(getMemoryDisplayName(entry)) + '</div>'
        + '<div class="memory-preview-text">' + escapeHtml(entry.description || 'No description') + '</div>'
        + '<div class="memory-preview-section"><div class="memory-preview-section-title">Metadata</div><div class="memory-preview-meta">' + (metaRows.join('') || '<div>None</div>') + '</div></div>'
        + '<div class="memory-preview-section"><div class="memory-preview-section-title">Source</div><div class="memory-preview-source">' + escapeHtml(entry.source || 'Unknown') + '</div></div>'
        + '<div class="memory-preview-section"><div class="memory-preview-section-title">Content</div><pre class="memory-preview-content">' + escapeHtml(entry.content || 'No content') + '</pre></div>'
        + '</div>';
}

function getMemoryDisplayName(entry) {
    var source = entry && entry.source ? String(entry.source) : '';
    if (source) {
        var normalized = source.replace(/\\/g, '/');
        var parts = normalized.split('/');
        var filename = parts[parts.length - 1];
        if (filename) return filename;
    }
    return (entry && (entry.title || entry.id)) || 'Unknown';
}
