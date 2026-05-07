// ===== Memory and Knowledge Pages =====

var memoryData = [];
var knowledgeData = [];
var selectedKnowledgePath = '';
var selectedKnowledgeDirectory = 'all';
var selectedKnowledgeTags = [];
var createdKnowledgeDirectories = [];

function fetchMemory(query) {
    query = query || '';
    if (!sendWSRequest('get_memory', {
        message: query,
        agent_id: currentAgentId,
        user_id: WEBCAT_USER_ID
    })) renderMemory();
}

function fetchKnowledge(query) {
    query = query || '';
    if (!sendWSRequest('get_knowledge', {
        message: query,
        user_id: WEBCAT_USER_ID
    })) renderKnowledge();
}

function createKnowledgeDirectory() {
    var directory = resolveKnowledgeDirectoryInput(getValue('knowledgeDirectoryInput'));
    if (!directory) {
        setKnowledgeEditorHint('Directory path is required.', 'error');
        return;
    }
    if (!sendWSRequest('create_knowledge_directory', {
        path: directory,
        user_id: WEBCAT_USER_ID
    })) {
        setKnowledgeEditorHint('WebSocket not connected. Could not create directory.', 'error');
        return;
    }
    setKnowledgeEditorHint('Creating directory...', 'info');
}

function deleteKnowledgeDirectory() {
    var directory = selectedKnowledgeDirectory;
    if (!directory || directory === 'all' || directory === 'root') {
        setKnowledgeEditorHint('Select an empty knowledge directory before deleting it.', 'error');
        return;
    }
    if (!window.confirm('Delete empty knowledge directory ' + directory + '?')) {
        return;
    }
    if (!sendWSRequest('delete_knowledge_directory', {
        path: directory,
        user_id: WEBCAT_USER_ID
    })) {
        setKnowledgeEditorHint('WebSocket not connected. Could not delete directory.', 'error');
        return;
    }
    setKnowledgeEditorHint('Deleting directory...', 'info');
}

function loadKnowledgeEntry(path) {
    if (!path) return;
    selectedKnowledgePath = path;
    if (!sendWSRequest('get_knowledge_entry', { path: path, user_id: WEBCAT_USER_ID })) {
        var entry = findKnowledgeEntryByPath(path);
        if (entry) populateKnowledgeEditor(entry);
    }
    renderKnowledge();
}

function saveKnowledgeEntry() {
    var path = normalizeKnowledgePath(getValue('knowledgePathInput'));
    var originalPath = normalizeKnowledgePath(getRawValue('knowledgeOriginalPathInput'));
    var title = getValue('knowledgeTitleInput');
    var content = getRawValue('knowledgeContentInput').trim();
    var tags = parseTagInput(getValue('knowledgeTagsInput'));

    if (!path) {
        setKnowledgeEditorHint('Path is required.', 'error');
        return;
    }
    if (!content) {
        setKnowledgeEditorHint('Content is required.', 'error');
        return;
    }
    if (originalPath && originalPath !== path) {
        setKnowledgeEditorHint('Path changed. Use Move / Rename to change the document path.', 'error');
        return;
    }

    if (!sendWSRequest('save_knowledge_entry', {
        path: path,
        name: title,
        message: content,
        tags: tags,
        user_id: WEBCAT_USER_ID
    })) {
        setKnowledgeEditorHint('WebSocket not connected. Could not save knowledge entry.', 'error');
        return;
    }

    selectedKnowledgePath = path;
    setKnowledgeEditorHint('Saving knowledge entry...', 'info');
}

function moveKnowledgeEntry() {
    var originalPath = normalizeKnowledgePath(getRawValue('knowledgeOriginalPathInput'));
    var targetPath = normalizeKnowledgePath(getValue('knowledgePathInput'));
    if (!originalPath) {
        setKnowledgeEditorHint('Load an existing knowledge entry before moving it.', 'error');
        return;
    }
    if (!targetPath) {
        setKnowledgeEditorHint('Target path is required.', 'error');
        return;
    }
    if (originalPath === targetPath) {
        setKnowledgeEditorHint('Path did not change. Nothing to move.', 'info');
        return;
    }
    if (!sendWSRequest('move_knowledge_entry', {
        from_path: originalPath,
        path: targetPath,
        user_id: WEBCAT_USER_ID
    })) {
        setKnowledgeEditorHint('WebSocket not connected. Could not move knowledge entry.', 'error');
        return;
    }
    setKnowledgeEditorHint('Moving knowledge entry...', 'info');
}

