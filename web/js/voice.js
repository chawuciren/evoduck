// ===== Voice: Web Speech API TTS (语音播报) + ASR (语音输入) =====
// 纯前端实现，零额外开销。
// - TTS: window.speechSynthesis（系统内置引擎，全平台可用，离线）
// - ASR: SpeechRecognition / webkitSpeechRecognition（Chrome/Edge/Android 走浏览器云端；iOS Safari 不支持）
//
// 已知陷阱（已在代码中处理）：
//   1. iOS Safari 截断约 200 字 → 分块朗读（每块 ≤ MAX_CHUNK）
//   2. Chrome 15 秒自动暂停 bug → keep-alive 定时器 pause+resume
//   3. voices 异步加载 → 监听 voiceschanged + 兜底轮询
//   4. iOS 首次 speak 需用户手势 → TTS 开关的 click 即手势，开启时先播一句提示
//   5. Safari < 16.4 不支持正则 lookbehind → 分句采用手动字符遍历
//   6. ASR 非纯离线（Google 云端）→ tooltip 声明 + network 错误提示

// ---------- State ----------
var ttsEnabled = false;
var ttsQueue = [];
var ttsSpeaking = false;
var ttsVoice = null;
var ttsKeepAliveTimer = null;
var ttsVoicesReady = false;

var asrRecognition = null;
var asrListening = false;

var TTS_MAX_CHUNK = 200;          // 单次朗读最大字符（iOS 截断阈值）
var TTS_KEEPALIVE_INTERVAL = 10000; // Chrome 15 秒暂停 bug，10 秒续命

// ---------- Init ----------
function voiceInit() {
    initTTS();
    initASR();
}

// ==================== TTS ====================
function initTTS() {
    if (!window.speechSynthesis) {
        var btn = document.getElementById('ttsBtn');
        if (btn) btn.style.display = 'none';
        addLog('warn', 'speechSynthesis not available, TTS disabled');
        return;
    }
    // 持久化状态
    ttsEnabled = localStorage.getItem('evoduck_tts') === '1';
    // voices 异步加载
    loadVoices();
    if (typeof window.speechSynthesis.onvoiceschange !== 'undefined') {
        window.speechSynthesis.onvoiceschange = loadVoices;
    }
    // 兜底：部分浏览器不触发 voiceschanged，延迟再试一次
    setTimeout(loadVoices, 300);
    setTimeout(loadVoices, 1000);
    updateTTSButton();
}

function loadVoices() {
    if (!window.speechSynthesis) return;
    var voices = window.speechSynthesis.getVoices() || [];
    if (!voices.length) return;
    ttsVoicesReady = true;
    // 优先级：zh-CN > 其他 zh* > 默认
    ttsVoice =
        voices.filter(function (v) { return v.lang === 'zh-CN'; })[0] ||
        voices.filter(function (v) { return v.lang && v.lang.indexOf('zh') === 0; })[0] ||
        null;
    if (!ttsVoice) {
        addLog('warn', 'No Chinese TTS voice found; fallback to default voice');
    }
}

function toggleTTS() {
    if (!window.speechSynthesis) return;
    ttsEnabled = !ttsEnabled;
    localStorage.setItem('evoduck_tts', ttsEnabled ? '1' : '0');
    updateTTSButton();
    if (ttsEnabled) {
        // iOS：此处 click 即用户手势，立即播一句短提示建立 audio context
        speakText('语音播报已开启');
    } else {
        stopSpeaking();
    }
}

function updateTTSButton() {
    var btn = document.getElementById('ttsBtn');
    if (!btn) return;
    btn.textContent = ttsEnabled ? '🔊' : '🔇';
    btn.classList.toggle('active', ttsEnabled);
    btn.title = ttsEnabled ? '语音播报：开（点击关闭）' : '语音播报：关（点击开启）';
}

// 朗读任意文本（入队）
function speakText(text, force) {
    if (!window.speechSynthesis) return;
    if (!force && !ttsEnabled) return;
    text = (text || '').replace(/\s+/g, ' ').trim();
    if (!text) return;
    var chunks = splitSentences(text, TTS_MAX_CHUNK);
    for (var i = 0; i < chunks.length; i++) {
        if (chunks[i]) ttsQueue.push(chunks[i]);
    }
    if (!ttsSpeaking) playNext();
}

// 单条消息的播报按钮：无论全局 TTS 开关是否打开，点一下就朗读这条
function speakOnDemand(text) {
    if (!window.speechSynthesis) return;
    stopSpeaking();
    speakText(text, true);
}

function playNext() {
    if (!window.speechSynthesis) return;
    // 关闭或队列空 → 结束
    if (!ttsEnabled || ttsQueue.length === 0) {
        ttsSpeaking = false;
        stopKeepAlive();
        return;
    }
    var chunk = ttsQueue.shift();
    var u = new SpeechSynthesisUtterance(chunk);
    u.lang = 'zh-CN';
    if (ttsVoice) u.voice = ttsVoice;
    u.rate = 1.0;
    u.onend = function () { playNext(); };
    u.onerror = function () { playNext(); };
    ttsSpeaking = true;
    startKeepAlive();
    window.speechSynthesis.speak(u);
}

function stopSpeaking() {
    if (!window.speechSynthesis) return;
    ttsQueue = [];
    ttsSpeaking = false;
    stopKeepAlive();
    try { window.speechSynthesis.cancel(); } catch (e) {}
}

