// ===== Chat UI Functions =====

// State
var currentStreamMessage = null;
var isFinalResponse = false;
var activeToolCalls = new Map();
var toolCallCounter = 0;
var currentThinkingCard = null;
var thinkingCounter = 0;
var streamRawContent = '';
var CHAT_SCROLL_SHOW_TOP_THRESHOLD = 240;
var CHAT_SCROLL_SHOW_BOTTOM_GAP = 160;

function getChatMessagesContainer() {
    return document.getElementById('chatMessages');
}

function getChatScrollBottomButton() {
    return document.getElementById('chatScrollBottomBtn');
}

function isChatNearBottom() {
    var container = getChatMessagesContainer();
    if (!container) return true;
    return container.scrollHeight - container.clientHeight - container.scrollTop <= 24;
}

function syncChatScrollAfterAppend(shouldStickToBottom) {
    if (shouldStickToBottom) {
        scrollChatToBottom();
        return;
    }
    updateChatScrollButton();
}

function updateChatScrollButton() {
    var container = getChatMessagesContainer();
    var button = getChatScrollBottomButton();
    if (!container || !button) return;

    var maxScrollTop = container.scrollHeight - container.clientHeight;
    var distanceFromBottom = container.scrollHeight - container.clientHeight - container.scrollTop;
    var shouldShow = maxScrollTop > CHAT_SCROLL_SHOW_TOP_THRESHOLD
        && distanceFromBottom > CHAT_SCROLL_SHOW_BOTTOM_GAP;

    button.classList.toggle('visible', shouldShow);
}

function scrollChatToBottom(behavior) {
    var container = getChatMessagesContainer();
    if (!container) return;

    if (behavior === 'smooth' && typeof container.scrollTo === 'function') {
        container.scrollTo({ top: container.scrollHeight, behavior: 'smooth' });
    } else {
        container.scrollTop = container.scrollHeight;
    }

    updateChatScrollButton();
}

function initChatScrollControls() {
    var container = getChatMessagesContainer();
    if (!container || container.dataset.scrollControlsReady === 'true') return;

    container.addEventListener('scroll', updateChatScrollButton);
    container.dataset.scrollControlsReady = 'true';
    updateChatScrollButton();
}

// ---- Streaming ----
function createStreamMessage() {
    var container = getChatMessagesContainer();
    var shouldStickToBottom = isChatNearBottom();
    var messageDiv = document.createElement('div');
    messageDiv.className = 'message assistant';
    messageDiv.id = 'stream-message-' + Date.now();
    messageDiv.style.display = 'none';
    messageDiv.innerHTML = '<div class="message-content">'
        + '<span class="stream-content"></span>'
        + '<div class="message-time">' + new Date().toLocaleTimeString() + '</div>'
        + '</div>';

    container.appendChild(messageDiv);
    syncChatScrollAfterAppend(shouldStickToBottom);
    return { element: messageDiv, contentSpan: messageDiv.querySelector('.stream-content') };
}

function appendStreamContent(text) {
    if (!currentStreamMessage) currentStreamMessage = createStreamMessage();
    var shouldStickToBottom = isChatNearBottom();
    streamRawContent += text;
    currentStreamMessage.contentSpan.innerHTML = renderMarkdown(streamRawContent);
    currentStreamMessage.element.style.display = 'flex';
    syncChatScrollAfterAppend(shouldStickToBottom);
}

function flushThinking() {
    // No longer needed, now displayed in real-time
}

function showFinalResponse() {
    if (currentStreamMessage) {
        currentStreamMessage.element.style.display = 'flex';
        scrollChatToBottom();
    }
}

function finalizeStreamMessage() {
    if (currentStreamMessage && !streamRawContent) {
        currentStreamMessage.element.remove();
        currentStreamMessage = null;
    }
    streamRawContent = '';
    document.getElementById('typingIndicator').classList.remove('active');
}

// ---- Thinking Cards ----
function appendThinkingContent(text) {
    if (!currentThinkingCard) currentThinkingCard = createThinkingCard();
    var shouldStickToBottom = isChatNearBottom();
    currentThinkingCard.contentEl.textContent += text;
    currentThinkingCard.element.style.display = 'flex';
    syncChatScrollAfterAppend(shouldStickToBottom);
}

