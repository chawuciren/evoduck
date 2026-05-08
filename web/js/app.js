// ===== Core App Logic =====

// State variables
var ws = null;
var sessionId = null;
var messageCount = 0;
var reconnectTimer = null;
var reconnectCount = 0;
var countdown = 0;
var currentPage = 'chat';
var currentAgentId = '';
var isAgentRunning = false;
var isAgentStopping = false;
var runningSessionKey = '';
var RECONNECT_INTERVAL = 5;
var GATEWAY_TOKEN_STORAGE_KEY = 'evoduck.gatewayToken';

var WEBCAT_USER_ID = 'admin';
var WEBCAT_SESSION_KEY = 'main';

// Cached data
var agentsData = [];
var skillsData = [];
var skillDetailsData = {};
var sessionsData = [];
var settingsData = {};
var settingsSummaryData = {};
var currentPlan = null;
var currentIteration = 0;
var schedulesData = [];
var scheduleRunsData = {};
var selectedScheduleRunId = '';
var capabilitiesData = null;
var pendingComposerMedia = [];
var composerMediaUploading = false;
var MAX_PENDING_MEDIA_COUNT = 4;
var MAX_PENDING_MEDIA_BYTES = 20 * 1024 * 1024;
var ACCEPTED_IMAGE_MIME_TYPES = ['image/png', 'image/jpeg', 'image/webp', 'image/gif'];

// ---- DOMContentLoaded ----
document.addEventListener('DOMContentLoaded', function() {
    var navItems = document.querySelectorAll('.nav-item');
    navItems.forEach(function(item) {
        item.addEventListener('click', function() {
            var page = this.getAttribute('data-page');
            if (page) {
                switchPage(page);
                navItems.forEach(function(i) { i.classList.remove('active'); });
                this.classList.add('active');
            }
        });
    });

    var userIdInput = document.getElementById('userIdInput');
    if (userIdInput) {
        userIdInput.value = WEBCAT_USER_ID;
        userIdInput.readOnly = true;
        userIdInput.style.opacity = '0.6';
        userIdInput.style.cursor = 'not-allowed';
    }

    var logLevelSelect = document.getElementById('logLevel');
    if (logLevelSelect) {
        logLevelSelect.addEventListener('change', fetchLogs);
    }

    var gatewayTokenInput = document.getElementById('gatewayTokenInput');
    if (gatewayTokenInput) {
        gatewayTokenInput.value = loadGatewayToken();
        gatewayTokenInput.addEventListener('input', function() {
            saveGatewayToken(this.value);
        });
    }

    var wsUrlInput = document.getElementById('wsUrlInput');
    if (wsUrlInput && !wsUrlInput.value) {
        var protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        wsUrlInput.value = protocol + '//' + window.location.host + '/ws';
    }

    initPages();
    initChatScrollControls();
    initComposerMediaControls();
    renderPendingMediaTray();
    updateComposerVisionState();
    updateSidebarVersion();

    initDuckAnimation();
});

function initDuckAnimation() {
    if (!window.DuckAnimator || !window.DUCK_ANIMATION) {
        return;
    }

    try {
        window._duckAnimator = new DuckAnimator('duckArt', 'evoduckAscii', window.DUCK_ANIMATION);
        window._duckAnimator.start();
    } catch (error) {
        console.error('Duck animation init failed:', error);
    }
}

function loadGatewayToken() {
    try {
        return localStorage.getItem(GATEWAY_TOKEN_STORAGE_KEY) || '';
    } catch (error) {
        return '';
    }
}

function saveGatewayToken(token) {
    try {
        if (token) {
            localStorage.setItem(GATEWAY_TOKEN_STORAGE_KEY, token);
            return;
        }
        localStorage.removeItem(GATEWAY_TOKEN_STORAGE_KEY);
    } catch (error) {
        // Ignore storage failures and continue with the in-memory value.
    }
}

function getGatewayToken() {
    var gatewayTokenInput = document.getElementById('gatewayTokenInput');
    return gatewayTokenInput ? gatewayTokenInput.value.trim() : '';
}

function updateSidebarVersion() {
    var el = document.getElementById('sidebarVersionText');
    if (!el) return;
    var version = settingsSummaryData && settingsSummaryData.system ? settingsSummaryData.system.version : '';
    version = String(version || '').trim();
    el.textContent = 'Version ' + (version || '—');
}

