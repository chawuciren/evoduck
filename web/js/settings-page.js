// ===== Settings Page =====

if (typeof escapeJs !== 'function') {
    function escapeJs(value) {
        return String(value || '').replace(/\\/g, '\\\\').replace(/'/g, "\\'");
    }
}

var settingsConfigPath = '';
var settingsDraft = {};
var settingsLastSavedSnapshot = {};
var settingsSchema = [];
var settingsIssues = [];

function fetchSettings() {
    if (!sendWSRequest('get_settings_full')) renderSettings();
}

function setSettingsStatus(message, kind) {
    var el = document.getElementById('settingsStatus');
    if (!el) return;
    el.textContent = message || '—';
    el.className = 'settings-status' + (kind ? ' ' + kind : '');
}

function setSettingsMeta(path) {
    var el = document.getElementById('settingsMeta');
    if (!el) return;
    el.textContent = 'Config path: ' + (path || '—');
}

function cloneSettingsValue(value) {
    return JSON.parse(JSON.stringify(value === undefined ? null : value));
}

function settingsStringify(value) {
    return JSON.stringify(value === undefined ? null : value, null, 2);
}

function isSettingsDirty() {
    return settingsStringify(settingsDraft) !== settingsStringify(settingsLastSavedSnapshot);
}

function formatSettingsIssues(issues) {
    if (!issues || issues.length === 0) return '';
    return issues.map(function(issue) {
        var field = issue.field ? '<strong>' + escapeHtml(issue.field) + '</strong>: ' : '';
        return '<li>' + field + escapeHtml(issue.message || 'Invalid value') + '</li>';
    }).join('');
}

function formatSettingsResultStatus(prefix, result) {
    result = result || {};
    var parts = [prefix];
    if (result.applied_now && result.applied_now.length) {
        parts.push('hot-applied: ' + result.applied_now.join(', '));
    }
    if (result.restart_required && result.restart_required.length) {
        parts.push('restart required: ' + result.restart_required.join(', '));
    }
    if (result.rolled_back) {
        parts.push('rollback completed');
    }
    return parts.join(' · ');
}

function updateSettingsDirtyState() {
    var dirty = isSettingsDirty();
    var hint = document.getElementById('settingsDirtyHint');
    if (hint) {
        hint.textContent = dirty ? 'Unsaved changes' : 'No local changes';
        hint.className = 'settings-dirty-hint' + (dirty ? ' dirty' : ' clean');
    }
}

function getSettingsByPath(root, path) {
    if (!path) return root;
    var parts = path.split('.');
    var current = root;
    for (var i = 0; i < parts.length; i++) {
        if (current == null) return undefined;
        current = current[parts[i]];
    }
    return current;
}

function setSettingsByPath(root, path, value) {
    var parts = path.split('.');
    var current = root;
    for (var i = 0; i < parts.length - 1; i++) {
        var key = parts[i];
        if (!current[key] || typeof current[key] !== 'object') current[key] = {};
        current = current[key];
    }
    current[parts[parts.length - 1]] = value;
}

function getSettingsIssueMap() {
    var map = {};
    (settingsIssues || []).forEach(function(issue) {
        if (!issue || !issue.field) return;
        if (!map[issue.field]) map[issue.field] = [];
        map[issue.field].push(issue.message || 'Invalid value');
    });
    return map;
}

function updateSettingsField(path, rawValue, fieldType) {
    var nextValue = rawValue;
    if (fieldType === 'number') {
        nextValue = rawValue === '' ? 0 : Number(rawValue);
    } else if (fieldType === 'boolean') {
        nextValue = !!rawValue;
    }
    setSettingsByPath(settingsDraft, path, nextValue);
    settingsIssues = [];
    updateSettingsDirtyState();
    renderSettingsIssues();
    setSettingsStatus('Editing local draft', 'pending');
}

function updateSettingsJSONField(path, rawValue) {
    try {
        var parsed = rawValue.trim() ? JSON.parse(rawValue) : {};
        setSettingsByPath(settingsDraft, path, parsed);
        settingsIssues = [];
        updateSettingsDirtyState();
        renderSettingsIssues();
        setSettingsStatus('Editing local draft', 'pending');
    } catch (error) {
        setSettingsStatus('Invalid JSON for ' + path + ': ' + error.message, 'error');
    }
}

function touchSettingsDraft() {
    settingsIssues = [];
    updateSettingsDirtyState();
    renderSettingsIssues();
    setSettingsStatus('Editing local draft', 'pending');
}

function getSettingsPathSegments(path) {
    return path ? path.split('.') : [];
}

function getSettingsParentByPath(root, path) {
    var parts = getSettingsPathSegments(path);
    if (!parts.length) return null;
    var current = root;
    for (var i = 0; i < parts.length - 1; i++) {
        if (!current || typeof current !== 'object') return null;
        current = current[parts[i]];
    }
    return current;
}

function removeSettingsByPath(root, path) {
    var parts = getSettingsPathSegments(path);
    if (!parts.length) return;
    var parent = getSettingsParentByPath(root, path);
    if (!parent || typeof parent !== 'object') return;
    delete parent[parts[parts.length - 1]];
}

function setSettingsSectionValue(path, value) {
    setSettingsByPath(settingsDraft, path, value);
    touchSettingsDraft();
    renderSettings();
}

function removeSettingsSectionEntry(path, key) {
    var section = getSettingsByPath(settingsDraft, path);
    if (!section || typeof section !== 'object') return;
    delete section[key];
    touchSettingsDraft();
    renderSettings();
}

function addSettingsMapEntry(path, templateFactory) {
    var name = window.prompt('Entry name');
    if (!name) return;
    name = name.trim();
    if (!name) return;
    var section = getSettingsByPath(settingsDraft, path);
    if (!section || typeof section !== 'object' || Array.isArray(section)) {
        section = {};
        setSettingsByPath(settingsDraft, path, section);
    }
    if (Object.prototype.hasOwnProperty.call(section, name)) {
        setSettingsStatus('Entry already exists: ' + name, 'error');
        return;
    }
    section[name] = templateFactory ? templateFactory(name) : {};
    touchSettingsDraft();
    renderSettings();
}

function updateSettingsMapString(path, key, field, value) {
    var section = getSettingsByPath(settingsDraft, path);
    if (!section || !section[key]) return;
    section[key][field] = value;
    touchSettingsDraft();
}

function updateSettingsMapNumber(path, key, field, value) {
    var section = getSettingsByPath(settingsDraft, path);
    if (!section || !section[key]) return;
    section[key][field] = value === '' ? 0 : Number(value);
    touchSettingsDraft();
}

function updateSettingsMapBoolean(path, key, field, value) {
    var section = getSettingsByPath(settingsDraft, path);
    if (!section || !section[key]) return;
    section[key][field] = !!value;
    touchSettingsDraft();
}

function updateSettingsMapStringArray(path, key, field, rawValue) {
    var section = getSettingsByPath(settingsDraft, path);
    if (!section || !section[key]) return;
    section[key][field] = rawValue.split(',').map(function(item) {
        return item.trim();
    }).filter(function(item) {
        return item.length > 0;
    });
    touchSettingsDraft();
}

function getSettingsAgentNames() {
    return Object.keys((settingsDraft && settingsDraft.agents) || {}).sort();
}

function getSettingsProviderNames() {
    return Object.keys((settingsDraft && settingsDraft.llm && settingsDraft.llm.providers) || {}).sort();
}

function renderSettingsSelect(options, value, onChange, placeholder) {
    var normalizedValue = value === undefined || value === null ? '' : String(value);
    var normalizedOptions = (options || []).map(function(option) {
        return String(option);
    });
    if (normalizedValue && normalizedOptions.indexOf(normalizedValue) === -1) {
        normalizedOptions = normalizedOptions.concat([normalizedValue]);
    }
    var optionHtml = '';
    if (placeholder) {
        optionHtml += '<option value="">' + escapeHtml(placeholder) + '</option>';
    }
    optionHtml += normalizedOptions.map(function(option) {
        return '<option value="' + escapeHtml(option) + '"' + (option === normalizedValue ? ' selected' : '') + '>' + escapeHtml(option) + '</option>';
    }).join('');
    return '<select class="settings-input settings-input-wide" onchange="' + onChange + '">' + optionHtml + '</select>';
}

function renderSettingsSecretInput(value, onChange) {
    return '<input type="password" class="settings-input settings-input-wide settings-secret-input" value="' + escapeHtml(value === undefined || value === null ? '' : String(value)) + '" oninput="' + onChange + '" autocomplete="off" spellcheck="false">';
}

function updateSettingsNestedMapString(path, key, nestedKey, field, value) {
    var section = getSettingsByPath(settingsDraft, path);
    if (!section || !section[key]) return;
    if (!section[key][nestedKey] || typeof section[key][nestedKey] !== 'object') {
        section[key][nestedKey] = {};
    }
    section[key][nestedKey][field] = value;
    touchSettingsDraft();
}

function addSettingsNestedMapEntry(path, key, nestedKey, templateFactory) {
    var name = window.prompt('Entry name');
    if (!name) return;
    name = name.trim();
    if (!name) return;
    var section = getSettingsByPath(settingsDraft, path);
    if (!section || !section[key]) return;
    if (!section[key][nestedKey] || typeof section[key][nestedKey] !== 'object') {
        section[key][nestedKey] = {};
    }
    var target = section[key][nestedKey];
    if (Object.prototype.hasOwnProperty.call(target, name)) {
        setSettingsStatus('Entry already exists: ' + name, 'error');
        return;
    }
    target[name] = templateFactory ? templateFactory(name) : '';
    touchSettingsDraft();
    renderSettings();
}

function removeSettingsNestedMapEntry(path, key, nestedKey, entryKey) {
    var section = getSettingsByPath(settingsDraft, path);
    if (!section || !section[key] || !section[key][nestedKey]) return;
    delete section[key][nestedKey][entryKey];
    touchSettingsDraft();
    renderSettings();
}

function renderSettingsKVRows(mapValue, onChangeCall, onRemoveCall, emptyText, valueType) {
    var entries = Object.keys(mapValue || {}).sort();
    if (!entries.length) {
        return '<div class="settings-empty-inline">' + escapeHtml(emptyText || 'No entries') + '</div>';
    }
    return entries.map(function(entryKey) {
        var entryValue = mapValue[entryKey];
        var inputType = valueType === 'number' ? 'number' : 'text';
        return '<div class="settings-kv-row">'
            + '<input type="text" class="settings-input settings-kv-key" value="' + escapeHtml(entryKey) + '" disabled>'
            + '<input type="' + inputType + '" class="settings-input settings-kv-value" value="' + escapeHtml(entryValue === undefined || entryValue === null ? '' : String(entryValue)) + '" oninput="' + onChangeCall.replace(/__KEY__/g, escapeJs(entryKey)) + '(this.value)">'
            + '<button type="button" class="session-btn session-btn-danger settings-inline-btn" onclick="' + onRemoveCall.replace(/__KEY__/g, escapeJs(entryKey)) + '">Remove</button>'
            + '</div>';
    }).join('');
}

function getSettingsFieldIssues(issueMap, fieldPath, extraPaths) {
    var messages = [];
    var seen = {};
    var paths = [fieldPath].concat(extraPaths || []);
    paths.forEach(function(path) {
        Object.keys(issueMap || {}).forEach(function(issuePath) {
            if (issuePath === path || issuePath.indexOf(path + '.') === 0) {
                (issueMap[issuePath] || []).forEach(function(message) {
                    var key = issuePath + '::' + message;
                    if (!seen[key]) {
                        seen[key] = true;
                        messages.push(issuePath === fieldPath ? message : issuePath + ': ' + message);
                    }
                });
            }
        });
    });
    return messages;
}

function renderGenericObjectEditor(field, value) {
    return '<textarea class="settings-json-editor" spellcheck="false" oninput="updateSettingsJSONField(\'' + escapeJs(field.path) + '\', this.value)">' + escapeHtml(settingsStringify(value || {})) + '</textarea>';
}

function renderAgentsEditor(field, value) {
    var agents = value || {};
    var providerNames = getSettingsProviderNames();
    var names = Object.keys(agents).sort();
    var cards = names.map(function(name) {
        var agent = agents[name] || {};
        return '<div class="settings-complex-card">'
            + '<div class="settings-complex-card-header">'
            + '<div><div class="settings-complex-title">' + escapeHtml(name) + '</div><div class="settings-field-path">agents.' + escapeHtml(name) + '</div></div>'
            + '<button type="button" class="session-btn session-btn-danger settings-inline-btn" onclick="removeSettingsSectionEntry(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\')">Remove</button>'
            + '</div>'
            + '<div class="settings-complex-grid">'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Role</span>' + renderSettingsSelect(['admin', 'employee', 'customer'], agent.role || '', 'updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'role\', this.value)', 'Select role') + '</label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Workspace</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(agent.workspace || '') + '" oninput="updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'workspace\', this.value)"></label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Provider</span>' + renderSettingsSelect(providerNames, agent.provider || '', 'updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'provider\', this.value)', 'Use default provider') + '</label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Model</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(agent.model || '') + '" oninput="updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'model\', this.value)"></label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Channels</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml((agent.channels || []).join(', ')) + '" oninput="updateSettingsMapStringArray(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'channels\', this.value)" placeholder="wechat, telegram"></label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Temperature</span><input type="number" step="0.1" class="settings-input settings-input-wide" value="' + escapeHtml(agent.temperature === undefined || agent.temperature === null ? '' : String(agent.temperature)) + '" oninput="updateSettingsMapNumber(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'temperature\', this.value)"></label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Max tokens</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(agent.max_tokens === undefined || agent.max_tokens === null ? '' : String(agent.max_tokens)) + '" oninput="updateSettingsMapNumber(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'max_tokens\', this.value)"></label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Top-p</span><input type="number" step="0.1" class="settings-input settings-input-wide" value="' + escapeHtml(agent.top_p === undefined || agent.top_p === null ? '' : String(agent.top_p)) + '" oninput="updateSettingsMapNumber(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'top_p\', this.value)"></label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Max iterations</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(agent.max_iterations === undefined || agent.max_iterations === null ? '' : String(agent.max_iterations)) + '" oninput="updateSettingsMapNumber(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'max_iterations\', this.value)"></label>'
            + '</div>'
            + '<div class="settings-subsection">'
            + '<div class="settings-subsection-title">User isolation</div>'
            + '<div class="settings-inline-toggle-group">'
            + '<label class="settings-toggle"><input type="checkbox" ' + (agent.user_isolation && agent.user_isolation.enabled ? 'checked' : '') + ' onchange="updateSettingsNestedMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'user_isolation\', \'enabled\', this.checked)"><span>Enabled</span></label>'
            + '<label class="settings-toggle"><input type="checkbox" ' + (agent.user_isolation && agent.user_isolation.auto_create ? 'checked' : '') + ' onchange="updateSettingsNestedMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'user_isolation\', \'auto_create\', this.checked)"><span>Auto create</span></label>'
            + '<label class="settings-toggle"><input type="checkbox" ' + (agent.user_isolation && agent.user_isolation.auto_profile ? 'checked' : '') + ' onchange="updateSettingsNestedMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'user_isolation\', \'auto_profile\', this.checked)"><span>Auto profile</span></label>'
            + '</div>'
            + '</div>'
            + '</div>';
    }).join('');
    return '<div class="settings-complex-editor">'
        + '<div class="settings-complex-toolbar"><button type="button" class="session-btn settings-inline-btn" onclick="addSettingsMapEntry(\'' + escapeJs(field.path) + '\', function(){ return { role: \'customer\', workspace: \'\', provider: \'\', model: \'\', channels: [], user_isolation: { enabled: false, auto_create: false, auto_profile: false }, temperature: 0, max_tokens: 0, top_p: 0, max_iterations: 0 }; })">Add agent</button></div>'
        + (cards || '<div class="settings-empty-inline">No agents configured.</div>')
        + '</div>';
}

function providerGroup(provider) {
    var type = ((provider && provider.type) || '').toLowerCase();
    if (type === 'openai-compatible' || type === 'openai-responses-compatible' || type === 'gemini-compatible' || type === 'anthropic-compatible') {
        return 'Custom';
    }
    if (type === 'ollama' || type === 'lmstudio' || type === 'vllm' || type === 'litellm') {
        return 'Local';
    }
    return 'Vendors';
}

function providerSortWeight(provider) {
    var type = ((provider && provider.type) || '').toLowerCase();
    var weights = {
        'openai-compatible': 10,
        'openai-responses-compatible': 20,
        'gemini-compatible': 30,
        'anthropic-compatible': 40,
        'ollama': 110,
        'lmstudio': 120,
        'vllm': 130,
        'litellm': 140,
        'openai': 210,
        'gemini': 220,
        'anthropic': 230,
        'deepseek': 240,
        'minimax': 250
    };
    return Object.prototype.hasOwnProperty.call(weights, type) ? weights[type] : 1000;
}

function renderProviderCard(field, name, provider) {
    return '<div class="settings-complex-card">'
        + '<div class="settings-complex-card-header">'
        + '<div><div class="settings-complex-title">' + escapeHtml(name) + '</div><div class="settings-field-path">llm.providers.' + escapeHtml(name) + '</div></div>'
        + '<button type="button" class="session-btn session-btn-danger settings-inline-btn" onclick="removeSettingsSectionEntry(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\')">Remove</button>'
        + '</div>'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Type</span>' + renderSettingsSelect(['openai', 'openai-compatible', 'openai-responses-compatible', 'anthropic', 'anthropic-compatible', 'gemini', 'gemini-compatible', 'deepseek', 'minimax', 'ollama', 'lmstudio', 'vllm', 'litellm'], provider.type || '', 'updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'type\', this.value)', 'Select type') + '</label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Base URL</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(provider.base_url || '') + '" oninput="updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'base_url\', this.value)"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Model</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(provider.model || '') + '" oninput="updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'model\', this.value)"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">API Key</span>' + renderSettingsSecretInput(provider.api_key || '', 'updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'api_key\', this.value)') + '</label>'
        + '</div>'
        + '</div>';
}

function renderProvidersEditor(field, value) {
    var providers = value || {};
    var names = Object.keys(providers).sort(function(a, b) {
        var providerA = providers[a] || {};
        var providerB = providers[b] || {};
        var weightDiff = providerSortWeight(providerA) - providerSortWeight(providerB);
        if (weightDiff !== 0) return weightDiff;
        return a.localeCompare(b);
    });
    var groups = { Custom: [], Local: [], Vendors: [] };
    names.forEach(function(name) {
        var provider = providers[name] || {};
        groups[providerGroup(provider)].push(renderProviderCard(field, name, provider));
    });
    var sections = ['Custom', 'Local', 'Vendors'].map(function(groupName) {
        if (!groups[groupName].length) return '';
        return '<div class="settings-subsection">'
            + '<div class="settings-subsection-title">' + escapeHtml(groupName) + '</div>'
            + groups[groupName].join('')
            + '</div>';
    }).join('');
    return '<div class="settings-complex-editor">'
        + '<div class="settings-complex-toolbar"><button type="button" class="session-btn settings-inline-btn" onclick="addSettingsMapEntry(\'' + escapeJs(field.path) + '\', function(){ return { type: \'openai-compatible\', base_url: \'\', api_key: \'\', model: \'\' }; })">Add Provider</button></div>'
        + (sections || '<div class="settings-empty-inline">No providers configured.</div>')
        + '</div>';
}

function renderChannelsEditor(field, value) {
    var channels = value || {};
    var agentNames = getSettingsAgentNames();
    var names = Object.keys(channels).sort();
    var cards = names.map(function(name) {
        var channel = channels[name] || {};
        return '<div class="settings-complex-card">'
            + '<div class="settings-complex-card-header">'
            + '<div><div class="settings-complex-title">' + escapeHtml(name) + '</div><div class="settings-field-path">channels.' + escapeHtml(name) + '</div></div>'
            + '<button type="button" class="session-btn session-btn-danger settings-inline-btn" onclick="removeSettingsSectionEntry(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\')">Remove</button>'
            + '</div>'
            + '<div class="settings-complex-grid">'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Type</span>' + renderSettingsSelect(['webchat', 'weixin', 'wecom'], channel.type || '', 'updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'type\', this.value)', 'Select type') + '</label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Name</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(channel.name || '') + '" oninput="updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'name\', this.value)"></label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Role</span>' + renderSettingsSelect(['admin', 'employee', 'customer'], channel.role || '', 'updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'role\', this.value)', 'Select role') + '</label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Agent</span>' + renderSettingsSelect(agentNames, channel.agent || '', 'updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'agent\', this.value)', 'Select agent') + '</label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Token</span>' + renderSettingsSecretInput(channel.token || '', 'updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'token\', this.value)') + '</label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">User ID</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(channel.user_id || '') + '" oninput="updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'user_id\', this.value)"></label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Corp ID</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(channel.corp_id || '') + '" oninput="updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'corp_id\', this.value)"></label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Corp Secret</span>' + renderSettingsSecretInput(channel.corp_secret || '', 'updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'corp_secret\', this.value)') + '</label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Agent ID</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(channel.agent_id === undefined || channel.agent_id === null ? '' : String(channel.agent_id)) + '" oninput="updateSettingsMapNumber(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'agent_id\', this.value)"></label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Encoding AES Key</span>' + renderSettingsSecretInput(channel.encoding_aes_key || '', 'updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'encoding_aes_key\', this.value)') + '</label>'
            + '<label class="settings-stack-field settings-stack-field-span"><span class="settings-inline-label">API base URL</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(channel.api_base_url || '') + '" oninput="updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'api_base_url\', this.value)"></label>'
            + '<label class="settings-stack-field settings-stack-field-span"><span class="settings-inline-label">Callback path</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(channel.callback_path || '') + '" oninput="updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'callback_path\', this.value)" placeholder="/wecom/agent"></label>'
            + '<label class="settings-stack-field settings-stack-field-span"><span class="settings-inline-label">Receive ID</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(channel.receive_id || '') + '" oninput="updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'receive_id\', this.value)"></label>'
            + '<label class="settings-stack-field settings-stack-field-span"><span class="settings-inline-label">Allowed users</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml((channel.allowed_users || []).join(', ')) + '" oninput="updateSettingsMapStringArray(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'allowed_users\', this.value)" placeholder="alice, bob"></label>'
            + '</div>'
            + '</div>';
    }).join('');
    return '<div class="settings-complex-editor">'
        + '<div class="settings-complex-toolbar"><button type="button" class="session-btn settings-inline-btn" onclick="addSettingsMapEntry(\'' + escapeJs(field.path) + '\', function(){ return { type: \'webchat\', name: \'\', role: \'customer\', agent: \'\', token: \'\', user_id: \'\', corp_id: \'\', corp_secret: \'\', agent_id: 0, encoding_aes_key: \'\', api_base_url: \'\', callback_path: \'\', receive_id: \'\', allowed_users: [] }; })">Add channel</button></div>'
        + (cards || '<div class="settings-empty-inline">No channels configured.</div>')
        + '</div>';
}

function renderEndpointsEditor(field, value) {
    var endpoints = value || {};
    var names = Object.keys(endpoints).sort();
    var cards = names.map(function(name) {
        var endpoint = endpoints[name] || {};
        var auth = endpoint.auth || {};
        return '<div class="settings-complex-card">'
            + '<div class="settings-complex-card-header">'
            + '<div><div class="settings-complex-title">' + escapeHtml(name) + '</div><div class="settings-field-path">tools.backend_call.endpoints.' + escapeHtml(name) + '</div></div>'
            + '<button type="button" class="session-btn session-btn-danger settings-inline-btn" onclick="removeSettingsSectionEntry(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\')">Remove</button>'
            + '</div>'
            + '<div class="settings-complex-grid">'
            + '<label class="settings-stack-field settings-stack-field-span"><span class="settings-inline-label">URL</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(endpoint.url || '') + '" oninput="updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'url\', this.value)"></label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Method</span>' + renderSettingsSelect(['GET', 'POST', 'PUT', 'PATCH', 'DELETE'], endpoint.method || '', 'updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'method\', this.value)', 'Select method') + '</label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Timeout</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(endpoint.timeout || '') + '" oninput="updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'timeout\', this.value)" placeholder="10s"></label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Rate limit</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(endpoint.rate_limit === undefined || endpoint.rate_limit === null ? '' : String(endpoint.rate_limit)) + '" oninput="updateSettingsMapNumber(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'rate_limit\', this.value)"></label>'
            + '<label class="settings-stack-field settings-stack-field-span"><span class="settings-inline-label">Allowed roles</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml((endpoint.allowed_roles || []).join(', ')) + '" oninput="updateSettingsMapStringArray(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'allowed_roles\', this.value)" placeholder="admin, employee"></label>'
            + '</div>'
            + '<div class="settings-subsection">'
            + '<div class="settings-subsection-title">Auth</div>'
            + '<div class="settings-complex-grid">'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Type</span>' + renderSettingsSelect(['bearer', 'basic', 'header', 'none'], auth.type || '', 'updateSettingsNestedMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'auth\', \'type\', this.value)', 'Select auth') + '</label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Header</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(auth.header || '') + '" oninput="updateSettingsNestedMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'auth\', \'header\', this.value)"></label>'
            + '<label class="settings-stack-field settings-stack-field-span"><span class="settings-inline-label">Token</span>' + renderSettingsSecretInput(auth.token || '', 'updateSettingsNestedMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'auth\', \'token\', this.value)') + '</label>'
            + '</div>'
            + '</div>'
            + '</div>';
    }).join('');
    return '<div class="settings-complex-editor">'
        + '<div class="settings-complex-toolbar"><button type="button" class="session-btn settings-inline-btn" onclick="addSettingsMapEntry(\'' + escapeJs(field.path) + '\', function(){ return { url: \'\', method: \'POST\', auth: { type: \'bearer\', token: \'\', header: \'Authorization\' }, allowed_roles: [], rate_limit: 0, timeout: \'10s\' }; })">Add endpoint</button></div>'
        + (cards || '<div class="settings-empty-inline">No backend endpoints configured.</div>')
        + '</div>';
}

function renderMCPServersEditor(field, value) {
    var servers = value || {};
    var names = Object.keys(servers).sort();
    var cards = names.map(function(name) {
        var server = servers[name] || {};
        return '<div class="settings-complex-card">'
            + '<div class="settings-complex-card-header">'
            + '<div><div class="settings-complex-title">' + escapeHtml(name) + '</div><div class="settings-field-path">mcp.servers.' + escapeHtml(name) + '</div></div>'
            + '<button type="button" class="session-btn session-btn-danger settings-inline-btn" onclick="removeSettingsSectionEntry(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\')">Remove</button>'
            + '</div>'
            + '<div class="settings-inline-toggle-group">'
            + '<label class="settings-toggle"><input type="checkbox" ' + (server.enabled ? 'checked' : '') + ' onchange="updateSettingsMapBoolean(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'enabled\', this.checked)"><span>Enabled</span></label>'
            + '</div>'
            + '<div class="settings-complex-grid">'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Type</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(server.type || '') + '" oninput="updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'type\', this.value)"></label>'
            + '<label class="settings-stack-field settings-stack-field-span"><span class="settings-inline-label">URL</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(server.url || '') + '" oninput="updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'url\', this.value)"></label>'
            + '<label class="settings-stack-field settings-stack-field-span"><span class="settings-inline-label">Command</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml((server.command || []).join(', ')) + '" oninput="updateSettingsMapStringArray(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'command\', this.value)" placeholder="npx, -y, package"></label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Timeout (ms)</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(server.timeout === undefined || server.timeout === null ? '' : String(server.timeout)) + '" oninput="updateSettingsMapNumber(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'timeout\', this.value)"></label>'
            + '</div>'
            + '<div class="settings-subsection">'
            + '<div class="settings-subsection-header"><div class="settings-subsection-title">Environment</div><button type="button" class="session-btn settings-inline-btn" onclick="addSettingsNestedMapEntry(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'environment\', function(){ return \'\'; })">Add env</button></div>'
            + renderSettingsKVRows(server.environment || {}, 'updateSettingsNestedMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'environment\', \'__KEY__\',', 'removeSettingsNestedMapEntry(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'environment\', \'__KEY__\')', 'No environment variables configured.', 'text')
            + '</div>'
            + '<div class="settings-subsection">'
            + '<div class="settings-subsection-header"><div class="settings-subsection-title">Headers</div><button type="button" class="session-btn settings-inline-btn" onclick="addSettingsNestedMapEntry(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'headers\', function(){ return \'\'; })">Add header</button></div>'
            + renderSettingsKVRows(server.headers || {}, 'updateSettingsNestedMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'headers\', \'__KEY__\',', 'removeSettingsNestedMapEntry(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'headers\', \'__KEY__\')', 'No headers configured.', 'text')
            + '</div>'
            + '</div>';
    }).join('');
    return '<div class="settings-complex-editor">'
        + '<div class="settings-complex-toolbar"><button type="button" class="session-btn settings-inline-btn" onclick="addSettingsMapEntry(\'' + escapeJs(field.path) + '\', function(){ return { type: \'local\', enabled: true, command: [], environment: {}, url: \'\', headers: {}, timeout: 0 }; })">Add server</button></div>'
        + (cards || '<div class="settings-empty-inline">No MCP servers configured.</div>')
        + '</div>';
}

function renderMemoryEditor(field, value) {
    var memory = value || {};
    var extract = memory.extract || {};
    var cleanup = memory.cleanup || {};
    var shortTerm = memory.short_term || {};
    var mediumTerm = memory.medium_term || {};
    var longTerm = memory.long_term || {};
    var vector = longTerm.vector || {};
    var embedder = vector.embedder || {};
    var cleanupPolicy = longTerm.cleanup_policy || {};
    var reference = cleanupPolicy.reference || {};
    var coreMemory = memory.core_memory || {};
    var bootstrap = memory.bootstrap || {};
    return '<div class="settings-complex-editor">'
        + '<div class="settings-subsection">'
        + '<div class="settings-subsection-title">Extract</div>'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-toggle"><input type="checkbox" ' + (extract.enabled ? 'checked' : '') + ' onchange="updateSettingsField(\'memory.extract.enabled\', this.checked, \'boolean\')"><span>Enabled</span></label>'
        + '<label class="settings-toggle"><input type="checkbox" ' + (extract.enable_llm_gating ? 'checked' : '') + ' onchange="updateSettingsField(\'memory.extract.enable_llm_gating\', this.checked, \'boolean\')"><span>LLM gating</span></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Interval</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(extract.interval || '') + '" oninput="updateSettingsField(\'memory.extract.interval\', this.value, \'text\')" placeholder="1h"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Medium lookback days</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(extract.medium_lookback_days === undefined || extract.medium_lookback_days === null ? '' : String(extract.medium_lookback_days)) + '" oninput="updateSettingsField(\'memory.extract.medium_lookback_days\', this.value, \'number\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Min medium chars</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(extract.min_medium_chars === undefined || extract.min_medium_chars === null ? '' : String(extract.min_medium_chars)) + '" oninput="updateSettingsField(\'memory.extract.min_medium_chars\', this.value, \'number\')"></label>'
        + '</div>'
        + '</div>'
        + '<div class="settings-subsection">'
        + '<div class="settings-subsection-title">Cleanup</div>'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-toggle"><input type="checkbox" ' + (cleanup.enabled ? 'checked' : '') + ' onchange="updateSettingsField(\'memory.cleanup.enabled\', this.checked, \'boolean\')"><span>Enabled</span></label>'
        + '<label class="settings-toggle"><input type="checkbox" ' + (cleanup.cleanup_user_longterm ? 'checked' : '') + ' onchange="updateSettingsField(\'memory.cleanup.cleanup_user_longterm\', this.checked, \'boolean\')"><span>Cleanup user longterm</span></label>'
        + '<label class="settings-toggle"><input type="checkbox" ' + (cleanup.cleanup_agent_longterm ? 'checked' : '') + ' onchange="updateSettingsField(\'memory.cleanup.cleanup_agent_longterm\', this.checked, \'boolean\')"><span>Cleanup agent longterm</span></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Schedule</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(cleanup.schedule || '') + '" oninput="updateSettingsField(\'memory.cleanup.schedule\', this.value, \'text\')" placeholder="0 3 * * *"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Medium days window</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(cleanup.medium_days_window === undefined || cleanup.medium_days_window === null ? '' : String(cleanup.medium_days_window)) + '" oninput="updateSettingsField(\'memory.cleanup.medium_days_window\', this.value, \'number\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Min chars to cleanup</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(cleanup.min_chars_to_cleanup === undefined || cleanup.min_chars_to_cleanup === null ? '' : String(cleanup.min_chars_to_cleanup)) + '" oninput="updateSettingsField(\'memory.cleanup.min_chars_to_cleanup\', this.value, \'number\')"></label>'
        + '</div>'
        + '</div>'
        + '<div class="settings-subsection">'
        + '<div class="settings-subsection-title">Short Term</div>'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-toggle"><input type="checkbox" ' + (shortTerm.flush_before_compact ? 'checked' : '') + ' onchange="updateSettingsField(\'memory.short_term.flush_before_compact\', this.checked, \'boolean\')"><span>Flush before compact</span></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Max messages</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(shortTerm.max_messages === undefined || shortTerm.max_messages === null ? '' : String(shortTerm.max_messages)) + '" oninput="updateSettingsField(\'memory.short_term.max_messages\', this.value, \'number\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Max tokens</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(shortTerm.max_tokens === undefined || shortTerm.max_tokens === null ? '' : String(shortTerm.max_tokens)) + '" oninput="updateSettingsField(\'memory.short_term.max_tokens\', this.value, \'number\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Keep recent</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(shortTerm.keep_recent === undefined || shortTerm.keep_recent === null ? '' : String(shortTerm.keep_recent)) + '" oninput="updateSettingsField(\'memory.short_term.keep_recent\', this.value, \'number\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Session TTL</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(shortTerm.session_ttl || '') + '" oninput="updateSettingsField(\'memory.short_term.session_ttl\', this.value, \'text\')" placeholder="24h"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Cleanup interval</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(shortTerm.cleanup_interval || '') + '" oninput="updateSettingsField(\'memory.short_term.cleanup_interval\', this.value, \'text\')" placeholder="1h"></label>'
        + '</div>'
        + '</div>'
        + '<div class="settings-subsection">'
        + '<div class="settings-subsection-title">Medium Term</div>'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-stack-field settings-stack-field-span"><span class="settings-inline-label">Directory</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(mediumTerm.dir || '') + '" oninput="updateSettingsField(\'memory.medium_term.dir\', this.value, \'text\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Max size</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(mediumTerm.max_size === undefined || mediumTerm.max_size === null ? '' : String(mediumTerm.max_size)) + '" oninput="updateSettingsField(\'memory.medium_term.max_size\', this.value, \'number\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Load days</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(mediumTerm.load_days === undefined || mediumTerm.load_days === null ? '' : String(mediumTerm.load_days)) + '" oninput="updateSettingsField(\'memory.medium_term.load_days\', this.value, \'number\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Min messages to extract</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(mediumTerm.min_messages_to_extract === undefined || mediumTerm.min_messages_to_extract === null ? '' : String(mediumTerm.min_messages_to_extract)) + '" oninput="updateSettingsField(\'memory.medium_term.min_messages_to_extract\', this.value, \'number\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Compression threshold</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(mediumTerm.compression_threshold === undefined || mediumTerm.compression_threshold === null ? '' : String(mediumTerm.compression_threshold)) + '" oninput="updateSettingsField(\'memory.medium_term.compression_threshold\', this.value, \'number\')"></label>'
        + '</div>'
        + '</div>'
        + '<div class="settings-subsection">'
        + '<div class="settings-subsection-title">Long Term</div>'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Dedup threshold</span><input type="number" step="0.01" class="settings-input settings-input-wide" value="' + escapeHtml(longTerm.dedup_threshold === undefined || longTerm.dedup_threshold === null ? '' : String(longTerm.dedup_threshold)) + '" oninput="updateSettingsField(\'memory.long_term.dedup_threshold\', this.value, \'number\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Compression threshold</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(longTerm.compression_threshold === undefined || longTerm.compression_threshold === null ? '' : String(longTerm.compression_threshold)) + '" oninput="updateSettingsField(\'memory.long_term.compression_threshold\', this.value, \'number\')"></label>'
        + '</div>'
        + '<div class="settings-subsection">'
        + '<div class="settings-subsection-title">Vector</div>'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-toggle"><input type="checkbox" ' + (vector.enabled ? 'checked' : '') + ' onchange="updateSettingsField(\'memory.long_term.vector.enabled\', this.checked, \'boolean\')"><span>Enabled</span></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Prefetch limit</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(vector.prefetch_limit === undefined || vector.prefetch_limit === null ? '' : String(vector.prefetch_limit)) + '" oninput="updateSettingsField(\'memory.long_term.vector.prefetch_limit\', this.value, \'number\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Score threshold</span><input type="number" step="0.01" class="settings-input settings-input-wide" value="' + escapeHtml(vector.score_threshold === undefined || vector.score_threshold === null ? '' : String(vector.score_threshold)) + '" oninput="updateSettingsField(\'memory.long_term.vector.score_threshold\', this.value, \'number\')"></label>'
        + '</div>'
        + '</div>'
        + '<div class="settings-subsection">'
        + '<div class="settings-subsection-title">Embedder</div>'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Type</span>' + renderSettingsSelect(['openai', 'local', 'ollama'], embedder.type || '', 'updateSettingsField(\'memory.long_term.vector.embedder.type\', this.value, \'text\')', 'Select type') + '</label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Model</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(embedder.model || '') + '" oninput="updateSettingsField(\'memory.long_term.vector.embedder.model\', this.value, \'text\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Dimensions</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(embedder.dimensions === undefined || embedder.dimensions === null ? '' : String(embedder.dimensions)) + '" oninput="updateSettingsField(\'memory.long_term.vector.embedder.dimensions\', this.value, \'number\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Base URL</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(embedder.base_url || '') + '" oninput="updateSettingsField(\'memory.long_term.vector.embedder.base_url\', this.value, \'text\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">API Key</span>' + renderSettingsSecretInput(embedder.api_key || '', 'updateSettingsField(\'memory.long_term.vector.embedder.api_key\', this.value, \'text\')') + '</label>'
        + '</div>'
        + '</div>'
        + '<div class="settings-subsection">'
        + '<div class="settings-subsection-title">Cleanup Policy</div>'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Check interval</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(cleanupPolicy.check_interval || '') + '" oninput="updateSettingsField(\'memory.long_term.cleanup_policy.check_interval\', this.value, \'text\')" placeholder="24h"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Min age days</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(cleanupPolicy.min_age_days === undefined || cleanupPolicy.min_age_days === null ? '' : String(cleanupPolicy.min_age_days)) + '" oninput="updateSettingsField(\'memory.long_term.cleanup_policy.min_age_days\', this.value, \'number\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Batch size</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(cleanupPolicy.batch_size === undefined || cleanupPolicy.batch_size === null ? '' : String(cleanupPolicy.batch_size)) + '" oninput="updateSettingsField(\'memory.long_term.cleanup_policy.batch_size\', this.value, \'number\')"></label>'
        + '</div>'
        + '</div>'
        + '<div class="settings-subsection">'
        + '<div class="settings-subsection-title">Reference</div>'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-toggle"><input type="checkbox" ' + (reference.include_core_memory ? 'checked' : '') + ' onchange="updateSettingsField(\'memory.long_term.cleanup_policy.reference.include_core_memory\', this.checked, \'boolean\')"><span>Include core memory</span></label>'
        + '<label class="settings-toggle"><input type="checkbox" ' + (reference.include_access_stats ? 'checked' : '') + ' onchange="updateSettingsField(\'memory.long_term.cleanup_policy.reference.include_access_stats\', this.checked, \'boolean\')"><span>Include access stats</span></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Medium memory days</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(reference.medium_memory_days === undefined || reference.medium_memory_days === null ? '' : String(reference.medium_memory_days)) + '" oninput="updateSettingsField(\'memory.long_term.cleanup_policy.reference.medium_memory_days\', this.value, \'number\')"></label>'
        + '</div>'
        + '</div>'
        + '</div>'
        + '<div class="settings-subsection">'
        + '<div class="settings-subsection-title">Core Memory</div>'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-toggle"><input type="checkbox" ' + (coreMemory.auto_consolidate ? 'checked' : '') + ' onchange="updateSettingsField(\'memory.core_memory.auto_consolidate\', this.checked, \'boolean\')"><span>Auto consolidate</span></label>'
        + '<label class="settings-stack-field settings-stack-field-span"><span class="settings-inline-label">File</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(coreMemory.file || '') + '" oninput="updateSettingsField(\'memory.core_memory.file\', this.value, \'text\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Importance threshold</span><input type="number" step="0.01" class="settings-input settings-input-wide" value="' + escapeHtml(coreMemory.importance_threshold === undefined || coreMemory.importance_threshold === null ? '' : String(coreMemory.importance_threshold)) + '" oninput="updateSettingsField(\'memory.core_memory.importance_threshold\', this.value, \'number\')"></label>'
        + '</div>'
        + '</div>'
        + '<div class="settings-subsection">'
        + '<div class="settings-subsection-title">Bootstrap</div>'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Max file chars</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(bootstrap.max_file_chars === undefined || bootstrap.max_file_chars === null ? '' : String(bootstrap.max_file_chars)) + '" oninput="updateSettingsField(\'memory.bootstrap.max_file_chars\', this.value, \'number\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Max total chars</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(bootstrap.max_total_chars === undefined || bootstrap.max_total_chars === null ? '' : String(bootstrap.max_total_chars)) + '" oninput="updateSettingsField(\'memory.bootstrap.max_total_chars\', this.value, \'number\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Warning threshold</span><input type="number" step="0.01" class="settings-input settings-input-wide" value="' + escapeHtml(bootstrap.warning_threshold === undefined || bootstrap.warning_threshold === null ? '' : String(bootstrap.warning_threshold)) + '" oninput="updateSettingsField(\'memory.bootstrap.warning_threshold\', this.value, \'number\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Truncation strategy</span>' + renderSettingsSelect(['head', 'tail'], bootstrap.truncation_strategy || '', 'updateSettingsField(\'memory.bootstrap.truncation_strategy\', this.value, \'text\')', 'Select strategy') + '</label>'
        + '</div>'
        + '</div>';
}

function renderProxyEditor(field, value) {
    var proxy = value || {};
    var subprocesses = proxy.subprocesses || {};
    return '<div class="settings-complex-editor">'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-toggle"><input type="checkbox" ' + (proxy.enabled ? 'checked' : '') + ' onchange="updateSettingsField(\'proxy.enabled\', this.checked, \'boolean\')"><span>Enabled</span></label>'
        + '</div>'
        + '<div class="settings-subsection">'
        + '<div class="settings-subsection-title">Proxy URLs</div>'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-stack-field"><span class="settings-inline-label">HTTP Proxy</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(proxy.http_proxy || '') + '" oninput="updateSettingsField(\'proxy.http_proxy\', this.value, \'text\')" placeholder="http://127.0.0.1:7897"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">HTTPS Proxy</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(proxy.https_proxy || '') + '" oninput="updateSettingsField(\'proxy.https_proxy\', this.value, \'text\')" placeholder="http://127.0.0.1:7897"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">SOCKS Proxy</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(proxy.socks_proxy || '') + '" oninput="updateSettingsField(\'proxy.socks_proxy\', this.value, \'text\')" placeholder="socks5://127.0.0.1:1080"></label>'
        + '</div>'
        + '</div>'
        + '<div class="settings-subsection">'
        + '<div class="settings-subsection-title">Authentication</div>'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Username</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(proxy.username || '') + '" oninput="updateSettingsField(\'proxy.username\', this.value, \'text\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Password</span>' + renderSettingsSecretInput(proxy.password || '', 'updateSettingsField(\'proxy.password\', this.value, \'text\')') + '</label>'
        + '</div>'
        + '</div>'
        + '<div class="settings-subsection">'
        + '<div class="settings-subsection-title">No Proxy (Bypass List)</div>'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-stack-field settings-stack-field-span"><span class="settings-inline-label">Domains</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml((proxy.no_proxy || []).join(', ')) + '" oninput="updateSettingsField(\'proxy.no_proxy\', this.value.split(\',\').map(function(item){ return item.trim(); }).filter(function(item){ return item.length > 0; }), \'text\')" placeholder="localhost, internal.example.com"></label>'
        + '</div>'
        + '</div>'
        + '<div class="settings-subsection">'
        + '<div class="settings-subsection-title">Subprocesses</div>'
        + '<div class="settings-inline-toggle-group">'
        + '<label class="settings-toggle"><input type="checkbox" ' + (subprocesses.mcp ? 'checked' : '') + ' onchange="updateSettingsField(\'proxy.subprocesses.mcp\', this.checked, \'boolean\')"><span>MCP inherits proxy</span></label>'
        + '<label class="settings-toggle"><input type="checkbox" ' + (subprocesses.plugin ? 'checked' : '') + ' onchange="updateSettingsField(\'proxy.subprocesses.plugin\', this.checked, \'boolean\')"><span>Plugin inherits proxy</span></label>'
        + '</div>'
        + '</div>'
        + '</div>';
}

function renderHeartbeatEditor(field, value) {
    value = value || {};
    return '<div class="settings-complex-editor">'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-toggle"><input type="checkbox" ' + (value.enabled ? 'checked' : '') + ' onchange="updateSettingsField(\'heartbeat.enabled\', this.checked, \'boolean\')"><span>Enabled</span></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Interval</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(value.interval || '') + '" oninput="updateSettingsField(\'heartbeat.interval\', this.value, \'text\')" placeholder="10m"></label>'
        + '<label class="settings-stack-field settings-stack-field-span"><span class="settings-inline-label">Prompt</span><textarea class="settings-json-editor" spellcheck="false" oninput="updateSettingsField(\'heartbeat.prompt\', this.value, \'text\')">' + escapeHtml(value.prompt || '') + '</textarea></label>'
        + '</div>'
        + '</div>';
}

function renderSchedulerEditor(field, value) {
    var scheduler = value || {};
    var systemTasks = scheduler.system_tasks || {};
    var memoryExtract = systemTasks.memory_extract || {};
    var memoryCleanup = systemTasks.memory_cleanup || {};
    return '<div class="settings-complex-editor">'
        + '<div class="schedule-helper-row settings-subsection-helper">'
        + '<div class="schedule-helper-text">System tasks use standard 5-field cron expressions and are hot-applied by the runtime.</div>'
        + '</div>'
        + '<div class="settings-subsection">'
        + '<div class="settings-subsection-title">Memory Extract</div>'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-stack-field settings-stack-field-span"><span class="settings-inline-label">Schedule</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(memoryExtract.schedule || '') + '" oninput="updateSettingsField(\'scheduler.system_tasks.memory_extract.schedule\', this.value, \'text\')" placeholder="0 * * * *"></label>'
        + '</div>'
        + '</div>'
        + '<div class="settings-subsection">'
        + '<div class="settings-subsection-title">Memory Cleanup</div>'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-stack-field settings-stack-field-span"><span class="settings-inline-label">Schedule</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(memoryCleanup.schedule || '') + '" oninput="updateSettingsField(\'scheduler.system_tasks.memory_cleanup.schedule\', this.value, \'text\')" placeholder="0 3 * * *"></label>'
        + '</div>'
        + '</div>'
        + '</div>';
}

function renderWSServerEditor(field, value) {
    var ws = value || {};
    return '<div class="settings-complex-editor">'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Host</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(ws.host || '127.0.0.1') + '" oninput="updateSettingsField(\'plugins.ws_server.host\', this.value, \'text\')"></label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Port</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(ws.port === undefined || ws.port === null ? '' : String(ws.port)) + '" oninput="updateSettingsField(\'plugins.ws_server.port\', this.value, \'number\')"></label>'
        + '</div>'
        + '</div>';
}

function renderPluginsEditor(field, value) {
    var plugins = value || {};
    var names = Object.keys(plugins).sort();
    var cards = names.map(function(name) {
        var plugin = plugins[name] || {};
        var capabilities = plugin.capabilities || {};
        return '<div class="settings-complex-card">'
            + '<div class="settings-complex-card-header">'
            + '<div><div class="settings-complex-title">' + escapeHtml(name) + '</div><div class="settings-field-path">plugins.plugins.' + escapeHtml(name) + '</div></div>'
            + '<button type="button" class="session-btn session-btn-danger settings-inline-btn" onclick="removeSettingsSectionEntry(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\')">Remove</button>'
            + '</div>'
            + '<div class="settings-inline-toggle-group">'
            + '<label class="settings-toggle"><input type="checkbox" ' + (plugin.enabled ? 'checked' : '') + ' onchange="updateSettingsMapBoolean(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'enabled\', this.checked)"><span>Enabled</span></label>'
            + '<label class="settings-toggle"><input type="checkbox" ' + (plugin.override ? 'checked' : '') + ' onchange="updateSettingsMapBoolean(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'override\', this.checked)"><span>Override</span></label>'
            + '</div>'
            + '<div class="settings-complex-grid">'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Type</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(plugin.type || '') + '" oninput="updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'type\', this.value)" placeholder="local, remote"></label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">URL</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(plugin.url || '') + '" oninput="updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'url\', this.value)"></label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Token</span>' + renderSettingsSecretInput(plugin.token || '', 'updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'token\', this.value)') + '</label>'
            + '<label class="settings-stack-field settings-stack-field-span"><span class="settings-inline-label">Command</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml((plugin.command || []).join(', ')) + '" oninput="updateSettingsMapStringArray(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'command\', this.value)" placeholder="go, run, ./plugin"></label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Restart</span>' + renderSettingsSelect(['never', 'on-failure', 'always'], plugin.restart || '', 'updateSettingsMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'restart\', this.value)', 'Select policy') + '</label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Restart delay (ms)</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(plugin.restart_delay === undefined || plugin.restart_delay === null ? '' : String(plugin.restart_delay)) + '" oninput="updateSettingsMapNumber(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'restart_delay\', this.value)"></label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Max restarts</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(plugin.max_restarts === undefined || plugin.max_restarts === null ? '' : String(plugin.max_restarts)) + '" oninput="updateSettingsMapNumber(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'max_restarts\', this.value)"></label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Connect timeout (ms)</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(plugin.connect_timeout === undefined || plugin.connect_timeout === null ? '' : String(plugin.connect_timeout)) + '" oninput="updateSettingsMapNumber(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'connect_timeout\', this.value)"></label>'
            + '<label class="settings-stack-field"><span class="settings-inline-label">Request timeout (ms)</span><input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(plugin.request_timeout === undefined || plugin.request_timeout === null ? '' : String(plugin.request_timeout)) + '" oninput="updateSettingsMapNumber(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'request_timeout\', this.value)"></label>'
            + '</div>'
            + '<div class="settings-subsection">'
            + '<div class="settings-subsection-header"><div class="settings-subsection-title">Environment</div><button type="button" class="session-btn settings-inline-btn" onclick="addSettingsNestedMapEntry(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'environment\', function(){ return \'\' })">Add env</button></div>'
            + renderSettingsKVRows(plugin.environment || {}, 'updateSettingsNestedMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'environment\', \'__KEY__\',', 'removeSettingsNestedMapEntry(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'environment\', \'__KEY__\')', 'No environment variables configured.', 'text')
            + '</div>'
            + '<div class="settings-subsection">'
            + '<div class="settings-subsection-title">Capabilities</div>'
            + '<div class="settings-complex-grid">'
            + '<label class="settings-stack-field settings-stack-field-span"><span class="settings-inline-label">Allow</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml((capabilities.allow || []).join(', ')) + '" oninput="updateSettingsNestedMapString(\'' + escapeJs(field.path) + '\', \'' + escapeJs(name) + '\', \'capabilities\', \'allow\', this.value.split(\',\').map(function(item){ return item.trim(); }).filter(function(item){ return item.length > 0; }))" placeholder="provider, channel, hook"></label>'
            + '</div>'
            + '</div>'
            + '</div>';
    }).join('');
    return '<div class="settings-complex-editor">'
        + '<div class="settings-complex-toolbar"><button type="button" class="session-btn settings-inline-btn" onclick="addSettingsMapEntry(\'' + escapeJs(field.path) + '\', function(){ return { enabled: true, type: \'local\', command: [], environment: {}, url: \'\', token: \'\', restart: \'never\', restart_delay: 0, max_restarts: 0, override: false, capabilities: { allow: [] }, connect_timeout: 0, request_timeout: 0 }; })">Add plugin</button></div>'
        + (cards || '<div class="settings-empty-inline">No plugins configured.</div>')
        + '</div>';
}

function renderSessionToolsEditor(field, value) {
    var session = value || {};
    var visibility = session.visibility || {};
    var allow = session.allow || {};
    return '<div class="settings-complex-editor">'
        + '<div class="settings-inline-toggle-group">'
        + '<label class="settings-toggle"><input type="checkbox" ' + (session.enabled ? 'checked' : '') + ' onchange="updateSettingsField(\'tools.session.enabled\', this.checked, \'boolean\')"><span>Enabled</span></label>'
        + '</div>'
        + '<div class="settings-subsection">'
        + '<div class="settings-subsection-title">Visibility</div>'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Employee</span>' + renderSettingsSelect(['user', 'self', 'all'], visibility.employee || '', 'updateSettingsField(\'tools.session.visibility.employee\', this.value, \'text\')', 'Select visibility') + '</label>'
        + '<label class="settings-stack-field"><span class="settings-inline-label">Customer</span>' + renderSettingsSelect(['user', 'self', 'all'], visibility.customer || '', 'updateSettingsField(\'tools.session.visibility.customer\', this.value, \'text\')', 'Select visibility') + '</label>'
        + '</div>'
        + '</div>'
        + '<div class="settings-subsection">'
        + '<div class="settings-subsection-title">Allow</div>'
        + '<div class="settings-complex-grid">'
        + '<label class="settings-stack-field settings-stack-field-span"><span class="settings-inline-label">Employee actions</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml((allow.employee || []).join(', ')) + '" oninput="updateSettingsField(\'tools.session.allow.employee\', this.value.split(\',\').map(function(item){ return item.trim(); }).filter(function(item){ return item.length > 0; }), \'text\')" placeholder="sessions_list, sessions_history, sessions_send"></label>'
        + '<label class="settings-stack-field settings-stack-field-span"><span class="settings-inline-label">Customer actions</span><input type="text" class="settings-input settings-input-wide" value="' + escapeHtml((allow.customer || []).join(', ')) + '" oninput="updateSettingsField(\'tools.session.allow.customer\', this.value.split(\',\').map(function(item){ return item.trim(); }).filter(function(item){ return item.length > 0; }), \'text\')"></label>'
        + '</div>'
        + '</div>'
        + '</div>';
}

function renderCustomSettingsField(field, value) {
    if (field.path === 'agents') return renderAgentsEditor(field, value);
    if (field.path === 'llm.providers') return renderProvidersEditor(field, value);
    if (field.path === 'channels') return renderChannelsEditor(field, value);
    if (field.path === 'plugins.ws_server') return renderWSServerEditor(field, value);
    if (field.path === 'plugins.plugins') return renderPluginsEditor(field, value);
    if (field.path === 'tools.backend_call.endpoints') return renderEndpointsEditor(field, value);
    if (field.path === 'tools.session') return renderSessionToolsEditor(field, value);
    if (field.path === 'memory') return renderMemoryEditor(field, value);
    if (field.path === 'heartbeat') return renderHeartbeatEditor(field, value);
    if (field.path === 'scheduler') return renderSchedulerEditor(field, value);
    if (field.path === 'mcp.servers') return renderMCPServersEditor(field, value);
    if (field.path === 'proxy') return renderProxyEditor(field, value);
    return '';
}

function renderSettingsField(field, issueMap) {
    var value = getSettingsByPath(settingsDraft, field.path);
    var fieldIssues = getSettingsFieldIssues(issueMap, field.path);
    var restartTag = field.restart_required ? '<span class="settings-field-tag">restart required</span>' : '';
    var sensitiveTag = field.sensitive ? '<span class="settings-field-tag">secret</span>' : '';
    var description = field.description ? '<div class="settings-field-description">' + escapeHtml(field.description) + '</div>' : '';
    var meta = restartTag || sensitiveTag ? '<div class="settings-field-tags">' + restartTag + sensitiveTag + '</div>' : '';
    var inputHtml = renderCustomSettingsField(field, value);

    if (!inputHtml) {
        if (field.path === 'default_agent') {
            inputHtml = renderSettingsSelect(getSettingsAgentNames(), value || '', 'updateSettingsField(\'' + escapeJs(field.path) + '\', this.value, \'text\')', 'Select default agent');
        } else if (field.path === 'llm.default_provider') {
            inputHtml = renderSettingsSelect(getSettingsProviderNames(), value || '', 'updateSettingsField(\'' + escapeJs(field.path) + '\', this.value, \'text\')', 'Select default provider');
        } else if (field.type === 'boolean') {
            inputHtml = '<label class="settings-toggle"><input type="checkbox" ' + (value ? 'checked' : '') + ' onchange="updateSettingsField(\'' + escapeJs(field.path) + '\', this.checked, \'boolean\')"><span>' + (value ? 'Enabled' : 'Disabled') + '</span></label>';
        } else if (field.type === 'select') {
            var options = (field.enum || []).map(function(option) {
                return '<option value="' + escapeHtml(option) + '"' + (String(value || '') === option ? ' selected' : '') + '>' + escapeHtml(option) + '</option>';
            }).join('');
            inputHtml = '<select class="settings-input settings-input-wide" onchange="updateSettingsField(\'' + escapeJs(field.path) + '\', this.value, \'text\')">' + options + '</select>';
        } else if (field.type === 'number') {
            inputHtml = '<input type="number" class="settings-input settings-input-wide" value="' + escapeHtml(value === undefined || value === null ? '' : String(value)) + '" oninput="updateSettingsField(\'' + escapeJs(field.path) + '\', this.value, \'number\')">';
        } else if (field.type === 'object' || field.type === 'map') {
            inputHtml = renderGenericObjectEditor(field, value);
        } else {
            inputHtml = field.type === 'secret'
                ? renderSettingsSecretInput(value, 'updateSettingsField(\'' + escapeJs(field.path) + '\', this.value, \'text\')')
                : '<input type="text" class="settings-input settings-input-wide" value="' + escapeHtml(value === undefined || value === null ? '' : String(value)) + '" oninput="updateSettingsField(\'' + escapeJs(field.path) + '\', this.value, \'text\')">';
        }
    }

    var errorsHtml = fieldIssues.length ? '<div class="settings-field-errors">' + fieldIssues.map(function(message) {
        return '<div class="settings-field-error">' + escapeHtml(message) + '</div>';
    }).join('') + '</div>' : '';

    return '<div class="settings-field-card' + (fieldIssues.length ? ' has-error' : '') + '">'
        + '<div class="settings-field-header">'
        + '<div>'
        + '<div class="settings-label">' + escapeHtml(field.label || field.path) + '</div>'
        + '<div class="settings-field-path">' + escapeHtml(field.path) + '</div>'
        + description
        + '</div>'
        + meta
        + '</div>'
        + '<div class="settings-field-input">' + inputHtml + '</div>'
        + errorsHtml
        + '</div>';
}

function renderSettingsIssues() {
    var container = document.getElementById('settingsIssues');
    if (!container) return;
    if (!settingsIssues || settingsIssues.length === 0) {
        container.innerHTML = '';
        container.style.display = 'none';
        return;
    }
    container.innerHTML = '<div class="settings-issues-title">Validation Issues</div><ul>' + formatSettingsIssues(settingsIssues) + '</ul>';
    container.style.display = 'block';
}

function renderSettings() {
    var container = document.getElementById('settingsPanel');
    if (!container) return;

    var issueMap = getSettingsIssueMap();
    var sections = settingsSchema || [];
    if (!sections.length) {
        container.innerHTML = '<div class="settings-section"><div class="settings-section-title">Settings</div><div class="settings-editor-copy">No schema loaded yet.</div><div class="settings-issues" id="settingsIssues" style="display:none;"></div></div>';
        renderSettingsIssues();
        return;
    }

    container.innerHTML = '<div class="settings-editor-header">'
        + '<div class="settings-editor-copy">Edit structured runtime settings, validate them, then save and hot-apply supported sections.</div>'
        + '<div class="settings-dirty-hint clean" id="settingsDirtyHint">No local changes</div>'
        + '</div>'
        + sections.map(function(section) {
            var fields = (section.fields || []).map(function(field) {
                return renderSettingsField(field, issueMap);
            }).join('');
            return '<div class="settings-section">'
                + '<div class="settings-section-title">' + escapeHtml(section.label || section.id) + '</div>'
                + (section.description ? '<div class="settings-editor-copy">' + escapeHtml(section.description) + '</div>' : '')
                + '<div class="settings-field-grid">' + fields + '</div>'
                + '</div>';
        }).join('')
        + '<div class="settings-issues" id="settingsIssues" style="display:none;"></div>';

    updateSettingsDirtyState();
    renderSettingsIssues();
}

function applySettingsPayload(data) {
    settingsData = data.settings || {};
    settingsSchema = data.schema || [];
    settingsConfigPath = data.config_path || settingsConfigPath || '';
    settingsLastSavedSnapshot = cloneSettingsValue(settingsData || {});
    settingsDraft = cloneSettingsValue(settingsData || {});
    setSettingsMeta(settingsConfigPath);
    renderSettings();
}

function validateSettingsDraft() {
    settingsIssues = [];
    renderSettingsIssues();
    setSettingsStatus('Validating configuration…', 'pending');
    if (!sendWSRequest('validate_settings', { settings: settingsDraft })) {
        setSettingsStatus('WebSocket disconnected', 'error');
    }
}

function saveSettingsDraft() {
    settingsIssues = [];
    renderSettingsIssues();
    setSettingsStatus('Saving configuration…', 'pending');
    if (!sendWSRequest('save_settings', { settings: settingsDraft })) {
        setSettingsStatus('WebSocket disconnected', 'error');
    }
}

function reloadSettingsFromDisk() {
    setSettingsStatus('Reloading configuration from disk…', 'pending');
    settingsIssues = [];
    renderSettingsIssues();
    if (!sendWSRequest('reload_settings')) {
        setSettingsStatus('WebSocket disconnected', 'error');
    }
}
