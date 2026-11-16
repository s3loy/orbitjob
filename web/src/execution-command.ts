export type ExecutionCommand = { command: string[]; args: string[] };
export type ExecutionCommandInput = { text: string; error: string; value: ExecutionCommand };
export type ExecutionScriptMethod = 'shell' | 'python' | 'javascript';
export type ExecutionMethod = 'image' | 'executable' | ExecutionScriptMethod;
export type ShellExecutable = 'sh' | '/bin/sh';

export type ShellScript = {
  executable: ShellExecutable;
  location: 'command' | 'args';
  text: string;
};

export function readShellScript(value: ExecutionCommand): ShellScript | undefined {
  const executable = value.command[0];
  if ((executable !== 'sh' && executable !== '/bin/sh') || value.command[1] !== '-c')
    return undefined;
  return value.command.length > 2
    ? { executable, location: 'command', text: value.command[2] }
    : { executable, location: 'args', text: value.args[0] ?? '' };
}

export type ExecutionScript = {
  method: ExecutionScriptMethod;
  executable: string;
  option: '-c' | '-e';
  location: 'command' | 'args';
  index: number;
  text: string;
};

export function readExecutionScript(value: ExecutionCommand): ExecutionScript | undefined {
  const shell = readShellScript(value);
  if (shell)
    return {
      ...shell,
      method: 'shell',
      option: '-c',
      index: shell.location === 'command' ? 2 : 0,
    };

  const executable = value.command[0];
  let method: 'python' | 'javascript';
  let option: '-c' | '-e';
  if (
    ['python', 'python3', '/usr/local/bin/python', '/usr/local/bin/python3'].includes(executable)
  ) {
    method = 'python';
    option = '-c';
  } else if (executable === 'node' || executable === '/usr/local/bin/node') {
    method = 'javascript';
    option = '-e';
  } else return undefined;

  if (value.command[1] === option)
    return value.command.length > 2
      ? { method, executable, option, location: 'command', index: 2, text: value.command[2] }
      : { method, executable, option, location: 'args', index: 0, text: value.args[0] ?? '' };
  return undefined;
}

export function executionMethod(value: ExecutionCommand): ExecutionMethod {
  if (!value.command.length) return 'image';
  return readExecutionScript(value)?.method ?? 'executable';
}

export function replaceShellScript(value: ExecutionCommand, text: string): ExecutionCommand {
  const script = readShellScript(value);
  if (!script) return value;
  return script.location === 'command'
    ? { command: [...value.command.slice(0, 2), text, ...value.command.slice(3)], args: value.args }
    : { command: value.command, args: [text, ...value.args.slice(1)] };
}

export function replaceExecutionScript(value: ExecutionCommand, text: string): ExecutionCommand {
  const script = readExecutionScript(value);
  if (!script) return value;
  if (script.location === 'command')
    return {
      command: [
        ...value.command.slice(0, script.index),
        text,
        ...value.command.slice(script.index + 1),
      ],
      args: value.args,
    };
  return {
    command: value.command,
    args: [...value.args.slice(0, script.index), text, ...value.args.slice(script.index + 1)],
  };
}

export function executionArguments(value: ExecutionCommand): string[] {
  const script = readExecutionScript(value);
  return script?.location === 'args' ? value.args.slice(script.index + 1) : value.args;
}

export function replaceExecutionArguments(
  value: ExecutionCommand,
  args: string[],
): ExecutionCommand {
  const script = readExecutionScript(value);
  return {
    command: value.command,
    args:
      script?.location === 'args'
        ? [...value.args.slice(0, script.index), script.text, ...args]
        : args,
  };
}

export type ExecutionCommandEditor = {
  value: ExecutionCommand;
  drafts: Partial<Record<ExecutionMethod, ExecutionCommand>>;
};

function copy(value: ExecutionCommand): ExecutionCommand {
  return { command: [...value.command], args: [...value.args] };
}

function sameValues(left: string[], right: string[]): boolean {
  return left.length === right.length && left.every((value, index) => value === right[index]);
}