// ---- Connection ----
function connect(auto) {
    auto = auto || false;
    var wsUrl = document.getElementById('wsUrlInput').value;
    var userId = document.getElementById('userIdInput').value;
    var gatewayToken = getGatewayToken();
    var connectBtn = document.getElementById('connectBtn');
    var connectionError = document.getElementById('connectionError');

    if (!wsUrl || !userId) {
        showConnectionError('Please enter WebSocket URL and User ID');
        stopAutoReconnect();
        return;
    }

    if (!auto) stopAutoReconnect();

    connectBtn.disabled = true;
    connectBtn.textContent = auto ? 'Connecting (Attempt #' + reconnectCount + ')...' : 'Connecting...';
    connectionError.classList.remove('show');

    saveGatewayToken(gatewayToken);

    var url = wsUrl + '?user_id=' + encodeURIComponent(userId);
    if (gatewayToken) {
        url += '&token=' + encodeURIComponent(gatewayToken);
    }

    try {
        ws = new WebSocket(url);

        ws.onopen = function() {
            stopAutoReconnect();
            document.getElementById('connectionPage').classList.add('hidden');
            document.getElementById('appContainer').classList.add('connected');
            updateStatus('connected', 'Connected');
            document.getElementById('sendBtn').disabled = false;
            addSystemMessage('Connected to EvoDuck WebChat');
            addLog('info', 'WebSocket connected successfully');
            initPages();
            if (currentAgentId) {
                requestAgentHistory(getSessionKey());
                // Request task status to restore running state after refresh
                requestTaskStatus(getSessionKey());
            }
        };

        ws.onmessage = function(event) {
            handleMessage(JSON.parse(event.data));
        };

        ws.onerror = function() {
            showConnectionError('Failed to connect. Please check WebSocket URL.');
            connectBtn.disabled = false;
            connectBtn.textContent = 'Connect';
            addLog('error', 'WebSocket connection failed');
            startAutoReconnect();
        };

        ws.onclose = function() {
            document.getElementById('connectionPage').classList.remove('hidden');
            document.getElementById('appContainer').classList.remove('connected');
            updateStatus('disconnected', 'Disconnected');
            document.getElementById('sendBtn').disabled = true;
            document.getElementById('chatMessages').innerHTML = '';
            updateChatScrollButton();
            sessionId = null;
            ws = null;
            isAgentRunning = false;
            isAgentStopping = false;
            updateSendButton();
            addLog('warn', 'WebSocket connection closed');
            startAutoReconnect();
        };
    } catch (error) {
        showConnectionError('Failed to connect: ' + error.message);
        connectBtn.disabled = false;
        connectBtn.textContent = 'Connect';
        addLog('error', 'Connection exception: ' + error.message);
        startAutoReconnect();
    }
}

function showConnectionError(message) {
    var el = document.getElementById('connectionError');
    el.textContent = message;
    el.classList.add('show');
}

function startAutoReconnect() {
    stopAutoReconnect();
    reconnectCount++;
    countdown = RECONNECT_INTERVAL;
    updateReconnectStatus();

    var countdownTimer = setInterval(function() {
        countdown--;
        if (countdown > 0) {
            updateReconnectStatus();
        } else {
            clearInterval(countdownTimer);
        }
    }, 1000);

    reconnectTimer = setTimeout(function() {
        clearInterval(countdownTimer);
        if (!ws || ws.readyState !== WebSocket.OPEN) {
            connect(true);
        }
    }, RECONNECT_INTERVAL * 1000);
}

function stopAutoReconnect() {
    if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
    }
    reconnectCount = 0;
    countdown = 0;

    var reconnectStatus = document.getElementById('reconnectStatus');
    if (reconnectStatus) reconnectStatus.classList.remove('show');
    var connectBtn = document.getElementById('connectBtn');
    if (connectBtn) {
        connectBtn.textContent = 'Connect';
        connectBtn.disabled = false;
    }
}

function updateReconnectStatus() {
    var reconnectStatus = document.getElementById('reconnectStatus');
    if (reconnectStatus) {
        reconnectStatus.textContent = 'Reconnecting in ' + countdown + 's (Attempt #' + reconnectCount + ')...';
        reconnectStatus.classList.add('show');
    }
    var connectBtn = document.getElementById('connectBtn');
    if (connectBtn) {
        connectBtn.textContent = 'Reconnecting (' + countdown + 's)...';
        connectBtn.disabled = true;
    }
}