function deleteKnowledgeEntry() {
    var path = normalizeKnowledgePath(getValue('knowledgePathInput') || selectedKnowledgePath);
    if (!path) {
        setKnowledgeEditorHint('Select or enter a knowledge entry before deleting.', 'error');
        return;
    }
    if (!window.confirm('Delete knowledge entry ' + path + '?')) {
        return;
    }
    if (!sendWSRequest('delete_knowledge_entry', {
        path: path,
        user_id: WEBCAT_USER_ID
    })) {
        setKnowledgeEditorHint('WebSocket not connected. Could not delete knowledge entry.', 'error');
        return;
    }
    setKnowledgeEditorHint('Deleting knowledge entry...', 'info');
}

function resetKnowledgeEditor() {
    selectedKnowledgePath = '';
    setValue('knowledgeOriginalPathInput', '');
    setValue('knowledgePathInput', '');
    setValue('knowledgeTitleInput', '');
    setValue('knowledgeTagsInput', '');
    setValue('knowledgeContentInput', '');
    renderKnowledgePreview();
    renderKnowledgeCurrentPath();
    setKnowledgeEditorHint('Shared knowledge is stored under .evoduck/knowledge and is available to all agents.', 'info');
}

function populateKnowledgeEditor(entry) {
    if (!entry) return;
    selectedKnowledgePath = entry.path || selectedKnowledgePath;
    selectedKnowledgeDirectory = entry.directory || 'root';
    setValue('knowledgeOriginalPathInput', entry.path || '');
    setValue('knowledgePathInput', entry.path || '');
    setValue('knowledgeTitleInput', entry.title || '');
    setValue('knowledgeTagsInput', (entry.tags || []).join(', '));
    setValue('knowledgeContentInput', entry.content || '');
    renderKnowledgePreview();
    renderKnowledgeCurrentPath();
    setKnowledgeEditorHint('Loaded ' + (entry.path || entry.title || 'knowledge entry') + '.', 'success');
}

function renderKnowledgePreview() {
    var container = document.getElementById('knowledgePreviewContent');
    if (!container) return;
    var content = getRawValue('knowledgeContentInput');
    if (!content.trim()) {
        container.innerHTML = '<div class="knowledge-preview-empty">Nothing to preview yet.</div>';
        return;
    }
    container.innerHTML = '<div class="knowledge-preview-markdown">' + renderMarkdown(content) + '</div>';
}

function renderKnowledgeCurrentPath() {
    var container = document.getElementById('knowledgeCurrentPath');
    if (!container) return;
    var path = normalizeKnowledgePath(getValue('knowledgePathInput'));
    if (!path) {
        container.innerHTML = 'Path: <span class="knowledge-path-empty">new entry</span>';
        return;
    }
    var parts = path.split('/');
    container.innerHTML = 'Path: ' + parts.map(function(part) {
        return '<span class="knowledge-breadcrumb-segment">' + escapeHtml(part) + '</span>';
    }).join('<span class="knowledge-breadcrumb-sep"> / </span>');
}

