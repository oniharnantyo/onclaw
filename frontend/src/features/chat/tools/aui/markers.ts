/**
 * eino's execute output parser (task 4.1, design D7): the combined text
 * stream ends in known marker lines — a failed exit code, a timeout, a size
 * truncation, and the no-output note. The parser strips ANSI escape
 * sequences defensively (the jailed shell has no TTY, but a child process
 * may still emit them), consumes the trailing marker lines, and reports
 * them as structured flags so the terminal block never duplicates the
 * failure note in its body (spec chat-tool-cards "Shell timeout and
 * truncation are distinct states").
 */

/** The parsed shape of one execute result. */
export interface ExecuteOutput {
  /** The output lines with the consumed marker lines and ANSI escapes gone. */
  lines: string[];
  /** The failed exit code carried by the failure marker; absent on success. */
  exitCode?: number;
  /** The command was stopped for exceeding its time limit. */
  timedOut: boolean;
  /** The output was cut for size. */
  truncated: boolean;
  /** The command ran successfully and produced no output. */
  noOutput: boolean;
}

/** ANSI escape sequences: CSI sequences and OSC strings (BEL- and ST-
 * terminated). Stripped defensively — the shell runs without a TTY. */
const ANSI_PATTERN =
  /\u001B(?:\[[0-?]*[ -/]*[@-~]|\][^\u0007\u001B]*(?:\u0007|\u001B\\))/g;

/** `[Command failed with exit code N]` */
const FAILED_MARKER = /^\[Command failed with exit code (\d+)\]$/;

/** `[Command timed out and was stopped]` */
const TIMED_OUT_MARKER = "[Command timed out and was stopped]";

/** `[Output was truncated due to size limits]` */
const TRUNCATED_MARKER = "[Output was truncated due to size limits]";

/** `[Command executed successfully with no output]` */
const NO_OUTPUT_MARKER = "[Command executed successfully with no output]";

/** The marker a trailing line matches, with its exit code when it is the
 * failed-exit one. */
function markerOf(line: string): { exitCode?: number } | null {
  const failed = FAILED_MARKER.exec(line);

  if (failed !== null) {
    return { exitCode: Number(failed[1]) };
  }

  if (
    line === TIMED_OUT_MARKER ||
    line === TRUNCATED_MARKER ||
    line === NO_OUTPUT_MARKER
  ) {
    return {};
  }

  return null;
}

/**
 * Parses an execute result: ANSI-stripped output lines with the trailing
 * marker lines consumed into the structured flags. A result with no markers
 * passes through untouched; an empty result yields no lines and no flags.
 */
export function parseExecuteOutput(text: string): ExecuteOutput {
  const output: ExecuteOutput = {
    lines: [],
    timedOut: false,
    truncated: false,
    noOutput: false,
  };

  if (text === "") return output;

  const raw = text.replace(/\n$/, "").split("\n");

  const stripped = raw.map((line: string): string =>
    line.replace(ANSI_PATTERN, "")
  );

  // Consume the trailing marker lines (in practice at most one of each).
  let end = stripped.length;

  while (end > 0) {
    const marker = markerOf(stripped[end - 1]);

    if (marker === null) break;

    if (marker.exitCode !== undefined) output.exitCode = marker.exitCode;
    else if (stripped[end - 1] === TIMED_OUT_MARKER) output.timedOut = true;
    else if (stripped[end - 1] === TRUNCATED_MARKER) output.truncated = true;
    else output.noOutput = true;

    end -= 1;
  }

  output.lines = stripped.slice(0, end);

  return output;
}