// ---- Message Handling ----
function handleMessage(data) {
    console.log('Received:', data);

    switch (data.type) {
        case 'welcome':
            sessionId = data.session_id;
            addSystemMessage('Welcome! Session ID: ' + sessionId);
            break;

        case 'history':
            if (data.messages && Array.isArray(data.messages)) {
                console.log('History received:', data.messages.length, 'messages');
                var container = document.getElementById('chatMessages');
                container.innerHTML = '';
                updateChatScrollButton();
                var displayCount = 0;
                data.messages.forEach(function(msg) {
                    var hasRenderableMedia = Array.isArray(msg.media) && msg.media.length > 0;
                    if (msg.content || hasRenderableMedia) {
                        if (msg.role === 'user') {
                            addMessage('user', msg.content, msg.timestamp, msg.media || []);
                            displayCount++;
                        } else if (msg.role === 'assistant') {
                            addMessage('assistant', msg.content, msg.timestamp, msg.media || []);
                            displayCount++;
                        } else if (msg.role === 'tool') {
                            addToolHistoryMessage(msg.content, msg.timestamp);
                            displayCount++;
                        }
                    }
                });
                addSystemMessage('Loaded ' + displayCount + ' messages from history');
                console.log('Displayed:', displayCount, 'messages');
            }
            break;

        case 'message':
            addMessage('assistant', data.content, data.timestamp, data.media || []);
            document.getElementById('typingIndicator').classList.remove('active');
            break;

        case 'content':
            appendStreamContent(data.content);
            break;

        case 'thinking':
            appendThinkingContent(data.thinking_content);
            break;

        case 'tool_start':
            addToolStartMessage(data.tool_name, data.tool_params);
            addLog('info', 'Tool started: ' + data.tool_name);
            break;

        case 'tool_end':
            addToolEndMessage(data.tool_name, data.tool_result);
            addLog('info', 'Tool completed: ' + data.tool_name);
            break;

        case 'iteration':
            currentIteration = data.iteration || currentIteration;
            addIterationMessage(data.iteration);
            addLog('info', 'Agent iteration: ' + data.iteration);
            if (currentPlan) updateContextInfo(currentPlan, currentIteration);
            break;

        case 'done':
            isFinalResponse = true;
            isAgentRunning = false;
            isAgentStopping = false;
            runningSessionKey = '';
            updateSendButton();
            showFinalResponse();
            finalizeStreamMessage();
            finalizeThinking();
            fetchContextStats(); // 问答完成后立即更新上下文统计
            addLog('info', 'Stream completed');
            break;

        case 'stream':
            if (data.content) appendStreamContent(data.content);
            if (data.done) {
                isAgentRunning = false;
                isAgentStopping = false;
                runningSessionKey = '';
                updateSendButton();
                finalizeStreamMessage();
            }
            break;

        case 'error':
            addSystemMessage('Error: ' + (data.content || 'Unknown error'));
            isAgentRunning = false;
            isAgentStopping = false;
            runningSessionKey = '';
            updateSendButton();
            finalizeStreamMessage();
            addLog('error', data.content || 'Unknown error');
            break;

        case 'agents':
            agentsData = data.agents || [];
            renderAgents();
            updateAgentSelector();
            addLog('info', 'Loaded ' + agentsData.length + ' agents');
            if (!currentAgentId && agentsData.length > 0) {
                var sorted = sortAgents(agentsData);
                var firstAgent = sorted[0];
                currentAgentId = firstAgent.id;
                var selector = document.getElementById('agentSelect');
                if (selector) selector.value = firstAgent.id;
                addSystemMessage('Auto-selected Agent: ' + firstAgent.id);
                requestAgentHistory(getSessionKey());
                requestTaskStatus(getSessionKey());
            }
            break;

        case 'skills':
            skillsData = data.skills || [];
            renderSkills();
            addLog('info', 'Loaded ' + skillsData.length + ' skills');
            break;

        case 'skill_detail':
            if (data.skill && data.skill.name) {
                skillDetailsData[data.skill.name] = data.skill;
                renderSkillPreview(data.skill.name);
                addLog('info', 'Loaded skill detail: ' + data.skill.name);
            }
            break;

        case 'schedules':
            schedulesData = data.schedules || data.tasks || [];
            renderSchedules();
            addLog('info', 'Loaded ' + schedulesData.length + ' schedules');
            break;

        case 'schedule_created':
            if (data.schedule || data.task) {
                schedulesData.unshift(data.schedule || data.task);
                renderSchedules();
            }
            resetScheduleForm();
            addSystemMessage('✓ Schedule created');
            addLog('info', 'Schedule created: ' + (((data.schedule || data.task) && (data.schedule || data.task).name) || 'unknown'));
            break;

        case 'schedule_updated':
            schedulesData = (schedulesData || []).map(function(schedule) {
                if (schedule.id === (data.schedule_id || data.task_id)) {
                    schedule.enabled = !!data.enabled;
                }
                return schedule;
            });
            renderSchedules();
            addLog('info', 'Schedule updated: ' + (data.schedule_id || data.task_id));
            break;

        case 'schedule_deleted':
            schedulesData = (schedulesData || []).filter(function(schedule) {
                return schedule.id !== (data.schedule_id || data.task_id);
            });
            renderSchedules();
            addLog('info', 'Schedule deleted: ' + (data.schedule_id || data.task_id));
            break;

        case 'schedule_triggered':
            addSystemMessage('✓ Schedule triggered: ' + (data.schedule_id || data.task_id));
            addLog('info', 'Schedule triggered: ' + (data.schedule_id || data.task_id) + ' [' + (data.trigger_source || 'manual') + ']');
            fetchSchedules();
            if (selectedScheduleRunId && selectedScheduleRunId === (data.schedule_id || data.task_id)) {
                fetchScheduleRuns(selectedScheduleRunId);
            }
            break;

        case 'schedule_runs':
            scheduleRunsData[data.schedule_id] = data.runs || [];
            if (selectedScheduleRunId === data.schedule_id) {
                renderScheduleRuns();
            }
            break;

        case 'sessions':
            sessionsData = data.sessions || [];
            renderSessions();
            addLog('info', 'Loaded ' + sessionsData.length + ' sessions');
            break;

        case 'settings':
            settingsSummaryData = data.settings || {};
            if (!isSettingsDirty()) {
                settingsData = settingsSummaryData;
            }
            updateSidebarVersion();
            renderSettings();
            addLog('info', 'Loaded settings summary');
            break;

        case 'capabilities':
            capabilitiesData = data.capabilities || null;
            renderDiagnostics();
            renderRightPanelSummaries();
            updateComposerVisionState();
            addLog('info', 'Loaded capability diagnostics');
            break;

        case 'settings_full':
            applySettingsPayload(data);
            setSettingsStatus('Loaded configuration from runtime', 'success');
            addLog('info', 'Loaded full settings configuration');
            break;

        case 'settings_validation':
            settingsIssues = data.issues || [];
            renderSettings();
            renderSettingsIssues();
            if (data.valid) {
                setSettingsStatus('Configuration is valid', 'success');
                addLog('info', 'Settings validation passed');
            } else if (data.error) {
                setSettingsStatus('Validation failed: ' + data.error, 'error');
                addLog('error', 'Settings validation failed: ' + data.error);
            } else {
                setSettingsStatus('Validation found ' + settingsIssues.length + ' issue(s)', 'warn');
                addLog('warn', 'Settings validation found ' + settingsIssues.length + ' issue(s)');
            }
            break;

        case 'settings_saved':
            settingsIssues = [];
            settingsLastSavedSnapshot = cloneSettingsValue(settingsDraft || {});
            updateSettingsDirtyState();
            renderSettingsIssues();
            fetchSettings();
            setSettingsStatus(formatSettingsResultStatus('Configuration saved and applied', data.result), 'success');
            addLog('info', 'Settings saved successfully');
            break;

        case 'settings_reloaded':
            settingsIssues = [];
            renderSettingsIssues();
            fetchSettings();
            setSettingsStatus(formatSettingsResultStatus('Configuration reloaded', data.result), 'success');
            addLog('info', 'Settings reloaded from disk');
            break;

        case 'settings_changed':
            if (!isSettingsDirty()) {
                fetchSettings();
                setSettingsStatus('Settings changed remotely and were refreshed', 'warn');
            } else {
                setSettingsStatus('Settings changed remotely. Reload when ready to avoid losing local edits.', 'warn');
            }
            addLog('info', 'Settings change broadcast received');
            break;

        case 'settings_save_failed':
            setSettingsStatus('Settings ' + (data.stage || 'save') + ' failed: ' + (data.error || 'unknown error'), 'error');
            addLog('error', 'Settings ' + (data.stage || 'save') + ' failed: ' + (data.error || 'unknown error'));
            break;

        case 'logs':
            logsData = data.logs || [];
            renderLogs();
            break;

        case 'memory':
            memoryData = data.memory || [];
            renderMemory();
            addLog('info', 'Loaded ' + memoryData.length + ' memory entries');
            break;

        case 'knowledge':
            knowledgeData = data.knowledge || [];
            createdKnowledgeDirectories = (data.directories || []).slice().sort();
            renderKnowledge();
            addLog('info', 'Loaded ' + knowledgeData.length + ' knowledge entries');
            break;

        case 'knowledge_entry':
            if (data.entry && data.entry.path) {
                upsertKnowledgeEntry(data.entry);
                populateKnowledgeEditor(data.entry);
                renderKnowledge();
                addLog('info', 'Loaded knowledge entry: ' + data.entry.path);
            }
            break;

        case 'knowledge_entry_saved':
            if (data.entry && data.entry.path) {
                upsertKnowledgeEntry(data.entry);
                populateKnowledgeEditor(data.entry);
                renderKnowledge();
                addLog('info', 'Saved knowledge entry: ' + data.entry.path);
            }
            break;

        case 'knowledge_entry_deleted':
            if (data.path) {
                removeKnowledgeEntry(data.path);
                addLog('info', 'Deleted knowledge entry: ' + data.path);
            }
            break;

        case 'knowledge_entry_moved':
            if (data.entry && data.entry.path) {
                moveKnowledgeEntryState(data.from_path || '', data.entry);
                addLog('info', 'Moved knowledge entry: ' + (data.from_path || 'unknown') + ' -> ' + data.entry.path);
            }
            break;

        case 'knowledge_directory_created':
            if (data.directory) {
                addKnowledgeDirectory(data.directory);
                addLog('info', 'Created knowledge directory: ' + data.directory);
            }
            break;

        case 'knowledge_directory_deleted':
            if (data.directory) {
                removeKnowledgeDirectory(data.directory);
                addLog('info', 'Deleted knowledge directory: ' + data.directory);
            }
            break;

        case 'pong':
            break;

        case 'command':
            if (data.content) addCommandResult(data.content);
            if (data.action) handleCommandAction(data.action, data.action_data || {});
            addLog('info', 'Command executed');
            break;

        case 'cancelled':
            finishCancelledTask();
            break;

        case 'plan':
            currentPlan = data.plan || null;
            renderTaskPanel();
            if (currentPlan) {
                addLog('info', 'Task plan received: ' + (currentPlan.intent || '').substring(0, 50) || 'N/A');
                updateContextInfo(currentPlan, currentIteration);
            }
            break;

        case 'plan_update':
            if (data.plan) currentPlan = data.plan;
            renderTaskPanel();
            if (currentPlan) {
                var completed = (currentPlan.sub_tasks || []).filter(function(t) {
                    return t.status === 'done' || t.status === 'skipped';
                }).length;
                var total = (currentPlan.sub_tasks || []).length;
                addLog('info', 'Task plan updated: ' + completed + '/' + total + ' subtasks done');
                updateContextInfo(currentPlan, currentIteration);
            }
            break;

        case 'context_stats':
            renderContextStats(data.context_stats);
            break;

        case 'compress_result':
            handleCompressResult(data.compress_result);
            break;

        case 'task_status':
            if (data.running) {
                isAgentRunning = true;
                isAgentStopping = false;
                runningSessionKey = data.session_id || '';
                updateSendButton();
                document.getElementById('typingIndicator').classList.add('active');
                addSystemMessage('Task is running (started ' + Math.round(data.duration || 0) + 's ago)');
                addLog('info', 'Restored running state from server: session=' + data.session_id);
            } else if (runningSessionKey === data.session_id) {
                isAgentRunning = false;
                isAgentStopping = false;
                runningSessionKey = '';
                updateSendButton();
                document.getElementById('typingIndicator').classList.remove('active');
            }
            break;
    }
}