function renderMemory() {
    var container = document.getElementById('memoryList');
    if (!container) return;
    if (!memoryData || memoryData.length === 0) {
        container.innerHTML = '<div class="empty-state"><div class="empty-state-icon">🧠</div><div class="empty-state-text">No memory entries found</div><div class="empty-state-detail">Create agent, user, or medium memory to populate this view.</div></div>';
        return;
    }

    var groups = {
        agent_memory: { title: 'Agent Memory', items: [] },
        user_memory: { title: 'User Memory', items: [] },
        medium_memory: { title: 'Medium Memory', items: [] }
    };

    memoryData.forEach(function(entry) {
        var category = entry.category || 'agent_memory';
        if (!groups[category]) {
            groups[category] = { title: category.replace(/_/g, ' '), items: [] };
        }
        groups[category].items.push(entry);
    });

    var order = ['agent_memory', 'user_memory', 'medium_memory'];
    container.innerHTML = order.map(function(category) {
        var group = groups[category];
        if (!group || group.items.length === 0) return '';
        return '<section class="knowledge-section">'
            + '<div class="knowledge-section-header">'
            + '<h2 class="knowledge-section-title">' + group.title + '</h2>'
            + '<span class="knowledge-section-count">' + group.items.length + '</span>'
            + '</div>'
            + '<div class="knowledge-section-list">'
            + group.items.map(function(entry) { return renderKnowledgeCard(entry, 'memory'); }).join('')
            + '</div>'
            + '</section>';
    }).join('');
}

function renderKnowledge() {
    renderKnowledgeTree();
    renderKnowledgeTagFilters();
    renderKnowledgeCurrentPath();
    renderKnowledgeDirectoryContext();

    var container = document.getElementById('knowledgeList');
    if (!container) return;

    var filtered = getFilteredKnowledgeEntries();
    if (!filtered.length) {
        container.innerHTML = '<div class="empty-state"><div class="empty-state-icon">📚</div><div class="empty-state-text">No matching knowledge entries</div><div class="empty-state-detail">Try another directory, clear tags, or search with a different keyword.</div></div>';
        return;
    }

    container.innerHTML = filtered.map(function(entry) {
        return renderKnowledgeCard(entry, 'knowledge');
    }).join('');
}

function renderKnowledgeTree() {
    var container = document.getElementById('knowledgeTree');
    if (!container) return;

    var tree = buildKnowledgeDirectoryTree();
    var html = '<div class="knowledge-tree-root">'
        + renderKnowledgeTreeNode('all', 'All Notes', 0, selectedKnowledgeDirectory === 'all', getFilteredCountForDirectory('all'))
        + renderKnowledgeTreeChildren(tree, 0)
        + '</div>';
    container.innerHTML = html;
}

function renderKnowledgeTreeNode(value, label, depth, active, count) {
    return '<button type="button" class="knowledge-tree-node knowledge-tree-depth-' + depth + (active ? ' active' : '') + '" onclick="selectKnowledgeDirectory(\'' + escapeJs(value) + '\')">'
        + '<span class="knowledge-tree-branch" aria-hidden="true"></span>'
        + '<span class="knowledge-tree-label">' + escapeHtml(label) + '</span>'
        + '<span class="knowledge-tree-count">' + count + '</span>'
        + '</button>';
}

function renderKnowledgeTreeChildren(nodes, depth) {
    if (!nodes || !nodes.length) {
        return '';
    }
    return '<div class="knowledge-tree-children">' + nodes.map(function(node) {
        return '<div class="knowledge-tree-item">'
            + renderKnowledgeTreeNode(node.path, node.name, depth + 1, selectedKnowledgeDirectory === node.path, getFilteredCountForDirectory(node.path))
            + renderKnowledgeTreeChildren(node.children, depth + 1)
            + '</div>';
    }).join('') + '</div>';
}

function buildKnowledgeDirectoryTree() {
    var root = [];
    var index = {};
    getKnowledgeDirectories().forEach(function(directory) {
        if (directory === 'root') return;
        var parts = directory.split('/');
        var parentChildren = root;
        var currentPath = '';
        for (var i = 0; i < parts.length; i++) {
            currentPath = currentPath ? currentPath + '/' + parts[i] : parts[i];
            if (!index[currentPath]) {
                index[currentPath] = {
                    name: parts[i],
                    path: currentPath,
                    children: []
                };
                parentChildren.push(index[currentPath]);
            }
            parentChildren = index[currentPath].children;
        }
    });
    sortKnowledgeTree(root);
    return root;
}

function sortKnowledgeTree(nodes) {
    nodes.sort(function(a, b) {
        return a.path.localeCompare(b.path);
    });
    nodes.forEach(function(node) {
        sortKnowledgeTree(node.children);
    });
}

