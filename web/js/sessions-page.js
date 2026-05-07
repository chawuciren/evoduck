// ===== Sessions Page =====

function fetchSessions() {
    if (!sendWSRequest('get_sessions')) renderSessions();
}

function renderSessions() {
    var container = document.getElementById('sessionsList');
    if (!container) return;
    if (!sessionsData || sessionsData.length === 0) {
        container.innerHTML = '<div class="empty-state"><div class="empty-state-icon">\uD83D\uDCDD</div><div class="empty-state-text">No active sessions</div><div class="empty-state-detail">Start a conversation to open a session trace.</div></div>';
        return;
    }
    container.innerHTML = sessionsData.map(function(session) {
        return '<div class="session-item">'
            + '<div class="session-info">'
            + '<div class="session-id">' + session.key + '</div>'
            + '<div class="session-meta">Messages: ' + session.message_count + ' | Updated: ' + formatTime(session.updated_at) + '</div>'
            + '</div>'
            + '<div class="session-actions">'
            + '<button class="session-btn" onclick="viewSession(\'' + session.key + '\')">View</button>'
            + '</div></div>';
    }).join('');
}

function refreshSessions() {
    addLog('info', 'Refreshing sessions list...');
    fetchSessions();
}

function viewSession(id) {
    addLog('info', 'Viewing session: ' + id);
    var agentId = '';
    if (typeof id === 'string') {
        var match = id.match(/agent:([^:]+)/);
        if (match) agentId = match[1];
    }
    openSpecificSession(id, agentId);
}