function createThinkingCard() {
    var container = getChatMessagesContainer();
    var shouldStickToBottom = isChatNearBottom();
    var callId = 'thinking-' + (thinkingCounter++) + '-' + Date.now();

    var cardDiv = document.createElement('div');
    cardDiv.className = 'message log-entry';
    cardDiv.style.display = 'none';
    cardDiv.innerHTML = '<div class="thinking-card" id="' + callId + '" onclick="toggleThinkingCard(\'' + callId + '\')">'
        + '<div class="thinking-header">'
        + '<span class="thinking-icon active">\uD83D\uDCAD</span>'
        + '<span class="thinking-label">Thinking</span>'
        + '<span class="thinking-phase">analysis</span>'
        + '<span class="thinking-time">' + new Date().toLocaleTimeString() + '</span>'
        + '<span class="thinking-expand">\u25BC</span>'
        + '</div>'
        + '<div class="thinking-body">'
        + '<div class="thinking-content"></div>'
        + '</div>'
        + '</div>';

    container.appendChild(cardDiv);
    syncChatScrollAfterAppend(shouldStickToBottom);
    return {
        element: cardDiv,
        cardId: callId,
        contentEl: cardDiv.querySelector('.thinking-content')
    };
}

function toggleThinkingCard(callId) {
    var card = document.getElementById(callId);
    if (!card) return;
    card.classList.toggle('expanded');
}

function finalizeThinking() {
    if (currentThinkingCard) {
        var icon = currentThinkingCard.element.querySelector('.thinking-icon');
        if (icon) icon.classList.remove('active');
        var card = document.getElementById(currentThinkingCard.cardId);
        if (card) card.classList.remove('expanded');
        currentThinkingCard = null;
    }
}

// ---- Tool Call Cards ----
function formatToolParamsSummary(toolName, paramsJson) {
    if (!paramsJson) return '';
    try {
        var params = typeof paramsJson === 'string' ? JSON.parse(paramsJson) : paramsJson;

        switch (toolName) {
            case 'Read':
                return params.file_path ? ' ' + truncateText(params.file_path, 60) : '';
            case 'Bash':
                return params.command ? ' ' + truncateText(params.command, 60) : '';
            case 'Glob':
                return params.pattern ? ' ' + truncateText(params.pattern, 40) : '';
            case 'Grep':
                var grepSummary = params.pattern ? '"' + truncateText(params.pattern, 30) + '"' : '';
                if (params.path) grepSummary += ' in ' + truncateText(params.path, 20);
                return grepSummary ? ' ' + grepSummary : '';
            case 'Edit':
                return params.file_path ? ' ' + truncateText(params.file_path, 60) : '';
            case 'Write':
                return params.file_path ? ' ' + truncateText(params.file_path, 60) : '';
            case 'LSP':
                return params.filePath ? ' ' + truncateText(params.filePath, 40) : '';
            default:
                // Show first important parameter
                var keys = Object.keys(params);
                if (keys.length > 0) {
                    var firstKey = keys[0];
                    var val = String(params[firstKey]);
                    return ' ' + truncateText(val, 50);
                }
                return '';
        }
    } catch (e) {
        return '';
    }
}

function truncateText(text, maxLen) {
    if (!text) return '';
    text = String(text);
    if (text.length <= maxLen) return text;
    return text.substring(0, maxLen - 3) + '...';
}