function renderKnowledgeTagFilters() {
    var container = document.getElementById('knowledgeTagFilters');
    if (!container) return;

    var tags = getKnowledgeTags();
    if (!tags.length) {
        container.innerHTML = '<div class="knowledge-filter-empty">No tags yet</div>';
        return;
    }

    var html = [];
    html.push('<button type="button" class="knowledge-tag-chip' + (selectedKnowledgeTags.length === 0 ? ' active' : '') + '" onclick="clearKnowledgeTags()">All</button>');
    tags.forEach(function(tag) {
        var active = selectedKnowledgeTags.indexOf(tag) >= 0;
        html.push('<button type="button" class="knowledge-tag-chip' + (active ? ' active' : '') + '" onclick="toggleKnowledgeTag(\'' + escapeJs(tag) + '\')">' + escapeHtml(tag) + '</button>');
    });
    container.innerHTML = html.join('');
}

function selectKnowledgeDirectory(directory) {
    selectedKnowledgeDirectory = directory || 'all';
    renderKnowledge();
}

function renderKnowledgeDirectoryContext() {
    var container = document.getElementById('knowledgeDirectoryContext');
    if (!container) return;

    var baseDirectory = getKnowledgeDirectoryBase();
    if (baseDirectory === 'root') {
        container.textContent = 'Creating folders at: root';
        return;
    }

    container.textContent = 'Creating folders at: ' + baseDirectory + ' (enter a child name like subfolder)';
}

function toggleKnowledgeTag(tag) {
    var index = selectedKnowledgeTags.indexOf(tag);
    if (index >= 0) {
        selectedKnowledgeTags.splice(index, 1);
    } else {
        selectedKnowledgeTags.push(tag);
        selectedKnowledgeTags.sort();
    }
    renderKnowledge();
}

function clearKnowledgeTags() {
    selectedKnowledgeTags = [];
    renderKnowledge();
}

function getFilteredKnowledgeEntries() {
    return (knowledgeData || []).filter(function(entry) {
        if (selectedKnowledgeDirectory !== 'all') {
            var directory = entry.directory || 'root';
            if (directory !== selectedKnowledgeDirectory && !directory.startsWith(selectedKnowledgeDirectory + '/')) {
                return false;
            }
        }
        if (selectedKnowledgeTags.length > 0) {
            var entryTags = entry.tags || [];
            for (var i = 0; i < selectedKnowledgeTags.length; i++) {
                if (entryTags.indexOf(selectedKnowledgeTags[i]) < 0) {
                    return false;
                }
            }
        }
        return true;
    }).sort(function(a, b) {
        return (a.path || '').localeCompare(b.path || '');
    });
}

function getKnowledgeDirectories() {
    var map = {};
    createdKnowledgeDirectories.forEach(function(directory) {
        map[directory] = true;
    });
    (knowledgeData || []).forEach(function(entry) {
        var directory = entry.directory || 'root';
        map[directory] = true;
    });
    return Object.keys(map).sort();
}

function getKnowledgeTags() {
    var map = {};
    (knowledgeData || []).forEach(function(entry) {
        (entry.tags || []).forEach(function(tag) {
            map[tag] = true;
        });
    });
    return Object.keys(map).sort();
}

function getFilteredCountForDirectory(directory) {
    return (knowledgeData || []).filter(function(entry) {
        var entryDirectory = entry.directory || 'root';
        if (directory !== 'all' && entryDirectory !== directory && !entryDirectory.startsWith(directory + '/')) {
            return false;
        }
        if (selectedKnowledgeTags.length > 0) {
            var entryTags = entry.tags || [];
            for (var i = 0; i < selectedKnowledgeTags.length; i++) {
                if (entryTags.indexOf(selectedKnowledgeTags[i]) < 0) {
                    return false;
                }
            }
        }
        return true;
    }).length;
}

