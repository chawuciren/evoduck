// ===== Shared Utilities =====

var WEBCAT_EXPLICIT_SESSION_KEY = '';

function escapeJs(value) {
    return String(value || '').replace(/\\/g, '\\\\').replace(/'/g, "\\'");
}