// ---- Send / Stop ----
function sendComposerMessage(messageOverride) {
    var input = document.getElementById('messageInput');
    if (!input) return;

    var message = typeof messageOverride === 'string' ? messageOverride.trim() : input.value.trim();
    var uploadedMedia = getUploadedComposerMedia();
    if ((!message && uploadedMedia.length === 0) || !ws || ws.readyState !== WebSocket.OPEN || composerMediaUploading) return;
    if (uploadedMedia.length > 0 && !composerSupportsVision()) {
        addSystemMessage('Current model does not support image understanding.');
        return;
    }

    var sessionKey = getSessionKey();
    ws.send(JSON.stringify({
        action: 'stream',
        message: message,
        media: uploadedMedia.map(function(item) {
            return {
                id: item.id || '',
                type: item.type || 'image',
                name: item.name || '',
                mime_type: item.mime_type || '',
                size: item.size || 0,
                file_size: item.size || 0,
                url: item.url || ''
            };
        }),
        agent_id: currentAgentId,
        user_id: WEBCAT_USER_ID,
        session: sessionKey
    }));
    addMessage('user', message, null, uploadedMedia);
    input.value = '';
    hideCommandDropdown();
    clearPendingMedia();

    currentStreamMessage = null;
    isFinalResponse = false;
    isAgentRunning = true;
    isAgentStopping = false;
    runningSessionKey = sessionKey;
    updateSendButton();

    document.getElementById('typingIndicator').classList.add('active');
    addLog('info', 'Message to ' + (currentAgentId || 'default') + ' [' + sessionKey + ']: text=' + message.length + ' media=' + uploadedMedia.length);
}