function addToolStartMessage(toolName, params) {
    finalizeStreamMessage();
    currentStreamMessage = null;

    var container = getChatMessagesContainer();
    var shouldStickToBottom = isChatNearBottom();
    var callId = 'tool-' + (toolCallCounter++) + '-' + Date.now();
    var startTime = Date.now();

    var paramsSummary = formatToolParamsSummary(toolName, params);

    var cardDiv = document.createElement('div');
    cardDiv.className = 'message log-entry';
    cardDiv.innerHTML = '<div class="tool-call-card running" id="' + callId + '" onclick="toggleToolCall(\'' + callId + '\')">'
        + '<div class="tool-call-header">'
        + '<span class="tool-status-icon running">\u27F3</span>'
        + '<span class="tool-name">' + escapeHtml(toolName) + '</span>'
        + '<span class="tool-params-summary">' + escapeHtml(paramsSummary) + '</span>'
        + '<span class="tool-time">' + new Date().toLocaleTimeString() + '</span>'
        + '<span class="tool-duration">\u2014</span>'
        + '<span class="tool-expand-indicator">\u25BC</span>'
        + '</div>'
        + '<div class="tool-call-body">'
        + (params
            ? '<div class="tool-call-params"><div class="tool-call-params-title">Parameters</div>'
            + '<pre class="tool-call-params-content">' + escapeHtml(formatJsonParams(params)) + '</pre></div>'
            : '')
        + '<div class="tool-call-result">'
        + '<div class="tool-call-result-title">Result</div>'
        + '<pre class="tool-call-result-content" id="' + callId + '-result">Executing...</pre>'
        + '</div>'
        + '<div class="tool-call-timestamp" id="' + callId + '-ts">Started at ' + new Date().toLocaleTimeString() + '</div>'
        + '</div></div>';

    container.appendChild(cardDiv);
    syncChatScrollAfterAppend(shouldStickToBottom);

    activeToolCalls.set(callId, {
        element: cardDiv.querySelector('.tool-call-card'),
        startTime: startTime,
        toolName: toolName,
        params: params
    });

    addLog('info', 'Tool started: ' + toolName + ' [' + callId + ']');
    return callId;
}

function addToolEndMessage(toolName, result, callId, isError) {
    if (!callId) {
        for (var entry of activeToolCalls) {
            if (entry[1].toolName === toolName) {
                callId = entry[0];
                break;
            }
        }
    }

    var toolData = activeToolCalls.get(callId);
    if (!toolData) {
        addToolHistoryMessage('Tool: ' + toolName + '\nResult: ' + (result || 'No result'), null);
        addLog('warn', 'Tool end without matching start: ' + toolName);
        return;
    }

    var card = toolData.element;
    var duration = Date.now() - toolData.startTime;
    var durationStr = duration < 1000 ? duration + 'ms' : (duration / 1000).toFixed(2) + 's';

    card.classList.remove('running');
    card.classList.add(isError ? 'error' : 'completed');

    var statusIcon = card.querySelector('.tool-status-icon');
    statusIcon.classList.remove('running');
    statusIcon.classList.add(isError ? 'error' : 'completed');
    statusIcon.textContent = isError ? '\u2717' : '\u2713';

    var durationEl = card.querySelector('.tool-duration');
    if (durationEl) durationEl.textContent = durationStr;

    var resultEl = card.querySelector('.tool-call-result-content');
    if (resultEl) {
        var displayResult = result
            ? (result.length > 2000 ? result.substring(0, 2000) + '\n...(truncated)' : result)
            : 'No result';
        resultEl.textContent = displayResult;
    }

    var tsEl = card.querySelector('.tool-call-timestamp');
    if (tsEl) tsEl.textContent = 'Completed at ' + new Date().toLocaleTimeString() + ' (' + durationStr + ')';

    activeToolCalls.delete(callId);
    addLog('info', 'Tool completed: ' + toolName + ' (' + durationStr + ')');
}

function toggleToolCall(callId) {
    var card = document.getElementById(callId);
    if (!card) return;
    card.classList.toggle('expanded');
}

function formatJsonParams(params) {
    if (!params) return 'None';
    try {
        if (typeof params === 'string') {
            return JSON.stringify(JSON.parse(params), null, 2);
        }
        return JSON.stringify(params, null, 2);
    } catch(e) {
        return String(params);
    }
}

