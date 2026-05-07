// ===== Agents Page =====

function fetchAgents() {
    if (!sendWSRequest('get_agents')) renderAgents();
}

function renderAgents() {
    var container = document.getElementById('agentsList');
    if (!container) return;
    var sorted = sortAgents(agentsData || []);
    if (!sorted || sorted.length === 0) {
        container.innerHTML = '<div class="empty-state"><div class="empty-state-icon">🤖</div><div class="empty-state-text">No agents configured</div><div class="empty-state-detail">Register an agent runtime to populate the registry.</div></div>';
        return;
    }
    container.innerHTML = sorted.map(function(agent) {
        var avatar = agent.role === 'employee' ? '👔' : (agent.role === 'admin' ? '👨\u200d💼' : '👤');
        return '<div class="agent-card" onclick="switchToAgent(\'' + agent.id + '\')">'
            + '<div class="agent-header">'
            + '<div class="agent-avatar">' + avatar + '</div>'
            + '<div class="agent-info"><h3>' + agent.id + '</h3><span>' + agent.model + '</span></div>'
            + '</div>'
            + '<div class="card-content"><strong>Role:</strong> ' + agent.role + '<br>'
            + '<strong>Channels:</strong> ' + (agent.channels ? agent.channels.join(', ') : 'none') + '</div>'
            + '<div class="agent-status">'
            + '<span class="status-badge ' + agent.status + '">' + agent.status + '</span>'
            + '<button class="action-btn action-btn-inline">Chat</button>'
            + '</div></div>';
    }).join('');
}

function refreshAgents() {
    addLog('info', 'Refreshing agents list...');
    fetchAgents();
}

function sortAgents(agents) {
    var roleOrder = { 'admin': 0, 'employee': 1, 'customer': 2 };
    return agents.slice().sort(function(a, b) {
        var ao = roleOrder[a.role] !== undefined ? roleOrder[a.role] : 99;
        var bo = roleOrder[b.role] !== undefined ? roleOrder[b.role] : 99;
        if (ao !== bo) return ao - bo;
        return a.id.localeCompare(b.id);
    });
}

function updateAgentSelector() {
    var selector = document.getElementById('agentSelect');
    if (!selector) return;
    var sorted = sortAgents(agentsData || []);
    selector.innerHTML = '';
    sorted.forEach(function(agent) {
        var option = document.createElement('option');
        option.value = agent.id;
        option.textContent = agent.id + ' (' + agent.role + ')';
        if (currentAgentId === agent.id) option.selected = true;
        selector.appendChild(option);
    });
}