function sendMessage() {
    sendComposerMessage();
}

function startNewSession() {
    sendComposerMessage('/new');
}

function beginStoppingTask(sessionKey) {
    if (isAgentStopping) return;
    isAgentStopping = true;

    var btn = document.getElementById('sendBtn');
    if (btn) {
        btn.textContent = '⏳ Stopping...';
        btn.disabled = true;
        btn.classList.add('stop-btn');
        btn.onclick = null;
    }

    addSystemMessage('⏳ Stopping task...');
    addLog('warn', 'Stopping task for session: ' + sessionKey);
}

function finishCancelledTask() {
    if (!isAgentStopping) {
        beginStoppingTask(runningSessionKey || getSessionKey());
    }

    finalizeStreamMessage();
    currentStreamMessage = null;
    isAgentRunning = false;
    isAgentStopping = false;
    runningSessionKey = '';
    updateSendButton();
    addSystemMessage('✅ Task stopped successfully');
    hideTaskPanel();
    document.getElementById('typingIndicator').classList.remove('active');
    addLog('warn', 'Task cancelled');
}

function stopAgent() {
    if (!isAgentRunning || !ws || ws.readyState !== WebSocket.OPEN) return;
    var sessionKey = runningSessionKey || getSessionKey();

    beginStoppingTask(sessionKey);

    ws.send(JSON.stringify({
        action: 'cancel',
        session: sessionKey,
        agent_id: currentAgentId,
        user_id: WEBCAT_USER_ID
    }));
}

