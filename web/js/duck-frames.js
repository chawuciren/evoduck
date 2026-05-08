// ===== Duck ASCII Art Animation Frames =====
// Each template: { delay: ms_between_frames, frames: string[] }
// Each frame string uses \n line separators.
// Characters: █ ▓ ▒ ░ (block shade sequence)

// ---- Helpers ----
function mirrorFrame(frame) {
  return frame.split('\n').map(function(line) {
    return line.split('').reverse().join('');
  }).join('\n');
}

function indentFrame(frame, n) {
  var pad = new Array(n + 1).join(' ');
  return frame.split('\n').map(function(line) {
    return pad + line;
  }).join('\n');
}

function shiftFrameRight(frame, n) {
  return indentFrame(frame, n);
}

function shiftFrameLeft(frame, n) {
  return frame.split('\n').map(function(line) {
    return line.substring(n);
  }).join('\n');
}

// ---- Base idle frames (referenced by templates) ----
var _idleF0 =
  "                    ░░░░\n" +
  "                ██████████░░\n" +
  "              ██████████████░\n" +
  "            ████  ██  ████████░\n" +
  "            ████  ██  ██████████  ██░░\n" +
  "        ░░██████████████████████ ██▓▓\n" +
  "      ░░██████████████▓▓██████▓▓\n" +
  "      ███████████████▓▓▓▓██████░\n" +
  "      ████████████▓▓▓▓▓▓████░░\n" +
  "        ████████▓▓▓▓▓▓██░░\n" +
  "          ░░██      ██\n" +
  "            ██      ██";

var _idleF1 =
  "\n" +
  "                    ░░░░\n" +
  "                ██████████░░\n" +
  "              ██████████████░\n" +
  "            ████  ██  ████████░\n" +
  "            ████  ██  ██████████  ██░░\n" +
  "        ░░██████████████████████ ██▓▓\n" +
  "      ░░██████████████▓▓██████▓▓\n" +
  "      ███████████████▓▓▓▓██████░\n" +
  "      ████████████▓▓▓▓▓▓████░░\n" +
  "        ████████▓▓▓▓▓▓██░░\n" +
  "          ░░██      ██\n";

// ---- walkRight frames (referenced by walkLeft below) ----
var _walkRightFrames = [
  "                      ░░░░\n" +
  "                  ██████████░░\n" +
  "                ██████████████░\n" +
  "              ████  ██  ████████░\n" +
  "              ████  ██  ██████████  ██░░\n" +
  "          ░░██████████████████████ ██▓▓\n" +
  "        ░░██████████████▓▓██████▓▓\n" +
  "        ███████████████▓▓▓▓██████░\n" +
  "        ████████████▓▓▓▓▓▓████░░\n" +
  "          ████████▓▓▓▓▓▓██░░\n" +
  "            ░░██      ██\n" +
  "          ██      ██",

  "                      ░░░░\n" +
  "                  ██████████░░\n" +
  "                ██████████████░\n" +
  "              ████  ██  ████████░\n" +
  "              ████  ██  ██████████  ██░░\n" +
  "          ░░██████████████████████ ██▓▓\n" +
  "        ░░██████████████▓▓██████▓▓\n" +
  "        ███████████████▓▓▓▓██████░\n" +
  "        ████████████▓▓▓▓▓▓████░░\n" +
  "          ████████▓▓▓▓▓▓██░░\n" +
  "          ░░██      ██\n" +
  "              ██      ██",

  "                        ░░░░\n" +
  "                    ██████████░░\n" +
  "                  ██████████████░\n" +
  "                ████  ██  ████████░\n" +
  "                ████  ██  ██████████  ██░░\n" +
  "            ░░██████████████████████ ██▓▓\n" +
  "          ░░██████████████▓▓██████▓▓\n" +
  "          ███████████████▓▓▓▓██████░\n" +
  "          ████████████▓▓▓▓▓▓████░░\n" +
  "            ████████▓▓▓▓▓▓██░░\n" +
  "              ░░██      ██\n" +
  "            ██      ██",

  "                        ░░░░\n" +
  "                    ██████████░░\n" +
  "                  ██████████████░\n" +
  "                ████  ██  ████████░\n" +
  "                ████  ██  ██████████  ██░░\n" +
  "            ░░██████████████████████ ██▓▓\n" +
  "          ░░██████████████▓▓██████▓▓\n" +
  "          ███████████████▓▓▓▓██████░\n" +
  "          ████████████▓▓▓▓▓▓████░░\n" +
  "            ████████▓▓▓▓▓▓██░░\n" +
  "            ░░██      ██\n" +
  "                ██      ██"
];

