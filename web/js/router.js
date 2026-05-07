// ===== Page Management Functions =====

function switchPage(page) {
    currentPage = page;
    document.querySelectorAll('.page-container').forEach(function(p) {
        p.classList.remove('active');
    });
    var targetPage = document.getElementById('page-' + page);
    if (targetPage) {
        targetPage.classList.add('active');
    }

    if (page === 'schedules') {
        fetchSchedules();
    }

    if (page === 'knowledge') {
        fetchKnowledge();
        if (!selectedKnowledgePath) {
            resetKnowledgeEditor();
        }
    }

    if (page === 'memory') {
        fetchMemory();
    }

    if (page === 'diagnostics') {
        fetchDiagnostics();
    }

    if (page === 'logs') {
        setTimeout(function() {
            var container = document.getElementById('logsList');
            if (container) container.scrollTop = container.scrollHeight;
        }, 50);
    }
}

function initPages() {
    fetchAgents();
    fetchSkills();
    fetchSessions();
    fetchSchedules();
    fetchSettings();
    fetchDiagnostics();
    fetchMemory();
    fetchKnowledge();
    initLogs();
    fetchContextStats();
}

function sendWSRequest(action, extra) {
    extra = extra || {};
    if (!ws || ws.readyState !== WebSocket.OPEN) {
        addLog('warn', 'WebSocket not connected, cannot fetch ' + action);
        return false;
    }
    ws.send(JSON.stringify(Object.assign({ action: action }, extra)));
    return true;
}