function updateSendButton() {
    var btn = document.getElementById('sendBtn');
    var input = document.getElementById('messageInput');
    if (!btn) return;

    var hasMessage = !!(input && input.value.trim());
    var hasUploadedMedia = getUploadedComposerMedia().length > 0;
    if (isAgentRunning) {
        btn.textContent = hasMessage || hasUploadedMedia ? 'Send' : '\u25A0 Stop';
        btn.disabled = composerMediaUploading ? true : false;
        btn.classList.toggle('stop-btn', !(hasMessage || hasUploadedMedia));
        btn.onclick = hasMessage || hasUploadedMedia ? sendMessage : stopAgent;
        if (input) input.placeholder = 'Agent is running... (type to interrupt, ESC to cancel)';
    } else {
        btn.textContent = composerMediaUploading ? 'Uploading...' : 'Send';
        btn.disabled = composerMediaUploading || (!hasMessage && !hasUploadedMedia);
        btn.classList.remove('stop-btn');
        btn.onclick = sendMessage;
        if (input) input.placeholder = composerSupportsVision() ? 'Type a message... (use / for commands)' : 'Type a message... (current model is text-only)';
    }
}

function initComposerMediaControls() {
    var input = document.getElementById('messageInput');
    var fileInput = document.getElementById('mediaFileInput');
    if (fileInput && fileInput.dataset.bound !== 'true') {
        fileInput.addEventListener('change', function(event) {
            queueComposerFiles(event.target.files);
            fileInput.value = '';
        });
        fileInput.dataset.bound = 'true';
    }
    if (input && input.dataset.mediaBound !== 'true') {
        input.addEventListener('paste', handleComposerPaste);
        input.addEventListener('dragover', handleComposerDragOver);
        input.addEventListener('drop', handleComposerDrop);
        input.dataset.mediaBound = 'true';
    }
}

