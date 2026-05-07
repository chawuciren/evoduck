// ===== Schedules Page =====

function fetchSchedules() {
    if (!sendWSRequest('get_schedules', {
        agent_id: currentAgentId,
        user_id: WEBCAT_USER_ID
    })) renderSchedules();
}

function refreshSchedules() {
    addLog('info', 'Refreshing schedules...');
    fetchSchedules();
    if (selectedScheduleRunId) fetchScheduleRuns(selectedScheduleRunId);
}

function applySchedulePreset(value) {
    var cron = document.getElementById('scheduleCron');
    if (!cron) return;
    cron.value = value || '';
}

function renderSchedules() {
    var container = document.getElementById('schedulesList');
    if (!container) return;
    if (!schedulesData || schedulesData.length === 0) {
        container.innerHTML = '<div class="empty-state"><div class="empty-state-icon">⏰</div><div class="empty-state-text">No schedules</div><div class="empty-state-detail">Create a schedule to automate repeated prompts for the current user.</div></div>';
        return;
    }

    container.innerHTML = schedulesData.map(function(schedule) {
        var statusClass = schedule.enabled ? 'connected' : 'disconnected';
        var statusText = schedule.enabled ? 'enabled' : 'disabled';
        var lastRun = schedule.last_run_at ? formatTime(schedule.last_run_at) : 'never';
        var lastSuccess = schedule.last_success_at ? formatTime(schedule.last_success_at) : 'never';
        var triggerSource = schedule.last_trigger_source ? escapeHtml(schedule.last_trigger_source) : 'never';
        var executionSession = schedule.execution_session_key ? escapeHtml(schedule.execution_session_key) : '—';
        var concurrencyPolicy = schedule.concurrency_policy ? escapeHtml(schedule.concurrency_policy) : '—';
        var desc = schedule.description ? escapeHtml(schedule.description) : '—';
        var lastError = schedule.last_error ? '<div class="schedule-error">Last error: ' + escapeHtml(schedule.last_error) + '</div>' : '';
        return '<div class="session-item schedule-item">'
            + '<div class="session-info">'
            + '<div class="session-id">' + escapeHtml(schedule.name || schedule.id) + '</div>'
            + '<div class="session-meta">ID: ' + escapeHtml(schedule.id) + ' | Cron: ' + escapeHtml(schedule.schedule) + ' | Runs: ' + (schedule.run_count || 0) + '</div>'
            + '<div class="schedule-desc">' + desc + '</div>'
            + '<div class="schedule-prompt">' + escapeHtml(schedule.prompt || '') + '</div>'
            + '<div class="schedule-runtime-meta">Last run: ' + lastRun + ' | Last success: ' + lastSuccess + ' | Last trigger: ' + triggerSource + '</div>'
            + '<div class="schedule-runtime-grid">'
            + '<div class="schedule-runtime-chip"><span>Execution Session</span><code>' + executionSession + '</code></div>'
            + '<div class="schedule-runtime-chip"><span>Concurrency</span><code>' + concurrencyPolicy + '</code></div>'
            + '</div>'
            + lastError
            + '</div>'
            + '<div class="session-actions schedule-actions">'
            + '<span class="status ' + statusClass + '">' + statusText + '</span>'
            + '<button class="session-btn" onclick="openScheduleSession(\'' + escapeJs(schedule.execution_session_key || '') + '\',\'' + escapeJs(schedule.agent_id || '') + '\')">Open Run Session</button>'
            + '<button class="session-btn" onclick="selectScheduleRuns(\'' + escapeJs(schedule.id) + '\')">View Runs</button>'
            + '<button class="session-btn" onclick="triggerScheduledTask(\'' + escapeJs(schedule.id) + '\')">Trigger</button>'
            + '<button class="session-btn" onclick="toggleScheduledTask(\'' + escapeJs(schedule.id) + '\',' + (!schedule.enabled) + ')">' + (schedule.enabled ? 'Disable' : 'Enable') + '</button>'
            + '<button class="session-btn session-btn-danger" onclick="deleteScheduledTask(\'' + escapeJs(schedule.id) + '\')">Delete</button>'
            + '</div></div>';
    }).join('');
}

function fetchScheduleRuns(scheduleId) {
    if (!scheduleId) return;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
        addLog('warn', 'WebSocket not connected');
        return;
    }
    ws.send(JSON.stringify({
        action: 'get_schedule_runs',
        agent_id: currentAgentId,
        user_id: WEBCAT_USER_ID,
        schedule_id: scheduleId,
        limit: 20
    }));
}

function selectScheduleRuns(scheduleId) {
    selectedScheduleRunId = scheduleId || '';
    renderScheduleRuns();
    if (selectedScheduleRunId) fetchScheduleRuns(selectedScheduleRunId);
}