// ---- Build mirrored walkLeft frames (duck faces RIGHT, walks left) ----
var _walkLeftFrames = (function() {
  var indents = [22, 22, 20, 20];
  return _walkRightFrames.map(function(fr, i) {
    return indentFrame(mirrorFrame(fr), indents[i]);
  });
})();

var DUCK_TEMPLATES = {

  // ---- Idle (subtle bob) ----
  idle: {
    delay: 600,
    frames: [_idleF0, _idleF1]
  },

  // ---- Walk Left (duck faces RIGHT, walks left) ----
  walkLeft: {
    delay: 200,
    frames: _walkLeftFrames
  },

  // ---- Walk Right (duck faces left, walks right) ----
  walkRight: {
    delay: 200,
    frames: _walkRightFrames
  },

  // ---- Happy (bounce + music notes) ----
  happy: {
    delay: 350,
    frames: [
      "         ♪                 ♪\n" + _idleF0,
      "                  ♪    ♪\n" + _idleF0
    ]
  },

  // ---- Sad (tears + droop) ----
  sad: {
    delay: 800,
    frames: [
      "      ···                ···\n" + _idleF0,
      "               ···  ···\n" + _idleF1
    ]
  },

  // ---- Surprised (exclamation + big jump) ----
  surprised: {
    delay: 250,
    frames: [
      "        !                  !\n" + _idleF0,
      "                 !    !\n" +
      "                    ░░░░\n" +
      "                ██████████░░\n" +
      "              ██████████████░\n" +
      "            ████  ██  ████████░\n" +
      "            ████  ██  ██████████  ██░░\n" +
      "        ░░██████████████████████ ██▓▓\n" +
      "      ░░██████████████▓▓██████▓▓\n" +
      "      ███████████████▓▓▓▓██████░\n" +
      "      ████████████▓▓▓▓▓▓████░░\n" +
      "        ████████▓▓▓▓▓▓██░░\n" +
      "          ░░██      ██\n" +
      "            ██      ██"
    ]
  },

  // ---- Love (floating hearts) ----
  love: {
    delay: 500,
    frames: [
      "  ♥\n" + _idleF0,
      "\n" + _idleF0 + "  ♥"
    ]
  },

  // ---- Angry (steam + jitter) ----
  angry: {
    delay: 180,
    frames: [
      "        ~     ~     ~\n" + shiftFrameRight(_idleF0, 0),
      "          ~     ~     ~\n" + shiftFrameRight(_idleF0, 1)
    ]
  }
};

// EVODUCK brand text (static, non-animated)
var EVODUCK_ASCII =
  "████████╗██╗   ██╗ ██████╗ ██████╗ ██╗   ██╗ ██████╗██╗  ██╗\n" +
  "██╔════╝██░   ██░██╔═══██╗██╔══██╗██░   ██░██╔════╝██░ ██╔╝\n" +
  "█████╗  ██░   ██░██░   ██░██░  ██░██░   ██░██░     █████╔╝\n" +
  "██╔══╝  ╚██╗ ██╔╝██░   ██░██░  ██░██░   ██░██░     ██╔══██╗\n" +
  "███████╗ ╚████╔╝ ╚██████╔╝██████╔╝╚██████╔╝╚██████╗██░  ██╗\n" +
  "╚══════╝  ╚═══╝   ╚═════╝ ╚═════╝  ╚═════╝  ╚══════╝╚═╝  ╚═╝\n" +
  " ░░░░░░    ░░░     ░░░░░   ░░░░░    ░░░░░    ░░░░░  ░░   ░░ ";
