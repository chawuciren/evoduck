// ===== Skills Page =====

var selectedSkillName = '';

function fetchSkills() {
    if (!sendWSRequest('get_skills')) renderSkills();
}

function renderSkills() {
    var container = document.getElementById('skillsList');
    if (!container) return;
    if (!selectedSkillName || !(skillsData || []).some(function(skill) { return skill.name === selectedSkillName; })) {
        selectedSkillName = skillsData && skillsData.length ? skillsData[0].name : '';
    }
    if (!skillsData || skillsData.length === 0) {
        container.innerHTML = '<div class="empty-state"><div class="empty-state-icon">\uD83D\uDD27</div><div class="empty-state-text">No skills loaded</div><div class="empty-state-detail">Attach a module pack to expose reusable runtime actions.</div></div>';
        renderSkillPreview('');
        return;
    }
    container.innerHTML = skillsData.map(function(skill) {
        var tagsHtml = '';
        if (skill.tags) skill.tags.forEach(function(tag) { tagsHtml += '<span class="skill-tag">' + escapeHtml(tag) + '</span>'; });
        if (skill.role) tagsHtml += '<span class="skill-tag">role: ' + escapeHtml(skill.role) + '</span>';
        return '<button class="skill-card' + (skill.name === selectedSkillName ? ' active' : '') + '" type="button" onclick="selectSkill(\'' + escapeJs(skill.name) + '\')">'
            + '<div class="skill-icon">\u26A1</div>'
            + '<div class="skill-name">' + escapeHtml(skill.name) + '</div>'
            + '<div class="skill-desc">' + escapeHtml(skill.description || 'No description') + '</div>'
            + '<div class="skill-tags">' + tagsHtml + '</div>'
            + '</button>';
    }).join('');

    renderSkillPreview(selectedSkillName);
    if (selectedSkillName && !skillDetailsData[selectedSkillName]) {
        requestSkillDetail(selectedSkillName);
    }
}

function refreshSkills() {
    addLog('info', 'Refreshing skills list...');
    skillDetailsData = {};
    fetchSkills();
}

function selectSkill(name) {
    selectedSkillName = name;
    renderSkills();
    if (!skillDetailsData[name]) {
        requestSkillDetail(name);
    }
}

function requestSkillDetail(name) {
    if (!name) return;
    renderSkillPreview(name, true);
    if (!sendWSRequest('get_skill_detail', { name: name })) {
        renderSkillPreview(name);
    }
}

function renderSkillPreview(name, loading) {
    var panel = document.getElementById('skillPreviewPanel');
    if (!panel) return;

    if (!name) {
        panel.innerHTML = '<div class="skill-preview-empty"><div class="empty-state-icon">\uD83D\uDD0D</div><div class="empty-state-text">Select a skill</div><div class="empty-state-detail">Choose a module from the list to inspect its parameters and content.</div></div>';
        return;
    }

    var detail = skillDetailsData[name];
    if (!detail) {
        panel.innerHTML = '<div class="skill-preview-loading"><div class="skill-preview-kicker">Skill Preview</div><div class="skill-preview-title">' + escapeHtml(name) + '</div><div class="skill-preview-text">' + (loading ? 'Loading skill details...' : 'Skill details are not available right now.') + '</div></div>';
        return;
    }

    var tagsHtml = '';
    (detail.tags || []).forEach(function(tag) {
        tagsHtml += '<span class="skill-tag">' + escapeHtml(tag) + '</span>';
    });
    if (detail.role) {
        tagsHtml += '<span class="skill-tag">role: ' + escapeHtml(detail.role) + '</span>';
    }
    if (detail.parameters && detail.parameters.length) {
        tagsHtml += '<span class="skill-tag">params: ' + detail.parameters.length + '</span>';
    }

    var paramsHtml = '<div class="skill-meta-empty">No parameters required</div>';
    if (detail.parameters && detail.parameters.length) {
        paramsHtml = detail.parameters.map(function(param) {
            var meta = [];
            meta.push(param.required ? 'required' : 'optional');
            if (param.default) meta.push('default: ' + param.default);
            return '<div class="skill-param-row">'
                + '<div class="skill-param-name">' + escapeHtml(param.name || '') + '</div>'
                + '<div class="skill-param-meta">' + escapeHtml(meta.join(' · ')) + '</div>'
                + '<div class="skill-param-desc">' + escapeHtml(param.description || 'No description') + '</div>'
                + '</div>';
        }).join('');
    }

    var content = detail.content || '';
    panel.innerHTML = '<div class="skill-preview-shell">'
        + '<div class="skill-preview-header">'
        + '<div class="skill-preview-kicker">Skill Preview</div>'
        + '<div class="skill-preview-title-row"><div class="skill-preview-title">' + escapeHtml(detail.name) + '</div><button class="action-btn skill-copy-btn" type="button" onclick="copySkillName(\'' + escapeJs(detail.name) + '\')">Copy Name</button></div>'
        + '<div class="skill-preview-text">' + escapeHtml(detail.description || 'No description') + '</div>'
        + '<div class="skill-tags skill-preview-tags">' + tagsHtml + '</div>'
        + '</div>'
        + '<div class="skill-preview-section"><div class="skill-preview-section-title">Source</div><div class="skill-source-path">' + escapeHtml(detail.location || 'Unknown') + '</div></div>'
        + '<div class="skill-preview-section"><div class="skill-preview-section-title">Parameters</div><div class="skill-params-list">' + paramsHtml + '</div></div>'
        + '<div class="skill-preview-section"><div class="skill-preview-section-title">Content</div><pre class="skill-content-preview">' + escapeHtml(content || 'No content') + '</pre></div>'
        + '</div>';
}

function copySkillName(name) {
    if (!navigator.clipboard || !navigator.clipboard.writeText) return;
    navigator.clipboard.writeText(name).then(function() {
        addLog('info', 'Copied skill name: ' + name);
    }).catch(function() {
        addLog('warn', 'Failed to copy skill name: ' + name);
    });
}

function escapeHtml(value) {
    return String(value || '')
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}