function addToolHistoryMessage(content, timestamp) {
    var container = getChatMessagesContainer();
    var shouldStickToBottom = isChatNearBottom();
    var callId = 'tool-history-' + (toolCallCounter++) + '-' + Date.now();
    var time = timestamp ? new Date(timestamp * 1000).toLocaleTimeString() : new Date().toLocaleTimeString();

    var toolName = 'Tool';
    var result = content;
    var params = null;

    var toolMatch = content.match(/^Tool:\s*(.+?)(?:\n|$)/i);
    if (toolMatch) {
        toolName = toolMatch[1].trim();
        var resultMatch = content.match(/(?:Result|Output):\s*(.+)$/i);
        if (resultMatch) {
            result = resultMatch[1].trim();
        } else {
            result = content.replace(/^Tool:\s*.+?\n/i, '').trim();
        }
    }

    var displayResult = result.length > 2000 ? result.substring(0, 2000) + '\n...(truncated)' : result;

    var messageDiv = document.createElement('div');
    messageDiv.className = 'message log-entry';
    messageDiv.innerHTML = '<div class="tool-call-card completed" id="' + callId + '" onclick="toggleToolCall(\'' + callId + '\')">'
        + '<div class="tool-call-header">'
        + '<span class="tool-status-icon completed">\u2713</span>'
        + '<span class="tool-name">' + escapeHtml(toolName) + '</span>'
        + '<span class="tool-time">' + time + '</span>'
        + '<span class="tool-duration">history</span>'
        + '<span class="tool-expand-indicator">\u25BC</span>'
        + '</div>'
        + '<div class="tool-call-body">'
        + (params ? '<div class="tool-call-params"><div class="tool-call-params-title">Parameters</div>'
            + '<pre class="tool-call-params-content">' + escapeHtml(formatJsonParams(params)) + '</pre></div>' : '')
        + '<div class="tool-call-result">'
        + '<div class="tool-call-result-title">Result</div>'
        + '<pre class="tool-call-result-content">' + escapeHtml(displayResult) + '</pre>'
        + '</div>'
        + '<div class="tool-call-timestamp">' + time + '</div>'
        + '</div></div>';

    container.appendChild(messageDiv);
    syncChatScrollAfterAppend(shouldStickToBottom);
}

// ---- Messages ----
function normalizeMessageContent(content, media) {
    var text = content || '';
    if (!Array.isArray(media) || media.length === 0) {
        return text;
    }
    return text.replace(/\n?\[(image|audio|video|file|media)(?::[^\]]+)?\]\s*/gi, '').trim();
}

function renderMessageMedia(media) {
    if (!Array.isArray(media) || media.length === 0) {
        return '';
    }
    var items = media.map(function(item) {
        if (!item || !item.url) {
            return '';
        }
        var type = (item.type || 'file').toLowerCase();
        var name = escapeHtml(item.name || type);
        var url = escapeHtml(item.url);
        if (type === 'image') {
            return '<div class="message-media-item image"><img src="' + url + '" alt="' + name + '" loading="lazy"></div>';
        }
        if (type === 'audio') {
            return '<div class="message-media-item audio"><audio controls preload="none" src="' + url + '"></audio><a href="' + url + '" target="_blank" rel="noopener noreferrer">' + name + '</a></div>';
        }
        if (type === 'video') {
            return '<div class="message-media-item video"><video controls preload="metadata" src="' + url + '"></video><a href="' + url + '" target="_blank" rel="noopener noreferrer">' + name + '</a></div>';
        }
        return '<div class="message-media-item file"><a href="' + url + '" target="_blank" rel="noopener noreferrer">' + name + '</a></div>';
    }).filter(Boolean).join('');
    if (!items) {
        return '';
    }
    return '<div class="message-media">' + items + '</div>';
}

function renderPendingComposerMedia(media) {
    if (!Array.isArray(media) || media.length === 0) {
        return '';
    }
    return media.map(function(item, index) {
        if (!item) {
            return '';
        }
        var status = String(item.status || 'uploaded').toLowerCase();
        var name = escapeHtml(item.name || 'image');
        var previewUrl = item.preview_url || item.url || '';
        var preview = previewUrl
            ? '<img src="' + escapeHtml(previewUrl) + '" alt="' + name + '" loading="lazy">'
            : '<div class="pending-media-placeholder">IMG</div>';
        var statusText = status === 'uploading' ? 'Uploading' : (status === 'failed' ? 'Failed' : 'Ready');
        return '<div class="pending-media-item ' + escapeHtml(status) + '">'
            + '<div class="pending-media-preview">' + preview + '</div>'
            + '<div class="pending-media-meta">'
            + '<div class="pending-media-name">' + name + '</div>'
            + '<div class="pending-media-status">' + escapeHtml(statusText) + '</div>'
            + '</div>'
            + '<button type="button" class="pending-media-remove" onclick="removePendingMedia(' + index + ')" aria-label="Remove image">×</button>'
            + '</div>';
    }).filter(Boolean).join('');
}