function openMediaPicker() {
    if (!composerSupportsVision()) {
        addSystemMessage('Current model does not support image understanding.');
        return;
    }
    if (pendingComposerMedia.length >= MAX_PENDING_MEDIA_COUNT) {
        addSystemMessage('You can upload up to ' + MAX_PENDING_MEDIA_COUNT + ' images per message.');
        return;
    }
    var fileInput = document.getElementById('mediaFileInput');
    if (fileInput) fileInput.click();
}

function handleComposerPaste(event) {
    if (!event.clipboardData || !event.clipboardData.items) return;
    var files = [];
    for (var i = 0; i < event.clipboardData.items.length; i++) {
        var item = event.clipboardData.items[i];
        if (item && item.kind === 'file') {
            var file = item.getAsFile();
            if (file) files.push(file);
        }
    }
    if (!files.length) return;
    event.preventDefault();
    queueComposerFiles(files);
}

function handleComposerDragOver(event) {
    if (!event.dataTransfer) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = 'copy';
}

function handleComposerDrop(event) {
    if (!event.dataTransfer || !event.dataTransfer.files || !event.dataTransfer.files.length) return;
    event.preventDefault();
    queueComposerFiles(event.dataTransfer.files);
}

function queueComposerFiles(fileList) {
    if (!fileList || !fileList.length) return;
    if (!composerSupportsVision()) {
        addSystemMessage('Current model does not support image understanding.');
        return;
    }
    var availableSlots = MAX_PENDING_MEDIA_COUNT - pendingComposerMedia.length;
    if (availableSlots <= 0) {
        addSystemMessage('You can upload up to ' + MAX_PENDING_MEDIA_COUNT + ' images per message.');
        return;
    }
    var files = Array.prototype.slice.call(fileList, 0, availableSlots);
    files.forEach(addPendingMediaFile);
}

function addPendingMediaFile(file) {
    if (!file) return;
    if (ACCEPTED_IMAGE_MIME_TYPES.indexOf(file.type) === -1) {
        addSystemMessage('Only PNG, JPEG, WEBP, and GIF images are supported.');
        return;
    }
    if (file.size > MAX_PENDING_MEDIA_BYTES) {
        addSystemMessage('Image exceeds the 20MB upload limit.');
        return;
    }
    var pendingItem = {
        type: 'image',
        name: file.name || 'image',
        mime_type: file.type || 'image/png',
        size: file.size || 0,
        status: 'uploading',
        file: file,
        preview_url: ''
    };
    if (typeof URL !== 'undefined' && URL.createObjectURL) {
        pendingItem.preview_url = URL.createObjectURL(file);
    }
    pendingComposerMedia.push(pendingItem);
    composerMediaUploading = true;
    renderPendingMediaTray();
    updateSendButton();
    uploadPendingMediaItem(pendingItem);
}

function uploadPendingMediaItem(item) {
    var formData = new FormData();
    formData.append('file', item.file, item.name || 'image');
    var headers = {};
    var token = getGatewayToken();
    if (token) {
        headers.Authorization = 'Bearer ' + token;
    }
    fetch('/api/media/upload', {
        method: 'POST',
        body: formData,
        headers: headers
    }).then(function(response) {
        if (!response.ok) {
            throw new Error('upload failed with status ' + response.status);
        }
        return response.json();
    }).then(function(payload) {
        item.id = payload.id || '';
        item.url = payload.url || '';
        item.name = payload.name || item.name;
        item.mime_type = payload.mime_type || item.mime_type;
        item.size = payload.size || item.size;
        item.status = 'uploaded';
        delete item.file;
    }).catch(function(error) {
        item.status = 'failed';
        item.error = error && error.message ? error.message : 'upload failed';
        addSystemMessage('Image upload failed: ' + item.name);
    }).finally(function() {
        composerMediaUploading = pendingComposerMedia.some(function(entry) {
            return entry && entry.status === 'uploading';
        });
        renderPendingMediaTray();
        updateSendButton();
    });
}