function renderScheduleRuns() {
    var header = document.getElementById('scheduleRunsHeader');
    var container = document.getElementById('scheduleRunsList');
    if (!header || !container) return;
    if (!selectedScheduleRunId) {
        header.textContent = 'Select a schedule to inspect recent runs.';
        container.innerHTML = '<div class="empty-state-detail">No schedule selected.</div>';
        return;
    }
    var selectedSchedule = (schedulesData || []).find(function(item) { return item.id === selectedScheduleRunId; });
    header.textContent = (selectedSchedule && (selectedSchedule.name || selectedSchedule.id) ? (selectedSchedule.name || selectedSchedule.id) : selectedScheduleRunId) + ' · latest 20 runs';
    var runs = scheduleRunsData[selectedScheduleRunId] || [];
    if (runs.length === 0) {
        container.innerHTML = '<div class="empty-state-detail">No recorded runs yet.</div>';
        return;
    }
    container.innerHTML = runs.map(function(run) {
        var statusClass = run.execution_status === 'success' ? 'schedule-run-success' : (run.execution_status === 'skipped' ? 'schedule-run-skipped' : 'schedule-run-failed');
        var finishedAt = run.finished_at ? formatTime(run.finished_at) : '—';
        var startedAt = run.started_at ? formatTime(run.started_at) : '—';
        var deliveryStatus = escapeHtml(run.delivery_status || 'unknown');
        var errorHtml = run.error ? '<div class="schedule-run-error">' + escapeHtml(run.error) + '</div>' : '';
        return '<div class="schedule-run-item ' + statusClass + '">'
            + '<div class="schedule-run-top">'
            + '<span class="schedule-run-status">' + escapeHtml(run.execution_status || 'unknown') + '</span>'
            + '<span class="schedule-run-trigger">' + escapeHtml(run.trigger_source || 'unknown') + '</span>'
            + '</div>'
            + '<div class="schedule-run-meta">Started: ' + startedAt + ' | Finished: ' + finishedAt + '</div>'
            + '<div class="schedule-run-meta">Run ID: ' + escapeHtml(run.run_id || '—') + '</div>'
            + '<div class="schedule-run-meta">Session: ' + escapeHtml(run.session_key || '—') + '</div>'
            + '<div class="schedule-run-meta">Delivery: ' + deliveryStatus + '</div>'
            + errorHtml
            + '</div>';
    }).join('');
}

function createScheduledTask() {
    var name = document.getElementById('scheduleName').value.trim();
    var schedule = document.getElementById('scheduleCron').value.trim();
    var description = document.getElementById('scheduleDescription').value.trim();
    var prompt = document.getElementById('schedulePrompt').value.trim();
    var enabled = document.getElementById('scheduleEnabled').checked;

    if (!name || !schedule || !prompt) {
        addSystemMessage('⚠️ Name, cron, and prompt are required');
        addLog('warn', 'Schedule creation blocked: missing required fields');
        return;
    }

    if (!/^\S+\s+\S+\s+\S+\s+\S+\s+\S+$/.test(schedule)) {
        addSystemMessage('⚠️ Cron must use 5 fields, for example: 0 9 * * *');
        addLog('warn', 'Schedule creation blocked: invalid cron format');
        return;
    }

    if (!ws || ws.readyState !== WebSocket.OPEN) {
        addLog('warn', 'WebSocket not connected');
        return;
    }

    ws.send(JSON.stringify({
        action: 'create_schedule',
        agent_id: currentAgentId,
        user_id: WEBCAT_USER_ID,
        name: name,
        description: description,
        schedule: schedule,
        prompt: prompt,
        enabled: enabled
    }));
    addLog('info', 'Creating schedule: ' + name);
}

function toggleScheduledTask(scheduleId, enabled) {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
        addLog('warn', 'WebSocket not connected');
        return;
    }
    ws.send(JSON.stringify({
        action: 'set_schedule_enabled',
        agent_id: currentAgentId,
        user_id: WEBCAT_USER_ID,
        schedule_id: scheduleId,
        enabled: enabled
    }));
}

function deleteScheduledTask(scheduleId) {
    if (!confirm('Delete schedule ' + scheduleId + '?')) return;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
        addLog('warn', 'WebSocket not connected');
        return;
    }
    ws.send(JSON.stringify({
        action: 'delete_schedule',
        agent_id: currentAgentId,
        user_id: WEBCAT_USER_ID,
        schedule_id: scheduleId
    }));
}

function triggerScheduledTask(scheduleId) {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
        addLog('warn', 'WebSocket not connected');
        return;
    }
    ws.send(JSON.stringify({
        action: 'trigger_schedule',
        agent_id: currentAgentId,
        user_id: WEBCAT_USER_ID,
        schedule_id: scheduleId
    }));
    addLog('info', 'Triggering schedule: ' + scheduleId);
    selectedScheduleRunId = scheduleId;
    renderScheduleRuns();
}

function openScheduleSession(sessionKey, agentId) {
    if (!sessionKey) {
        addLog('warn', 'No execution session available for this schedule');
        return;
    }
    openSpecificSession(sessionKey, agentId || currentAgentId);
}

function resetScheduleForm() {
    var name = document.getElementById('scheduleName');
    var cron = document.getElementById('scheduleCron');
    var description = document.getElementById('scheduleDescription');
    var prompt = document.getElementById('schedulePrompt');
    var enabled = document.getElementById('scheduleEnabled');
    if (name) name.value = '';
    if (cron) cron.value = '';
    if (description) description.value = '';
    if (prompt) prompt.value = '';
    if (enabled) enabled.checked = true;
}