export function executionCommandInputError(
  input: ExecutionCommandInput | undefined,
  value: ExecutionCommand,
): string {
  const missingExecutable =
    value.command.length > 0 && value.command[0] === ''
      ? 'Enter a command, or choose Use image default.'
      : '';
  const script = readExecutionScript(value);
  const missingScript =
    script && !script.text.trim()
      ? `Enter ${script.method === 'shell' ? 'a Shell script' : script.method === 'python' ? 'a Python script' : 'JavaScript code'} before continuing.`
      : '';
  return input &&
    sameValues(input.value.command, value.command) &&
    sameValues(input.value.args, value.args)
    ? input.error || missingExecutable || missingScript
    : missingExecutable || missingScript;
}

export function formatExecutionCommandLine(value: ExecutionCommand): string {
  return [...value.command, ...value.args]
    .map((token) =>
      /^[a-zA-Z0-9_./:@%+=,-]+$/.test(token) ? token : `'${token.replaceAll("'", "'\\''")}'`,
    )
    .join(' ');
}

export function parseExecutionCommandLine(text: string): {
  value: ExecutionCommand;
  error: string;
} {
  const tokens: string[] = [];
  let token = '';
  let started = false;
  let quote: 'single' | 'double' | undefined;
  const invalid = (error: string) => ({ value: { command: [], args: [] }, error });

  for (let index = 0; index < text.length; index++) {
    const character = text[index];
    if (quote === 'single') {
      if (character === "'") quote = undefined;
      else token += character;
      continue;
    }
    if (character === '\\') {
      const next = text[++index];
      if (next === undefined) return invalid('Finish the escape with a character after \\.');
      if (!quote && (next === '\n' || next === '\r'))
        return invalid('Use Shell script for multiple commands or line continuations.');
      token += next;
      started = true;
      continue;
    }
    if (quote === 'double') {
      if (character === '"') quote = undefined;
      else token += character;
      continue;
    }
    if (character === "'" || character === '"') {
      quote = character === "'" ? 'single' : 'double';
      started = true;
      continue;
    }
    if (character === '\n' || character === '\r')
      return invalid('Use Shell script for multiple commands.');
    if (';|&<>`'.includes(character) || (character === '$' && text[index + 1] === '('))
      return invalid('Use Shell script for shell operators, or quote this value as one argument.');
    if (/\s/.test(character)) {
      if (started) {
        tokens.push(token);
        token = '';
        started = false;
      }
      continue;
    }
    token += character;
    started = true;
  }

  if (quote) return invalid(`Close the ${quote} quote to finish this command.`);
  if (started) tokens.push(token);
  if (!tokens.length) return invalid('Enter a command, or choose Use image default.');
  return {
    value: { command: [tokens[0]], args: tokens.slice(1) },
    error: tokens[0] ? '' : 'Enter a command, or choose Use image default.',
  };
}

export function createExecutionCommandEditor(value: ExecutionCommand): ExecutionCommandEditor {
  const current = copy(value);
  return { value: current, drafts: { [executionMethod(current)]: current } };
}

export function reconcileExecutionCommandEditor(
  editor: ExecutionCommandEditor,
  value: ExecutionCommand,
): ExecutionCommandEditor {
  return sameValues(editor.value.command, value.command) &&
    sameValues(editor.value.args, value.args)
    ? editor
    : createExecutionCommandEditor(value);
}

export function updateExecutionCommandEditor(
  editor: ExecutionCommandEditor,
  value: ExecutionCommand,
): ExecutionCommandEditor {
  const current = copy(value);
  return { value: current, drafts: { ...editor.drafts, [executionMethod(current)]: current } };
}

export function selectExecutionMethod(
  editor: ExecutionCommandEditor,
  method: ExecutionMethod,
): ExecutionCommandEditor {
  if (executionMethod(editor.value) === method) return editor;
  const saved = editor.drafts[method];
  if (saved) return updateExecutionCommandEditor(editor, saved);
  const args = readExecutionScript(editor.value) ? [] : editor.value.args;
  const value: ExecutionCommand =
    method === 'shell' || method === 'python' || method === 'javascript'
      ? {
          command: [
            method === 'shell' ? 'sh' : method === 'python' ? 'python' : 'node',
            method === 'javascript' ? '-e' : '-c',
          ],
          args: [''],
        }
      : { command: method === 'image' ? [] : [''], args };
  return updateExecutionCommandEditor(editor, value);
}
