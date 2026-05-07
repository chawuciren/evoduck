// ===== Logs Page =====

var logsData = [];

function initLogs() {
    logsData = [];
    fetchLogs();
}

function fetchLogs() {
    var levelEl = document.getElementById('logLevel');
    var level = levelEl ? levelEl.value : 'all';
    if (!sendWSRequest('get_logs', { level: level })) renderLogs();
}

function renderLogs() {
    var container = document.getElementById('logsList');
    if (!container) return;
    if (!logsData || logsData.length === 0) {
        container.innerHTML = '<div class="empty-state"><div class="empty-state-icon">\uD83D\uDCCB</div><div class="empty-state-text">No logs available</div><div class="empty-state-detail">Event stream is idle until runtime activity is captured.</div></div>';
        return;
    }
    container.innerHTML = logsData.map(function(log) {
        var level = (log.level || 'info').toLowerCase();
        var message = String(log.message || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
        return '<article class="log-entry ' + level + '">'
            + '<div class="log-entry-header">'
            + '<span class="log-time">' + (log.time || '--:--:--') + '</span>'
            + '<span class="log-level">[' + level.toUpperCase() + ']</span>'
            + '</div>'
            + '<pre class="log-message">' + message + '</pre>'
            + '</article>';
    }).join('');
    container.scrollTop = container.scrollHeight;
}

function addLog(level, message) {
    logsData.push({ level: level, message: message, time: new Date().toTimeString().split(' ')[0] });
    if (currentPage === 'logs') renderLogs();
}

function refreshLogs() { fetchLogs(); }
function clearLogs() { logsData = []; renderLogs(); }

function formatTime(dateStr) {
    if (!dateStr) return 'unknown';
    try { return new Date(dateStr).toLocaleString(); }
    catch (e) { return dateStr; }
}
