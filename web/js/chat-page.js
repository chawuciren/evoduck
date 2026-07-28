// ===== Chat Page =====

function updateSessionViewState() {
    var backBtn = document.getElementById('backToMainChatBtn');
    if (!backBtn) return;
    backBtn.style.display = WEBCAT_EXPLICIT_SESSION_KEY ? 'inline-flex' : 'none';
}

function returnToMainChat() {
    if (!WEBCAT_EXPLICIT_SESSION_KEY) return;
    WEBCAT_EXPLICIT_SESSION_KEY = '';
    currentStreamMessage = null;
    isAgentRunning = false;
    runningSessionKey = '';
    clearPendingMedia();
    updateComposerVisionState();
    updateSendButton();
    clearChatMessages();
    var sessionKey = getSessionKey();
    requestAgentHistory(sessionKey);
    requestTaskStatus(sessionKey);
    fetchContextStats();
    updateSessionViewState();
    addSystemMessage('Returned to main chat session');
    addLog('info', 'Returned to main chat session: ' + sessionKey);
}

function onAgentChange() {
    var selector = document.getElementById('agentSelect');
    var newAgentId = selector.value || '';
    if (newAgentId === currentAgentId) return;

    currentAgentId = newAgentId;
    WEBCAT_EXPLICIT_SESSION_KEY = '';
    updateSessionViewState();
    currentStreamMessage = null;
    isAgentRunning = false;
    runningSessionKey = '';
    clearPendingMedia();
    updateComposerVisionState();
    updateSendButton();
    clearChatMessages();

    var sessionKey = getSessionKey();
    requestAgentHistory(sessionKey);
    requestTaskStatus(sessionKey);
    fetchContextStats();
    fetchSchedules();
    fetchDiagnostics();

    if (currentAgentId) {
        addSystemMessage('Switched to Agent: ' + currentAgentId);
        addLog('info', 'Agent switched to: ' + currentAgentId + ', session: ' + sessionKey);
    } else {
        addSystemMessage('Switched to Default Agent');
        addLog('info', 'Switched to default agent, session: ' + sessionKey);
    }
}

function switchToAgent(agentId) {
    if (agentId === currentAgentId) {
        switchPage('chat');
        document.querySelectorAll('.nav-item').forEach(function(i) { i.classList.remove('active'); });
        document.querySelector('.nav-item[data-page="chat"]').classList.add('active');
        return;
    }

    currentAgentId = agentId;
    WEBCAT_EXPLICIT_SESSION_KEY = '';
    updateSessionViewState();
    currentStreamMessage = null;
    isAgentRunning = false;
    runningSessionKey = '';
    clearPendingMedia();
    updateComposerVisionState();
    updateSendButton();

    var selector = document.getElementById('agentSelect');
    if (selector) selector.value = agentId;

    clearChatMessages();

    var sessionKey = getSessionKey();
    requestAgentHistory(sessionKey);
    requestTaskStatus(sessionKey);
    fetchContextStats();
    fetchDiagnostics();

    switchPage('chat');
    document.querySelectorAll('.nav-item').forEach(function(i) { i.classList.remove('active'); });
    document.querySelector('.nav-item[data-page="chat"]').classList.add('active');

    addSystemMessage('Switched to Agent: ' + agentId);
    addLog('info', 'Switched to agent ' + agentId + ', session: ' + sessionKey);
}

function openSpecificSession(sessionKey, agentId) {
    if (!sessionKey) return;
    if (agentId) {
        currentAgentId = agentId;
        var selector = document.getElementById('agentSelect');
        if (selector) selector.value = agentId;
    }
    WEBCAT_EXPLICIT_SESSION_KEY = sessionKey;
    updateSessionViewState();
    isAgentRunning = false;
    runningSessionKey = '';
    clearPendingMedia();
    updateComposerVisionState();
    updateSendButton();
    clearChatMessages();
    requestAgentHistory(sessionKey);
    requestTaskStatus(sessionKey);
    fetchContextStats();
    fetchDiagnostics();
    switchPage('chat');
    document.querySelectorAll('.nav-item').forEach(function(i) { i.classList.remove('active'); });
    document.querySelector('.nav-item[data-page="chat"]').classList.add('active');
    addSystemMessage('Opened session view: ' + sessionKey);
    addLog('info', 'Opened session: ' + sessionKey);
}

function getSessionKey() {
    if (WEBCAT_EXPLICIT_SESSION_KEY) return WEBCAT_EXPLICIT_SESSION_KEY;
    if (currentAgentId) return 'agent:' + currentAgentId + ':user:' + WEBCAT_USER_ID + ':ws';
    return WEBCAT_SESSION_KEY;
}

function clearChatMessages() {
    var container = document.getElementById('chatMessages');
    if (container) container.innerHTML = '';
    updateChatScrollButton();
    currentPlan = null;
    currentIteration = 0;
    hideTaskPanel();
    resetContextInfo();
}

function requestAgentHistory(sessionKey) {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify({
        action: 'get_history',
        session: sessionKey,
        agent_id: currentAgentId,
        user_id: WEBCAT_USER_ID,
        limit: 50
    }));
}

function requestTaskStatus(sessionKey) {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify({
        action: 'get_task_status',
        session: sessionKey,
        agent_id: currentAgentId,
        user_id: WEBCAT_USER_ID
    }));
}

updateSessionViewState();

function handleCommandAction(action, data) {
    data = data || {};
    switch (action) {
        case 'new_session':
            WEBCAT_EXPLICIT_SESSION_KEY = '';
            updateSessionViewState();
            document.getElementById('chatMessages').innerHTML = '';
            updateChatScrollButton();
            currentStreamMessage = null;
            isAgentRunning = false;
            clearPendingMedia();
    updateComposerVisionState();
    updateSendButton();
            // 不在此处补 addSystemMessage —— 后端会通过 EndMessage 下发结束提示，
            // 避免与后端消息重复。
            addLog('info', 'New session started');
            break;
        case 'switch_agent':
            if (data.agent_id) {
                currentAgentId = data.agent_id;
                WEBCAT_EXPLICIT_SESSION_KEY = '';
                updateSessionViewState();
                var selector = document.getElementById('agentSelect');
                if (selector) selector.value = data.agent_id;
                document.getElementById('chatMessages').innerHTML = '';
                updateChatScrollButton();
                currentStreamMessage = null;
                isAgentRunning = false;
                clearPendingMedia();
    updateComposerVisionState();
    updateSendButton();
                var sessionKey = getSessionKey();
                requestAgentHistory(sessionKey);
                fetchDiagnostics();
                addLog('info', 'Switched to agent: ' + data.agent_id);
            }
            break;
        case 'resume_session':
            // 恢复后当前会话已是归档消息，重新拉取历史刷新 UI（与 /new 一致的本地状态清理）。
            document.getElementById('chatMessages').innerHTML = '';
            updateChatScrollButton();
            currentStreamMessage = null;
            isAgentRunning = false;
            clearPendingMedia();
    updateComposerVisionState();
    updateSendButton();
            requestAgentHistory(getSessionKey());
            addLog('info', 'Resumed session: ' + (data.archive_id || ''));
            break;
        case 'clear_ui':
            document.getElementById('chatMessages').innerHTML = '';
            updateChatScrollButton();
            currentStreamMessage = null;
            isAgentRunning = false;
            clearPendingMedia();
    updateComposerVisionState();
    updateSendButton();
            break;
    }
}
