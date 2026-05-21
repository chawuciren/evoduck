/**
 * Final focused test: Using .bat files (which cmd CAN execute directly).
 * This isolates the QUOTING issue from the "not a valid exe" issue.
 */
const { spawnSync, execSync } = require('child_process');
const path = require('path');
const fs = require('fs');
const os = require('os');

const dir = path.join(os.tmpdir(), 'evo_q_' + Date.now());
fs.mkdirSync(dir, { recursive: true });

// Create .bat files in paths with spaces (like real Windows apps)
const appBat = path.join(dir, 'My Tools', 'app.bat');
fs.mkdirSync(path.dirname(appBat), { recursive: true });
fs.writeFileSync(appBat, '@echo off\r\necho OK args=[%*]\r\n');

function test(label, fn) {
  try {
    const r = fn();
    const ok = r.status === 0 && !r.error;
    console.log(`${ok ? '✓' : '✗'} ${label}`);
    if (r.stdout?.trim()) console.log(`  → ${r.stdout.trim()}`);
    if (!ok) console.log(`  → ${(r.stderr || r.error?.message || '').trim().substring(0, 130)}`);
  } catch (e) {
    console.log(`✗ ${label}\n  → ${(e.stderr || e.message || '').trim().substring(0, 130)}`);
  }
}

console.log('╔══════════════════════════════════════════════════════════╗');
console.log('║     Simulating LLM → Go exec/process → cmd /c           ║');
console.log('╚══════════════════════════════════════════════════════════╝');
console.log(`\nApp path: ${appBat}\n`);

// Simulates LLM generating: "C:\...\My Tools\app.bat" --flag "file name.txt"
const llmCmd = `"${appBat}" --flag "a file.txt"`;

console.log('=== Simulated Go exec.Command("cmd", "/c", command) ===\n');
console.log(`LLM command: ${llmCmd}`);

// This is what Go's exec.go:157 does
test('BUG: cmd /c with LLM-quoted path', () =>
  spawnSync('cmd', ['/c', llmCmd], { encoding: 'utf8', timeout: 5000, windowsHide: true })
);

console.log('\n=== Fix attempts via spawn (Go-compatible) ===\n');

// Strip LLM quotes — but path has spaces...
test('Strip quotes (path has spaces → breaks)', () =>
  spawnSync('cmd', ['/c', `${appBat} --flag "a file.txt"`], { encoding: 'utf8', timeout: 5000, windowsHide: true })
);

// Split into exe + args manually — Go would do exec.Command(exe, args...)
console.log('\n=== Direct execution (no cmd /c) ===\n');

test('execSync (Node internal shell)', () => {
  const r = execSync(`"${appBat}" --flag "a file.txt"`, { encoding: 'utf8', timeout: 5000, windowsHide: true });
  return { status: 0, stdout: r, error: null };
});

test('spawn .bat directly (shell:true)', () =>
  spawnSync(appBat, ['--flag', 'a file.txt'], { encoding: 'utf8', timeout: 5000, windowsHide: true, shell: true })
);

// PowerShell
console.log('\n=== PowerShell (works for all cases) ===\n');

test('powershell -Command', () =>
  spawnSync('powershell', ['-Command', `& "${appBat}" --flag "a file.txt"`], { encoding: 'utf8', timeout: 5000, windowsHide: true })
);

// Cleanup
try { fs.rmSync(dir, { recursive: true }); } catch (_) {}