function addMessage(role, content, timestamp, media) {
    var container = getChatMessagesContainer();
    var shouldStickToBottom = isChatNearBottom();
    var messageDiv = document.createElement('div');
    messageDiv.className = 'message ' + role;
    var time = timestamp ? new Date(timestamp * 1000).toLocaleTimeString() : new Date().toLocaleTimeString();
    var normalizedContent = normalizeMessageContent(content, media);
    var renderedContent = role === 'assistant' ? renderMarkdown(normalizedContent) : escapeHtml(normalizedContent);
    var mediaHtml = renderMessageMedia(media);
    messageDiv.innerHTML = '<div class="message-content">'
        + renderedContent
        + mediaHtml
        + '<div class="message-time">' + time + '</div></div>';
    container.appendChild(messageDiv);
    syncChatScrollAfterAppend(shouldStickToBottom);
}

function addSystemMessage(content) {
    var container = getChatMessagesContainer();
    var shouldStickToBottom = isChatNearBottom();
    var messageDiv = document.createElement('div');
    messageDiv.className = 'message system';
    messageDiv.style.justifyContent = 'center';
    messageDiv.innerHTML = '<div class="message-content">' + escapeHtml(content) + '</div>';
    container.appendChild(messageDiv);
    syncChatScrollAfterAppend(shouldStickToBottom);
}

function addIterationMessage(iteration) {
    finalizeStreamMessage();
    currentStreamMessage = null;
    addLogMessage('iteration', '\uD83D\uDD04 Iteration #' + iteration, 'Processing next step...');
}

function addLogMessage(type, title, content) {
    var container = getChatMessagesContainer();
    var shouldStickToBottom = isChatNearBottom();
    var messageDiv = document.createElement('div');
    messageDiv.className = 'message log-entry ' + type;
    messageDiv.innerHTML = '<div class="log-message-content">'
        + '<div class="log-title">' + escapeHtml(title) + '</div>'
        + '<div class="log-body">' + escapeHtml(content) + '</div>'
        + '<div class="message-time">' + new Date().toLocaleTimeString() + '</div></div>';
    container.appendChild(messageDiv);
    syncChatScrollAfterAppend(shouldStickToBottom);
}