function renderKnowledgeCard(entry, kind) {
    var summary = entry.description || (entry.content ? entry.content.substring(0, 150) : '');
    if (summary && entry.content && summary !== entry.description && entry.content.length > 150) {
        summary += '...';
    }
    var metaParts = [];
    if (kind === 'memory') {
        metaParts.push('Layer: ' + escapeHtml(formatKnowledgeCategory(entry.category || entry.type || 'memory')));
    }
    if (entry.path) {
        metaParts.push('Path: ' + escapeHtml(entry.path));
    }
    if (entry.source) {
        metaParts.push('Source: ' + escapeHtml(entry.source));
    }
    if (entry.directory) {
        metaParts.push('Directory: ' + escapeHtml(entry.directory));
    }
    if (entry.tags && entry.tags.length) {
        metaParts.push('Tags: ' + escapeHtml(entry.tags.join(', ')));
    }
    if (entry.agent_id) {
        metaParts.push('Agent: ' + escapeHtml(entry.agent_id));
    }
    if (entry.user_id) {
        metaParts.push('User: ' + escapeHtml(entry.user_id));
    }

    var activeClass = kind === 'knowledge' && entry.path && entry.path === selectedKnowledgePath ? ' active' : '';
    var onclick = '';
    if (kind === 'knowledge') {
        onclick = "loadKnowledgeEntry('" + escapeJs(entry.path || '') + "')";
    } else {
        onclick = "showKnowledgeDetail('" + escapeJs(kind) + "', '" + escapeJs(entry.id) + "')";
    }

    return '<div class="knowledge-item' + activeClass + '" onclick="' + onclick + '">'
        + '<div class="knowledge-title">' + escapeHtml(entry.title || entry.id) + '</div>'
        + '<div class="knowledge-content">' + escapeHtml(summary || 'No preview available.') + '</div>'
        + '<div class="knowledge-meta">'
        + metaParts.map(function(part) { return '<span>' + part + '</span>'; }).join('')
        + '</div></div>';
}

function formatKnowledgeCategory(category) {
    var map = {
        agent_memory: 'Agent Memory',
        user_memory: 'User Memory',
        medium_memory: 'Medium Memory',
        memory: 'Memory',
        knowledge: 'Knowledge'
    };
    return map[category] || category.replace(/_/g, ' ');
}

function searchMemory() {
    var query = document.getElementById('memorySearch').value;
    addLog('info', 'Searching memory: "' + query + '"');
    fetchMemory(query);
}

function searchKnowledge() {
    var query = document.getElementById('knowledgeSearch').value;
    addLog('info', 'Searching knowledge: "' + query + '"');
    fetchKnowledge(query);
}

function refreshKnowledge() {
    var query = document.getElementById('knowledgeSearch').value;
    addLog('info', 'Refreshing knowledge list...');
    fetchKnowledge(query);
}

function showKnowledgeDetail(kind, id) {
    var source = kind === 'memory' ? memoryData : knowledgeData;
    var entry = source.find(function(e) { return e.id === id; });
    if (!entry) return;

    var metaParts = [];
    metaParts.push('<strong>Type:</strong> ' + escapeHtml(formatKnowledgeCategory(entry.category || entry.type || kind)));
    if (entry.path) metaParts.push('<strong>Path:</strong> ' + escapeHtml(entry.path));
    if (entry.directory) metaParts.push('<strong>Directory:</strong> ' + escapeHtml(entry.directory));
    if (entry.tags && entry.tags.length) metaParts.push('<strong>Tags:</strong> ' + escapeHtml(entry.tags.join(', ')));
    if (entry.source) metaParts.push('<strong>Source:</strong> ' + escapeHtml(entry.source));
    if (entry.agent_id) metaParts.push('<strong>Agent:</strong> ' + escapeHtml(entry.agent_id));
    if (entry.user_id) metaParts.push('<strong>User:</strong> ' + escapeHtml(entry.user_id));

    var modal = document.createElement('div');
    modal.className = 'knowledge-modal-overlay';
    modal.innerHTML = '<div class="knowledge-modal">'
        + '<div class="knowledge-modal-header">'
        + '<h2 class="knowledge-modal-title">' + escapeHtml(entry.title) + '</h2>'
        + '<button onclick="this.closest(\'.knowledge-modal-overlay\').remove()" class="knowledge-modal-close">Close</button>'
        + '</div>'
        + '<div class="knowledge-modal-meta">' + metaParts.join(' | ') + '</div>'
        + '<div class="knowledge-modal-content">' + escapeHtml(entry.content) + '</div>'
        + '</div>';
    document.body.appendChild(modal);
    addLog('info', 'Viewing ' + kind + ': ' + entry.title);
}