function renderPendingMediaTray() {
    var tray = document.getElementById('pendingMediaTray');
    var uploadBtn = document.getElementById('mediaUploadBtn');
    if (!tray) return;
    if (!pendingComposerMedia.length) {
        tray.innerHTML = '';
        tray.style.display = 'none';
    } else {
        tray.innerHTML = renderPendingComposerMedia(pendingComposerMedia);
        tray.style.display = 'flex';
    }
    if (uploadBtn) {
        uploadBtn.disabled = !composerSupportsVision() || pendingComposerMedia.length >= MAX_PENDING_MEDIA_COUNT;
        uploadBtn.classList.toggle('disabled', uploadBtn.disabled);
    }
}

function removePendingMedia(index) {
    if (index < 0 || index >= pendingComposerMedia.length) return;
    var removed = pendingComposerMedia.splice(index, 1)[0];
    if (removed && removed.preview_url && typeof URL !== 'undefined' && URL.revokeObjectURL) {
        URL.revokeObjectURL(removed.preview_url);
    }
    composerMediaUploading = pendingComposerMedia.some(function(entry) {
        return entry && entry.status === 'uploading';
    });
    renderPendingMediaTray();
    updateSendButton();
}

function clearPendingMedia() {
    pendingComposerMedia.forEach(function(item) {
        if (item && item.preview_url && typeof URL !== 'undefined' && URL.revokeObjectURL) {
            URL.revokeObjectURL(item.preview_url);
        }
    });
    pendingComposerMedia = [];
    composerMediaUploading = false;
    renderPendingMediaTray();
}

function getUploadedComposerMedia() {
    return pendingComposerMedia.filter(function(item) {
        return item && item.status === 'uploaded' && item.url;
    });
}

function getCurrentAgentCapabilities() {
    if (!capabilitiesData || !Array.isArray(capabilitiesData.agents)) return null;
    if (!currentAgentId) return capabilitiesData.agents[0] || null;
    for (var i = 0; i < capabilitiesData.agents.length; i++) {
        if (capabilitiesData.agents[i].id === currentAgentId) {
            return capabilitiesData.agents[i];
        }
    }
    return null;
}

function composerSupportsVision() {
    var agent = getCurrentAgentCapabilities();
    if (!agent) return true;
    if (typeof agent.supports_vision === 'boolean') return agent.supports_vision;
    if (typeof agent.supportsVision === 'boolean') return agent.supportsVision;
    if (typeof agent.model_supports_vision === 'boolean') return agent.model_supports_vision;
    return true;
}

function updateComposerVisionState() {
    var uploadBtn = document.getElementById('mediaUploadBtn');
    if (!uploadBtn) return;
    var enabled = composerSupportsVision();
    uploadBtn.title = enabled ? 'Upload image' : 'Current model does not support image understanding';
    if (!enabled && pendingComposerMedia.length > 0) {
        clearPendingMedia();
    }
    renderPendingMediaTray();
    updateSendButton();
}

// ---- Status ----
function updateStatus(connected, text) {
    var status = document.getElementById('status');
    status.className = 'status ' + connected;
    status.textContent = text;
}

// ---- Keyboard ----
function handleKeyPress(event) {
    if (event.key === 'Escape' && isAgentRunning) {
        stopAgent();
        event.preventDefault();
        return;
    }
    if (event.key === 'Enter') {
        var dropdown = document.getElementById('commandDropdown');
        if (dropdown && dropdown.style.display !== 'none' && selectedCommandIndex >= 0) {
            selectCommand(selectedCommandIndex);
            event.preventDefault();
            return;
        }
        sendMessage();
    }
}

// ---- Ping ----
setInterval(function() {
    if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'ping' }));
    }
}, 30000);
