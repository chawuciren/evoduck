// ===== Slash Command Autocomplete =====

// Available commands (keep in sync with backend builtin.go)
var AVAILABLE_COMMANDS = [
    { name: 'help', desc: '显示所有可用命令', usage: '/help [命令名]', icon: '❓' },
    { name: 'status', desc: '显示当前会话状态', usage: '/status', icon: '📊' },
    { name: 'new', desc: '开始新会话，清空历史', usage: '/new', icon: '🆕' },
    { name: 'reset', desc: '重置会话，清空历史 (同 /new)', usage: '/reset', icon: '🔄' },
    { name: 'resume', desc: '查看或恢复历史会话', usage: '/resume [next|prev|list <页>|<id>]', icon: '📚' },
    { name: 'history', desc: '历史会话归档（/resume 的别名）', usage: '/history [next|prev|list <页>|<id>]', icon: '📜' },
    { name: 'model', desc: '显示当前使用的模型', usage: '/model', icon: '🤖' },
    { name: 'models', desc: '显示所有可用模型列表', usage: '/models', icon: '📋', role: 'employee' },
    { name: 'agent', desc: '切换到指定 Agent', usage: '/agent <agent_id>', icon: '🦾' },
    { name: 'agents', desc: '显示所有可用 Agent', usage: '/agents', icon: '👥' },
    { name: 'export', desc: '导出当前会话为 JSON', usage: '/export', icon: '📤' },
    { name: 'compress', desc: '手动触发会话压缩', usage: '/compress', icon: '📦' },
    { name: 'logs', desc: '查看系统日志', usage: '/logs [level] [limit]', icon: '📝', role: 'admin' },
    { name: 'memory', desc: '显示记忆系统状态', usage: '/memory', icon: '🧠', role: 'employee' },
    { name: 'schedule', desc: '管理用户定时任务', usage: '/schedule [list|enable <id>|disable <id>|delete <id>]', icon: '⏰', role: 'employee' },
    { name: 'mcp', desc: 'Show MCP server status or reconnect', usage: '/mcp [status|reconnect [name]]', icon: '🔌', role: 'employee' }
];

var selectedCommandIndex = -1;
var filteredCommands = [];

function handleInputChange(event) {
    var value = event.target.value;
    if (value.startsWith('/')) {
        var query = value.substring(1).toLowerCase();
        filteredCommands = AVAILABLE_COMMANDS.filter(function(cmd) {
            return cmd.name.toLowerCase().includes(query) || cmd.desc.toLowerCase().includes(query);
        });
        showCommandDropdown(filteredCommands);
    } else {
        hideCommandDropdown();
    }
    if (typeof updateSendButton === 'function') updateSendButton();
}

function handleInputKeydown(event) {
    var dropdown = document.getElementById('commandDropdown');
    if (!dropdown || dropdown.style.display === 'none') return;

    switch (event.key) {
        case 'ArrowDown':
            event.preventDefault();
            navigateCommand(1);
            break;
        case 'ArrowUp':
            event.preventDefault();
            navigateCommand(-1);
            break;
        case 'Tab':
            event.preventDefault();
            if (filteredCommands.length > 0) {
                selectCommand(selectedCommandIndex >= 0 ? selectedCommandIndex : 0);
            }
            break;
        case 'Escape':
            event.preventDefault();
            hideCommandDropdown();
            break;
        case 'Enter':
            if (filteredCommands.length > 0) {
                event.preventDefault();
                selectCommand(selectedCommandIndex >= 0 ? selectedCommandIndex : 0);
            }
            break;
    }
}

function showCommandDropdown(commands) {
    var dropdown = document.getElementById('commandDropdown');
    if (!dropdown) return;

    if (commands.length === 0) {
        dropdown.innerHTML = '<div class="command-dropdown-empty">No commands found</div>';
        dropdown.style.display = 'block';
        selectedCommandIndex = -1;
        return;
    }

    var html = '<div class="command-dropdown-header">Commands</div>';
    commands.forEach(function(cmd, index) {
        var roleBadge = cmd.role
            ? '<span style="font-size:10px;padding:2px 6px;background:rgba(59,130,246,0.2);border-radius:4px;margin-left:8px;color:#94A3B8;">' + cmd.role + '</span>'
            : '';
        html += '<div class="command-item' + (index === selectedCommandIndex ? ' selected' : '') + '" '
            + 'data-index="' + index + '" '
            + 'onclick="selectCommand(' + index + ')" '
            + 'onmouseover="highlightCommand(' + index + ')">'
            + '<div class="command-icon">' + cmd.icon + '</div>'
            + '<div class="command-info">'
            + '<div class="command-name">/' + cmd.name + roleBadge + '</div>'
            + '<div class="command-desc">' + cmd.desc + '</div>'
            + '</div>'
            + '<div class="command-usage">' + cmd.usage + '</div>'
            + '</div>';
    });

    dropdown.innerHTML = html;
    dropdown.style.display = 'block';
    // 默认选中第一项，方便直接回车补全
    selectedCommandIndex = 0;
    updateCommandHighlight();
}

function hideCommandDropdown() {
    var dropdown = document.getElementById('commandDropdown');
    if (dropdown) dropdown.style.display = 'none';
    selectedCommandIndex = -1;
    filteredCommands = [];
}

function toggleCommandDropdown() {
    var dropdown = document.getElementById('commandDropdown');
    if (dropdown && dropdown.style.display === 'block') {
        hideCommandDropdown();
    } else {
        filteredCommands = AVAILABLE_COMMANDS.slice();
        showCommandDropdown(filteredCommands);
        var input = document.getElementById('messageInput');
        if (input) input.focus();
    }
}

function navigateCommand(direction) {
    if (filteredCommands.length === 0) return;
    if (direction === 1) {
        selectedCommandIndex = (selectedCommandIndex + 1) % filteredCommands.length;
    } else {
        selectedCommandIndex = selectedCommandIndex <= 0 ? filteredCommands.length - 1 : selectedCommandIndex - 1;
    }
    updateCommandHighlight();
}

function highlightCommand(index) {
    selectedCommandIndex = index;
    updateCommandHighlight();
}

function updateCommandHighlight() {
    document.querySelectorAll('.command-item').forEach(function(item, index) {
        if (index === selectedCommandIndex) {
            item.classList.add('selected');
            item.scrollIntoView({ block: 'nearest' });
        } else {
            item.classList.remove('selected');
        }
    });
}

function selectCommand(index) {
    if (index < 0 || index >= filteredCommands.length) index = 0;
    var cmd = filteredCommands[index];
    if (!cmd) return;

    var input = document.getElementById('messageInput');
    input.value = '/' + cmd.name + ' ';
    input.focus();
    input.setSelectionRange(input.value.length, input.value.length);
    hideCommandDropdown();
    if (typeof updateSendButton === 'function') updateSendButton();
}

// Click outside to close dropdown
document.addEventListener('click', function(event) {
    var input = document.getElementById('messageInput');
    var dropdown = document.getElementById('commandDropdown');
    var cmdBtn = document.getElementById('cmdBtn');
    if (input && dropdown && !input.contains(event.target) && !dropdown.contains(event.target) && cmdBtn && !cmdBtn.contains(event.target)) {
        hideCommandDropdown();
    }
});