function upsertKnowledgeEntry(entry) {
    if (!entry || !entry.path) return;
    var replaced = false;
    knowledgeData = (knowledgeData || []).map(function(item) {
        if (item.path === entry.path) {
            replaced = true;
            return entry;
        }
        return item;
    });
    if (!replaced) {
        knowledgeData.push(entry);
    }
}

function moveKnowledgeEntryState(fromPath, entry) {
    knowledgeData = (knowledgeData || []).filter(function(item) {
        return item.path !== fromPath;
    });
    upsertKnowledgeEntry(entry);
    populateKnowledgeEditor(entry);
    renderKnowledge();
}

function removeKnowledgeEntry(path) {
    knowledgeData = (knowledgeData || []).filter(function(entry) {
        return entry.path !== path;
    });
    if (selectedKnowledgePath === path) {
        resetKnowledgeEditor();
    }
    renderKnowledge();
}

function addKnowledgeDirectory(directory) {
    directory = normalizeKnowledgeDirectory(directory);
    if (!directory) return;
    if (createdKnowledgeDirectories.indexOf(directory) < 0) {
        createdKnowledgeDirectories.push(directory);
        createdKnowledgeDirectories.sort();
    }
    selectedKnowledgeDirectory = directory;
    setValue('knowledgeDirectoryInput', directory);
    renderKnowledge();
}

function removeKnowledgeDirectory(directory) {
    directory = normalizeKnowledgeDirectory(directory);
    if (!directory) return;
    createdKnowledgeDirectories = createdKnowledgeDirectories.filter(function(item) {
        return item !== directory;
    });
    if (selectedKnowledgeDirectory === directory) {
        selectedKnowledgeDirectory = 'all';
    }
    if (getValue('knowledgeDirectoryInput') === directory) {
        setValue('knowledgeDirectoryInput', '');
    }
    renderKnowledge();
}

function findKnowledgeEntryByPath(path) {
    return (knowledgeData || []).find(function(entry) {
        return entry.path === path;
    });
}

function normalizeKnowledgePath(path) {
    var value = (path || '').trim().replace(/\\/g, '/').replace(/^\/+/, '');
    if (!value) return '';
    if (!/\.md$/i.test(value)) {
        value += '.md';
    }
    return value;
}

function normalizeKnowledgeDirectory(path) {
    var value = (path || '').trim().replace(/\\/g, '/').replace(/^\/+/, '').replace(/\/+$/, '');
    return value;
}

function resolveKnowledgeDirectoryInput(path) {
    var value = normalizeKnowledgeDirectory(path);
    if (!value) return '';
    var baseDirectory = getKnowledgeDirectoryBase();
    if (value.indexOf('/') >= 0 || baseDirectory === 'root') {
        return value;
    }
    return baseDirectory + '/' + value;
}

function getKnowledgeDirectoryBase() {
    if (!selectedKnowledgeDirectory || selectedKnowledgeDirectory === 'all' || selectedKnowledgeDirectory === 'root') {
        return 'root';
    }
    return selectedKnowledgeDirectory;
}

function parseTagInput(value) {
    return (value || '').split(',').map(function(tag) {
        return tag.trim();
    }).filter(function(tag) {
        return !!tag;
    });
}

function setKnowledgeEditorHint(message, level) {
    var hint = document.getElementById('knowledgeEditorHint');
    if (!hint) return;
    hint.textContent = message;
    hint.className = 'knowledge-editor-hint' + (level ? ' ' + level : '');
}

function getValue(id) {
    var el = document.getElementById(id);
    return el ? el.value.trim() : '';
}

function getRawValue(id) {
    var el = document.getElementById(id);
    return el ? el.value : '';
}

function setValue(id, value) {
    var el = document.getElementById(id);
    if (el) el.value = value || '';
}