// Chrome 已知 bug：连续朗读超过 ~15s 会自动暂停。每 10s pause+resume 续命。
function startKeepAlive() {
    stopKeepAlive();
    ttsKeepAliveTimer = setInterval(function () {
        if (!window.speechSynthesis) { stopKeepAlive(); return; }
        if (window.speechSynthesis.speaking) {
            try {
                window.speechSynthesis.pause();
                window.speechSynthesis.resume();
            } catch (e) {}
        } else {
            stopKeepAlive();
        }
    }, TTS_KEEPALIVE_INTERVAL);
}
function stopKeepAlive() {
    if (ttsKeepAliveTimer) {
        clearInterval(ttsKeepAliveTimer);
        ttsKeepAliveTimer = null;
    }
}

// 手动字符遍历分句（兼容 Safari<16.4，不用 lookbehind）
// 按句末标点（。！？!?；;\n）切分，超长块硬切。
function splitSentences(text, maxLen) {
    var result = [];
    var buf = '';
    var ENDERS = '。！？!?；;\n';
    function flush(str) {
        str = str.trim();
        if (!str) return;
        if (str.length <= maxLen) { result.push(str); return; }
        for (var i = 0; i < str.length; i += maxLen) result.push(str.slice(i, i + maxLen));
    }
    for (var i = 0; i < text.length; i++) {
        buf += text.charAt(i);
        if (ENDERS.indexOf(text.charAt(i)) >= 0) {
            flush(buf);
            buf = '';
        }
    }
    flush(buf);
    return result;
}

// agent 回复完成时：从流式消息 DOM 提取纯文本并朗读
function speakReplyFromStream() {
    if (!ttsEnabled) return;
    var text = '';
    if (typeof currentStreamMessage !== 'undefined' && currentStreamMessage && currentStreamMessage.contentSpan) {
        text = currentStreamMessage.contentSpan.innerText;
    } else if (typeof streamRawContent !== 'undefined' && streamRawContent) {
        text = streamRawContent;
    }
    if (text && text.trim()) speakText(text);
}

// ==================== ASR ====================
function initASR() {
    var ASR = window.SpeechRecognition || window.webkitSpeechRecognition;
    var micBtn = document.getElementById('micBtn');
    if (!micBtn) return;
    if (!ASR) {
        // iOS Safari 等不支持 → 隐藏按钮，不显示不可用功能
        micBtn.style.display = 'none';
        return;
    }
    micBtn.title = '语音输入（需联网，由浏览器云端识别）';
    updateMicButton();
}

function toggleASR() {
    var ASR = window.SpeechRecognition || window.webkitSpeechRecognition;
    if (!ASR) {
        addSystemMessage('🎤 当前浏览器不支持语音识别（iOS Safari 暂不支持）');
        return;
    }
    if (asrListening) {
        if (asrRecognition) { try { asrRecognition.stop(); } catch (e) {} }
        return;
    }
    try {
        asrRecognition = new ASR();
    } catch (e) {
        addSystemMessage('🎤 无法启动语音识别: ' + e.message);
        return;
    }
    asrRecognition.lang = 'zh-CN';
    asrRecognition.continuous = false;     // 单次识别，避免长时资源消耗
    asrRecognition.interimResults = true;  // 实时显示中间结果
    asrRecognition.maxAlternatives = 1;

    // 识别开始：记录已有输入作为前缀，便于累加
    asrRecognition.onstart = function () {
        var input = document.getElementById('messageInput');
        if (input) input.dataset.asrPrefix = input.value || '';
        asrListening = true;
        updateMicButton();
    };

    asrRecognition.onresult = function (event) {
        var input = document.getElementById('messageInput');
        if (!input) return;
        var prefix = input.dataset.asrPrefix || '';
        var finalText = '';
        var interim = '';
        for (var i = event.resultIndex; i < event.results.length; i++) {
            var r = event.results[i];
            if (r.isFinal) finalText += r[0].transcript;
            else interim += r[0].transcript;
        }
        input.value = prefix + finalText + interim;
        // 触发现有输入事件链（更新发送按钮状态等）
        if (typeof handleInputChange === 'function') handleInputChange({ target: input });
        if (finalText) input.dataset.asrPrefix = prefix + finalText;
    };

    asrRecognition.onend = function () {
        asrListening = false;
        updateMicButton();
        var input = document.getElementById('messageInput');
        if (input) delete input.dataset.asrPrefix;
        // 不自动发送：用户可核对/编辑后手动发送
    };

    asrRecognition.onerror = function (event) {
        asrListening = false;
        updateMicButton();
        var err = event.error || '';
        if (err === 'not-allowed' || err === 'service-not-allowed') {
            addSystemMessage('🎤 需要麦克风权限才能使用语音输入');
        } else if (err === 'network') {
            addSystemMessage('🎤 语音识别需要联网（浏览器云端服务）');
        } else if (err === 'no-speech') {
            // 静默：用户没说话
        } else if (err) {
            addSystemMessage('🎤 语音识别错误: ' + err);
        }
    };

    try {
        asrRecognition.start();
    } catch (e) {
        addSystemMessage('🎤 启动语音识别失败: ' + e.message);
        asrListening = false;
        updateMicButton();
    }
}

function updateMicButton() {
    var btn = document.getElementById('micBtn');
    if (!btn) return;
    btn.textContent = asrListening ? '⏹' : '🎤';
    btn.classList.toggle('recording', asrListening);
    if (asrListening) btn.title = '停止语音输入';
}