// ---- Command Result Display ----
function addCommandResult(content) {
    var container = getChatMessagesContainer();
    var shouldStickToBottom = isChatNearBottom();
    var messageDiv = document.createElement('div');
    messageDiv.className = 'message assistant';

    var htmlContent = escapeHtml(content);

    // Headings
    htmlContent = htmlContent.replace(/^### (.+)$/gm, '<h4 style="color:#F1F5F9;margin:12px 0 8px;font-size:16px;">$1</h4>');
    htmlContent = htmlContent.replace(/^## (.+)$/gm, '<h3 style="color:#F1F5F9;margin:16px 0 10px;font-size:18px;">$1</h3>');
    htmlContent = htmlContent.replace(/^# (.+)$/gm, '<h2 style="color:#3B82F6;margin:16px 0 12px;font-size:20px;">$1</h2>');

    // Bold and italic
    htmlContent = htmlContent.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
    htmlContent = htmlContent.replace(/\*(.+?)\*/g, '<em>$1</em>');

    // Inline code
    htmlContent = htmlContent.replace(/`([^`]+)`/g, '<code style="background:rgba(30,58,95,0.3);padding:2px 6px;border-radius:4px;font-family:monospace;font-size:13px;">$1</code>');

    // Code blocks
    htmlContent = htmlContent.replace(/```(\w*)\n([\s\S]*?)```/g, '<pre style="background:rgba(10,15,28,0.6);padding:12px;border-radius:8px;overflow-x:auto;margin:12px 0;font-family:monospace;font-size:13px;"><code>$2</code></pre>');

    // Lists
    htmlContent = htmlContent.replace(/^- (.+)$/gm, '<li style="margin-left:16px;">$1</li>');
    htmlContent = htmlContent.replace(/^  - (.+)$/gm, '<li style="margin-left:32px;list-style-type:circle;">$1</li>');

    // Tables: collect contiguous `| ... |` line groups, identify header/separator,
    // and emit a single contiguous <table> with <thead>/<tbody> so rows don't get
    // split apart by later paragraph transforms.
    htmlContent = htmlContent.replace(/(^|\n)((?:\|[^\n]*\|\s*\n?)+)/g, function(block, prefix, tableText) {
        var lines = tableText.split('\n').filter(function(l) { return l.trim() !== ''; });
        if (lines.length === 0) return block;
        var rows = [];
        var seenSep = false;
        for (var i = 0; i < lines.length; i++) {
            var inner = lines[i].trim().replace(/^\|/, '').replace(/\|$/, '');
            var cells = inner.split('|');
            var isSep = cells.every(function(c) { return /^\s*:?-{2,}:?\s*$/.test(c); });
            if (isSep) { seenSep = true; continue; }
            var header = rows.length === 0 && !seenSep && i === 0;
            rows.push({ cells: cells, header: header });
        }
        if (rows.length === 0) return block;
        var thead = '', tbody = '';
        rows.forEach(function(r) {
            var tag = r.header ? 'th' : 'td';
            var cellStyle = 'padding:6px 12px;text-align:left;' + (r.header ? 'color:#94A3B8;font-weight:600;' : '');
            var cellHtml = r.cells.map(function(c) { return '<' + tag + ' style="' + cellStyle + '">' + c.trim() + '</' + tag + '>'; }).join('');
            var trStyle = r.header ? 'border-bottom:1px solid rgba(59,130,246,0.3);' : 'border-bottom:1px solid rgba(255,255,255,0.06);';
            var tr = '<tr style="' + trStyle + '">' + cellHtml + '</tr>';
            if (r.header) thead += tr; else tbody += tr;
        });
        var tableHtml = '<table style="width:100%;border-collapse:collapse;margin:8px 0;font-size:13px;">'
            + (thead ? '<thead>' + thead + '</thead>' : '')
            + '<tbody>' + tbody + '</tbody></table>';
        return prefix + tableHtml;
    });

    // HR
    htmlContent = htmlContent.replace(/^---$/gm, '<hr style="border:none;border-top:1px solid rgba(59,130,246,0.2);margin:16px 0;">');

    // Paragraphs
    htmlContent = htmlContent.replace(/\n\n/g, '</p><p style="margin:10px 0;">');
    htmlContent = '<p style="margin:10px 0;">' + htmlContent + '</p>';

    messageDiv.innerHTML = '<div class="message-content">'
        + '<div style="font-size:13px;color:#3B82F6;margin-bottom:8px;font-weight:600;">\u26A1 Command Result</div>'
        + '<div style="line-height:1.7;">' + htmlContent + '</div>'
        + '<div class="message-time">' + new Date().toLocaleTimeString() + '</div></div>';

    container.appendChild(messageDiv);
    syncChatScrollAfterAppend(shouldStickToBottom);
}

// ---- Utility ----
function escapeHtml(text) {
    var div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// ---- Markdown Rendering ----
function renderMarkdown(text) {
    if (!text) return '';
    var escaped = escapeHtml(text);

    // Code blocks FIRST (prevent markdown inside)
    var codeBlocks = [];
    escaped = escaped.replace(/```(\w*)\n([\s\S]*?)```/g, function(match, lang, code) {
        var idx = codeBlocks.length;
        codeBlocks.push('<pre><code class="language-' + (lang || '') + '">' + code + '</code></pre>');
        return '\x00CODEBLOCK' + idx + '\x00';
    });

    // Inline code
    var inlineCodes = [];
    escaped = escaped.replace(/`([^`]+)`/g, function(match, code) {
        var idx = inlineCodes.length;
        inlineCodes.push('<code>' + code + '</code>');
        return '\x00INLINECODE' + idx + '\x00';
    });

    // Headings
    escaped = escaped.replace(/^#### (.+)$/gm, '<h4>$1</h4>');
    escaped = escaped.replace(/^### (.+)$/gm, '<h3>$1</h3>');
    escaped = escaped.replace(/^## (.+)$/gm, '<h2>$1</h2>');
    escaped = escaped.replace(/^# (.+)$/gm, '<h2>$1</h2>');

    // Bold + Italic (must come before bold/italic alone)
    escaped = escaped.replace(/\*\*\*(.+?)\*\*\*/g, '<strong><em>$1</em></strong>');
    escaped = escaped.replace(/___(.+?)___/g, '<strong><em>$1</em></strong>');

    // Bold
    escaped = escaped.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
    escaped = escaped.replace(/__(.+?)__/g, '<strong>$1</strong>');

    // Italic
    escaped = escaped.replace(/\*(.+?)\*/g, '<em>$1</em>');
    escaped = escaped.replace(/_([^_\s][^_]*)_/g, '<em>$1</em>');

    // Images (before links)
    escaped = escaped.replace(/!\[([^\]]*)\]\(([^)]+)\)/g, '<img src="$2" alt="$1">');

    // Links
    escaped = escaped.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener noreferrer">$1</a>');

    // Blockquotes
    escaped = escaped.replace(/^&gt; (.+)$/gm, '<blockquote>$1</blockquote>');

    // Horizontal rules
    escaped = escaped.replace(/^(?:---|\*\*\*|\_\_\_)$/gm, '<hr>');

    // Tables
    escaped = escaped.replace(/^(\|.+\|)$/gm, function(match) {
        var cells = match.split('|').filter(function(c) { return c.trim(); });
        var isSeparator = cells.every(function(c) { return /^[\s\-:]+$/.test(c.trim()); });
        if (isSeparator) return '%%TABLESEP%%';
        return '<tr>' + cells.map(function(c) { return '<td>' + c.trim() + '</td>'; }).join('') + '</tr>';
    });

    // Wrap table rows
    escaped = escaped.replace(/((?:<tr>.*<\/tr>\n?)+(?:%%TABLESEP%%\n)?)((?:<tr>.*<\/tr>\n?)+)/g, function(match, headerRows, bodyRows) {
        var headerHtml = headerRows.replace(/%%TABLESEP%%\n?/, '').replace(/<td>/g, '<th>').replace(/<\/td>/g, '</th>');
        return '<table><thead>' + headerHtml + '</thead><tbody>' + bodyRows + '</tbody></table>';
    });
    // Handle tables without header separator
    escaped = escaped.replace(/((?:<tr>.*<\/tr>\n?){2,})/g, function(match) {
        if (match.indexOf('<table>') !== -1) return match;
        return '<table><tbody>' + match + '</tbody></table>';
    });

    // Unordered lists
    escaped = escaped.replace(/^[\-\*] (.+)$/gm, '<li>$1</li>');
    escaped = escaped.replace(/((?:<li>.*<\/li>\n?)+)/g, function(match) {
        if (match.indexOf('<li>') !== -1 && match.indexOf('<table>') === -1) {
            return '<ul>' + match + '</ul>';
        }
        return match;
    });

    // Ordered lists
    escaped = escaped.replace(/^\d+\. (.+)$/gm, '<oli>$1</oli>');
    escaped = escaped.replace(/((?:<oli>.*<\/oli>\n?)+)/g, function(match) {
        return '<ol>' + match.replace(/<oli>/g, '<li>').replace(/<\/oli>/g, '</li>') + '</ol>';
    });

    // Line breaks: double newline → paragraph break, single → <br>
    escaped = escaped.replace(/\n\n/g, '</p><p>');
    escaped = escaped.replace(/\n/g, '<br>');

    // Restore code blocks (use null character placeholders)
    for (var i = 0; i < codeBlocks.length; i++) {
        escaped = escaped.replace(new RegExp('\x00CODEBLOCK' + i + '\x00', 'g'), codeBlocks[i]);
    }

    // Restore inline codes
    for (var j = 0; j < inlineCodes.length; j++) {
        escaped = escaped.replace(new RegExp('\x00INLINECODE' + j + '\x00', 'g'), inlineCodes[j]);
    }

    return escaped;
}
