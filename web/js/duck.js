// ===== Duck ASCII Art Animator =====

function DuckAnimator(duckContainerId, textContainerId) {
  this.duckEl = document.getElementById(duckContainerId);
  this.textEl = document.getElementById(textContainerId);
  this.currentTemplate = 'idle';
  this.frameIndex = 0;
  this.timer = null;
  this.switchTimer = null;
}

DuckAnimator.prototype.loadTemplate = function(name) {
  if (!DUCK_TEMPLATES[name]) return;
  this.currentTemplate = name;
  this.frameIndex = 0;
};

DuckAnimator.prototype.start = function() {
  this.stop();
  this.renderDuck();
  this.renderText();
  this._tick();
  this._scheduleSwitch();
};

DuckAnimator.prototype.stop = function() {
  if (this.timer) { clearInterval(this.timer); this.timer = null; }
  if (this.switchTimer) { clearTimeout(this.switchTimer); this.switchTimer = null; }
};

DuckAnimator.prototype._tick = function() {
  var self = this;
  this.timer = setInterval(function() {
    var tmpl = DUCK_TEMPLATES[self.currentTemplate];
    if (!tmpl) return;
    self.frameIndex = (self.frameIndex + 1) % tmpl.frames.length;
    self.renderDuck();
  }, DUCK_TEMPLATES[this.currentTemplate].delay);
};

DuckAnimator.prototype._scheduleSwitch = function() {
  var self = this;
  var delay = 8000 + Math.random() * 7000;
  this.switchTimer = setTimeout(function() {
    var names = Object.keys(DUCK_TEMPLATES);
    var others = names.filter(function(n) { return n !== self.currentTemplate; });
    if (!others.length) return;
    var next = others[Math.floor(Math.random() * others.length)];
    self.loadTemplate(next);
    self.start();
  }, delay);
};

// ---- Render duck frame as plain text (color via CSS) ----
DuckAnimator.prototype.renderDuck = function() {
  if (!this.duckEl) return;
  var tmpl = DUCK_TEMPLATES[this.currentTemplate];
  if (!tmpl || !tmpl.frames[this.frameIndex]) return;
  this.duckEl.textContent = tmpl.frames[this.frameIndex];
};

// ---- Render EVODUCK text into colored HTML (static) ----
DuckAnimator.prototype.renderText = function() {
  if (!this.textEl) return;
  this.textEl.innerHTML = colorizeEvoduckText(EVODUCK_ASCII);
};

// EVODUCK text: box-drawing → green, blocks → cyan, shadow ░ → dim
function colorizeEvoduckText(text) {
  var lines = text.split('\n');
  var out = [];
  for (var i = 0; i < lines.length; i++) {
    out.push(colorizeEvoduckLine(lines[i]));
  }
  return out.join('\n');
}

function colorizeEvoduckLine(line) {
  if (!line) return '';
  var result = '';
  var i = 0;
  while (i < line.length) {
    var ch = line[i];
    var color = getEvoduckCharColor(ch);
    var j = i;
    while (j < line.length && getEvoduckCharColor(line[j]) === color) { j++; }
    var segment = line.substring(i, j);
    if (color) {
      result += '<span style="color:' + color + '">' + escapeHtml(segment) + '</span>';
    } else {
      result += segment;
    }
    i = j;
  }
  return result;
}

function getEvoduckCharColor(ch) {
  // Box-drawing chars → green accent
  if ('╗╔╝╚║═╖╓╜╟╠╣╦╩'.indexOf(ch) >= 0) {
    return '#56f7b1';
  }
  // Full blocks → cyan
  if (ch === '█') return '#35d6ff';
  // Shadow chars → dim
  if (ch === '░') return 'rgba(53,214,255,0.15)';
  // Other box-drawing variants
  if (ch === '╗' || ch === '╔' || ch === '╝' || ch === '╚' ||
      ch === '║' || ch === '═') return '#56f7b1';
  return null;
}
