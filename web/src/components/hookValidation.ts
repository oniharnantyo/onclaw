export function validateRegex(pattern: string): { ok: boolean; error?: string; warn?: string } {
  if (!pattern) return { ok: true };
  try {
    new RegExp(pattern);
  } catch (err: any) {
    return { ok: false, error: err.message };
  }

  if (
    pattern.includes('(?=') ||
    pattern.includes('(?!') ||
    pattern.includes('(?<=') ||
    pattern.includes('(?<!') ||
    /\\[1-9]/.test(pattern)
  ) {
    return {
      ok: true,
      warn: "Pattern uses lookahead, lookbehind, or backreferences which Go's RE2 engine does not support."
    };
  }

  return { ok: true };
}

export function validateCommand(cmd: string): { ok: boolean; error?: string; warn?: string } {
  const trimmed = cmd.trim();
  if (!trimmed) {
    return { ok: false, error: 'Command cannot be empty' };
  }

  let singleQuotes = 0;
  let doubleQuotes = 0;
  let backticks = 0;

  for (let i = 0; i < trimmed.length; i++) {
    const char = trimmed[i];
    if (char === "'") singleQuotes++;
    else if (char === '"') doubleQuotes++;
    else if (char === '`') backticks++;
  }

  if (singleQuotes % 2 !== 0) {
    return { ok: false, error: "Unbalanced single quotes (')" };
  }
  if (doubleQuotes % 2 !== 0) {
    return { ok: false, error: 'Unbalanced double quotes (")' };
  }
  if (backticks % 2 !== 0) {
    return { ok: false, error: 'Unbalanced backticks (`)' };
  }

  return { ok: true };
}

export function validateScript(src: string): { ok: boolean; error?: string; warn?: string } {
  if (!src.trim()) {
    return { ok: false, error: 'Script source cannot be empty' };
  }

  try {
    new Function(src);
  } catch (err: any) {
    return { ok: false, error: err.message };
  }

  const hasHandle = 
    /\bfunction\s+handle\s*\([^)]*\)/.test(src) || 
    /\bhandle\s*=\s*function\b/.test(src) || 
    /\bhandle\s*=\s*(?:\([^)]*\)|[a-zA-Z_$][\w$]*)\s*=>/.test(src) || 
    /\bhandle\s*\([^)]*\)\s*\{/.test(src) || 
    /\bhandle\s*:\s*/.test(src);

  if (!hasHandle) {
    return { ok: false, error: 'Script must define a "handle(ctx)" function' };
  }

  return { ok: true };
}

export function validateTimeout(ms: any): { ok: boolean; error?: string; warn?: string } {
  const val = Number(ms);
  if (isNaN(val) || !Number.isInteger(val) || val < 1 || val > 10000) {
    return { ok: false, error: 'Timeout must be an integer between 1 and 10000' };
  }
  return { ok: true };
}
