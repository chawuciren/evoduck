function DuckAnimator(duckContainerId, textContainerId, animationData) {
  this.duckEl = document.getElementById(duckContainerId);
  this.textEl = document.getElementById(textContainerId);
  this.frameTimer = null;
  this.switchTimer = null;
  this.data = null;
  this.animationMap = {};
  this.currentAnimation = '';
  this.frameIndex = 0;

  if (animationData) {
    this.setData(animationData);
  }
}

DuckAnimator.prototype.setData = function(animationData) {
  var animations = animationData && Array.isArray(animationData.animations) ? animationData.animations : [];
  var animationMap = {};

  for (var i = 0; i < animations.length; i++) {
    var animation = animations[i];
    if (!animation || !animation.name || !Array.isArray(animation.frames) || !animation.frames.length) {
      continue;
    }
    animationMap[animation.name] = animation;
  }

  this.data = {
    version: animationData && animationData.version ? animationData.version : 1,
    defaultAnimation: animationData && animationData.defaultAnimation ? animationData.defaultAnimation : '',
    logoAscii: animationData && animationData.logoAscii ? animationData.logoAscii : '',
    switching: {
      minDelayMs: animationData && animationData.switching && typeof animationData.switching.minDelayMs === 'number' ? animationData.switching.minDelayMs : 8000,
      maxDelayMs: animationData && animationData.switching && typeof animationData.switching.maxDelayMs === 'number' ? animationData.switching.maxDelayMs : 15000
    },
    animations: animations
  };
  this.animationMap = animationMap;

  if (!this.animationMap[this.currentAnimation]) {
    this.currentAnimation = this.data.defaultAnimation && this.animationMap[this.data.defaultAnimation]
      ? this.data.defaultAnimation
      : Object.keys(this.animationMap)[0] || '';
    this.frameIndex = 0;
  }
};

DuckAnimator.prototype.loadAnimation = function(name) {
  if (!this.animationMap[name]) return false;
  this.currentAnimation = name;
  this.frameIndex = 0;
  return true;
};

DuckAnimator.prototype.start = function() {
  if (!this.data || !this.currentAnimation) return;
  this.stop();
  this.renderText();
  this.renderDuck();
  this._scheduleNextFrame();
  this._scheduleSwitch();
};

DuckAnimator.prototype.stop = function() {
  if (this.frameTimer) {
    clearTimeout(this.frameTimer);
    this.frameTimer = null;
  }
  if (this.switchTimer) {
    clearTimeout(this.switchTimer);
    this.switchTimer = null;
  }
};

DuckAnimator.prototype._getCurrentAnimation = function() {
  return this.animationMap[this.currentAnimation] || null;
};

DuckAnimator.prototype._scheduleNextFrame = function() {
  var self = this;
  var animation = this._getCurrentAnimation();
  if (!animation || !animation.frames.length) return;

  var frame = animation.frames[this.frameIndex];
  var holdMs = frame && typeof frame.holdMs === 'number' ? frame.holdMs : 500;

  this.frameTimer = setTimeout(function() {
    var nextAnimation = self._getCurrentAnimation();
    if (!nextAnimation || !nextAnimation.frames.length) return;

    self.frameIndex++;
    if (self.frameIndex >= nextAnimation.frames.length) {
      self.frameIndex = nextAnimation.loop === false ? nextAnimation.frames.length - 1 : 0;
    }

    self.renderDuck();
    self._scheduleNextFrame();
  }, holdMs);
};

DuckAnimator.prototype._scheduleSwitch = function() {
  var self = this;
  if (!this.data) return;

  var minDelayMs = this.data.switching.minDelayMs;
  var maxDelayMs = this.data.switching.maxDelayMs;
  if (maxDelayMs < minDelayMs) {
    maxDelayMs = minDelayMs;
  }

  var delay = minDelayMs;
  if (maxDelayMs > minDelayMs) {
    delay += Math.random() * (maxDelayMs - minDelayMs);
  }

  this.switchTimer = setTimeout(function() {
    var next = self._pickNextAnimation();
    if (!next) return;
    if (self.frameTimer) {
      clearTimeout(self.frameTimer);
      self.frameTimer = null;
    }
    self.loadAnimation(next);
    self.renderDuck();
    self._scheduleNextFrame();
    self._scheduleSwitch();
  }, delay);
};

DuckAnimator.prototype._pickNextAnimation = function() {
  var names = Object.keys(this.animationMap);
  if (!names.length) return '';
  if (names.length === 1) return names[0];

  var candidates = [];
  var totalWeight = 0;

  for (var i = 0; i < names.length; i++) {
    var animation = this.animationMap[names[i]];
    if (!animation || animation.name === this.currentAnimation) {
      continue;
    }

    var weight = typeof animation.weight === 'number' && animation.weight > 0 ? animation.weight : 1;
    candidates.push({ name: animation.name, weight: weight });
    totalWeight += weight;
  }

  if (!candidates.length) return this.currentAnimation;

  var roll = Math.random() * totalWeight;
  for (var j = 0; j < candidates.length; j++) {
    roll -= candidates[j].weight;
    if (roll <= 0) {
      return candidates[j].name;
    }
  }

  return candidates[candidates.length - 1].name;
};

DuckAnimator.prototype.renderDuck = function() {
  if (!this.duckEl) return;

  var animation = this._getCurrentAnimation();
  if (!animation || !animation.frames.length) return;

  var frame = animation.frames[this.frameIndex];
  if (!frame || typeof frame.art !== 'string') return;

  this.duckEl.textContent = frame.art;
};

DuckAnimator.prototype.renderText = function() {
  if (!this.textEl || !this.data) return;
  this.textEl.innerHTML = colorizeEvoduckText(this.data.logoAscii || '');
};

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
    while (j < line.length && getEvoduckCharColor(line[j]) === color) {
      j++;
    }
    var segment = line.substring(i, j);
    if (color) {
      result += '<span style="color:' + color + '">' + escapeHtml(segment) + '</span>';
    } else {
      result += escapeHtml(segment);
    }
    i = j;
  }
  return result;
}

function getEvoduckCharColor(ch) {
  if ('╗╔╝╚║═╖╓╜╟╠╣╦╩'.indexOf(ch) >= 0) {
    return '#56f7b1';
  }
  if (ch === '█') return '#35d6ff';
  if (ch === '░') return 'rgba(53,214,255,0.15)';
  return null;
}
