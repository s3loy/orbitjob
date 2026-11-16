import { describe, expect, it } from 'vitest';
import {
  createExecutionCommandEditor,
  executionArguments,
  executionCommandInputError,
  executionMethod,
  formatExecutionCommandLine,
  parseExecutionCommandLine,
  readExecutionScript,
  readShellScript,
  reconcileExecutionCommandEditor,
  replaceExecutionArguments,
  replaceExecutionScript,
  replaceShellScript,
  selectExecutionMethod,
  updateExecutionCommandEditor,
} from './execution-command';

describe('combined command input', () => {
  it('requires a nonempty explicit program even without a raw command draft', () => {
    expect(executionCommandInputError(undefined, { command: [''], args: [] })).toContain(
      'Enter a command',
    );
    expect(executionCommandInputError(undefined, { command: ['', '-u'], args: [''] })).toContain(
      'Enter a command',
    );
    expect(executionCommandInputError(undefined, { command: [], args: [''] })).toBe('');
    expect(executionCommandInputError(undefined, { command: ['echo'], args: [''] })).toBe('');
  });

  it('splits a command and quoted or escaped arguments without adding a shell', () => {
    expect(
      parseExecutionCommandLine(`python report.py --title "two words" '' escaped\\ value a"b c"d`),
    ).toEqual({
      value: {
        command: ['python'],
        args: ['report.py', '--title', 'two words', '', 'escaped value', 'ab cd'],
      },
      error: '',
    });
  });

  it('roundtrips imported literal tokens, including empty arguments and Kubernetes references', () => {
    const value = {
      command: ['path with spaces/program'],
      args: [
        '',
        'two words',
        'single\' and "double" quotes',
        'C:\\path\\file',
        'line 1\nline 2',
        'carriage\rreturn',
        '; | && > < `echo hello` $(echo hello)',
        '$(VAR_NAME)',
        '$HOME',
        '*.json',
        '\\',
        "'",
      ],
    };
    const formatted = formatExecutionCommandLine(value);
    expect(formatted).toContain("'$(VAR_NAME)'");
    expect(parseExecutionCommandLine(formatted)).toEqual({ value, error: '' });
  });

  it('formats extra command tokens in order without changing their stored placement', () => {
    const value = { command: ['python', '-u', ''], args: ['script.py', 'two words'] };
    const before = structuredClone(value);
    expect(parseExecutionCommandLine(formatExecutionCommandLine(value))).toEqual({
      value: { command: ['python'], args: ['-u', '', 'script.py', 'two words'] },
      error: '',
    });
    expect(value).toEqual(before);
  });

  it.each([
    'echo hello; echo next',
    'echo hello | sort',
    'echo hello && echo next',
    'echo hello &',
    'echo hello > out',
    'cat < input',
    'echo `date`',
    'echo $(date)',
    'echo $(VAR_NAME)',
    'echo one\necho two',
    'echo one\r\necho two',
    'echo one\\\necho two',
  ])('offers Shell script for unquoted shell syntax: %s', (text) => {
    expect(parseExecutionCommandLine(text).error).toContain('Use Shell script');
  });

  it('preserves quoted and escaped shell-looking arguments as literal tokens', () => {
    expect(
      parseExecutionCommandLine('echo ";" \'|\' "$(VAR_NAME)" "`date`" \\> \\$\\(date\\)'),
    ).toEqual({
      value: { command: ['echo'], args: [';', '|', '$(VAR_NAME)', '`date`', '>', '$(date)'] },
      error: '',
    });
    expect(parseExecutionCommandLine("echo 'first\nsecond'").value.args).toEqual(['first\nsecond']);
    expect(parseExecutionCommandLine('echo "first\nsecond"').value.args).toEqual(['first\nsecond']);
  });

  it('does not expand environment references or filename patterns', () => {
    expect(parseExecutionCommandLine('echo $HOME *.json ~/file').value).toEqual({
      command: ['echo'],
      args: ['$HOME', '*.json', '~/file'],
    });
  });

  it.each([
    ['echo "unfinished', 'Close the double quote'],
    ["echo 'unfinished", 'Close the single quote'],
    ['echo trailing\\', 'Finish the escape'],
    ['', 'Enter a command'],
    ['  \t ', 'Enter a command'],
    ["'' argument", 'Enter a command'],
    ['""', 'Enter a command'],
  ])('reports unfinished or empty input: %s', (text, error) => {
    expect(parseExecutionCommandLine(text).error).toContain(error);
  });

  it('applies a pending input error only to the exact command and arguments it describes', () => {
    const value = { command: ['echo'], args: ['hello', ''] };
    const input = { text: 'echo "', error: 'Close the double quote.', value };
    expect(executionCommandInputError(input, structuredClone(value))).toBe(input.error);
    expect(executionCommandInputError(input, { command: ['echo'], args: ['hello'] })).toBe('');
    expect(executionCommandInputError(input, { command: ['other'], args: value.args })).toBe('');
    expect(executionCommandInputError(undefined, value)).toBe('');
    expect(executionCommandInputError({ ...input, error: '' }, value)).toBe('');
  });
});

describe('execution command semantics', () => {
  it('keeps a literal executable and every argument without parsing shell syntax', () => {
    const value = {
      command: ['echo hello', '', 'extra command token'],
      args: ['', 'two words', '$HOME', '"quoted"', 'line 1\nline 2'],
    };
    const editor = createExecutionCommandEditor(value);
    expect(executionMethod(editor.value)).toBe('executable');
    expect(readShellScript(editor.value)).toBeUndefined();
    expect(editor.value).toEqual(value);
    expect(executionArguments(editor.value)).toEqual(value.args);
  });

  it('keeps image defaults distinct from an explicitly empty executable token', () => {
    expect(executionMethod({ command: [], args: [''] })).toBe('image');
    expect(executionMethod({ command: [''], args: [] })).toBe('executable');
  });

  it.each(['sh', '/bin/sh'])(
    'edits a %s script in args without changing its command layout',
    (sh) => {
      const value = { command: [sh, '-c'], args: ['', 'job-name', '', 'two words', '$HOME'] };
      const changed = replaceShellScript(value, 'printf "%s\\n" "$1" | sort');
      expect(changed.command).toEqual([sh, '-c']);
      expect(changed.args).toEqual(['printf "%s\\n" "$1" | sort', ...value.args.slice(1)]);
      expect(executionArguments(changed)).toEqual(value.args.slice(1));
      expect(replaceExecutionArguments(changed, ['', 'new value']).args).toEqual([
        changed.args[0],
        '',
        'new value',
      ]);
      expect(value.args[0]).toBe('');
    },
  );

  it('preserves a script and shell parameters stored in command ahead of args', () => {
    const value = {
      command: ['/bin/sh', '-c', 'printf "%s" "$1"', '', 'from command'],
      args: ['', 'from args'],
    };
    expect(readShellScript(value)?.text).toBe(value.command[2]);
    expect(executionArguments(value)).toEqual(value.args);
    expect(replaceShellScript(value, 'echo "$@"')).toEqual({
      command: ['/bin/sh', '-c', 'echo "$@"', '', 'from command'],
      args: value.args,
    });
    expect(replaceExecutionArguments(value, ['replacement'])).toEqual({
      command: value.command,
      args: ['replacement'],
    });
  });

  it('does not treat another shell option or a shell-looking argument as a script', () => {
    expect(readShellScript({ command: ['sh', '-lc'], args: ['echo hello'] })).toBeUndefined();
    expect(
      readShellScript({ command: ['python'], args: ['sh', '-c', 'echo hello'] }),
    ).toBeUndefined();
  });

  it('inserts an explicit empty script before a new shell parameter when none exists', () => {
    expect(replaceExecutionArguments({ command: ['sh', '-c'], args: [] }, ['name'])).toEqual({
      command: ['sh', '-c'],
      args: ['', 'name'],
    });
  });
});

describe('execution method drafts', () => {
  it('restores exact typed content after selecting another execution method', () => {
    const original = { command: ['python', '-u', ''], args: ['report.py', '', 'two words'] };
    const executable = createExecutionCommandEditor(original);
    const shell = selectExecutionMethod(executable, 'shell');
    expect(shell.value).toEqual({ command: ['sh', '-c'], args: [''] });
    const script = updateExecutionCommandEditor(shell, {
      command: ['/bin/sh', '-c'],
      args: ['echo "$1"', '', 'kept'],
    });
    const image = selectExecutionMethod(script, 'image');
    expect(image.value).toEqual({ command: [], args: [] });
    const restoredExecutable = selectExecutionMethod(image, 'executable');
    expect(restoredExecutable.value).toEqual(original);
    expect(selectExecutionMethod(restoredExecutable, 'shell').value).toEqual(script.value);
  });

  it('carries image argument overrides into the first custom executable draft', () => {
    const image = createExecutionCommandEditor({
      command: [],
      args: ['--config', 'two words', ''],
    });
    expect(selectExecutionMethod(image, 'executable').value).toEqual({
      command: [''],
      args: ['--config', 'two words', ''],
    });
  });

  it('accepts a parent replacement and forgets drafts retained for the previous source', () => {
    const previous = selectExecutionMethod(
      createExecutionCommandEditor({ command: ['old program'], args: ['old argument'] }),
      'shell',
    );
    const incoming = { command: ['/bin/sh', '-c', 'echo new', ''], args: ['new argument', ''] };
    const replaced = reconcileExecutionCommandEditor(previous, incoming);
    expect(replaced.value).toEqual(incoming);
    expect(readShellScript(replaced.value)?.text).toBe('echo new');
    expect(selectExecutionMethod(replaced, 'executable').value).toEqual({
      command: [''],
      args: [],
    });
  });

  it('keeps retained method drafts when the parent echoes the same tokens in new arrays', () => {
    const original = { command: ['echo'], args: ['hello', ''] };
    const shell = selectExecutionMethod(createExecutionCommandEditor(original), 'shell');
    const echoed = reconcileExecutionCommandEditor(shell, {
      command: [...shell.value.command],
      args: [...shell.value.args],
    });
    expect(selectExecutionMethod(echoed, 'executable').value).toEqual(original);
  });
});

describe('interpreter script editing', () => {
  it.each([
    ['python', '-c', 'python'],
    ['python3', '-c', 'python'],
    ['/usr/local/bin/python', '-c', 'python'],
    ['/usr/local/bin/python3', '-c', 'python'],
    ['node', '-e', 'javascript'],
    ['/usr/local/bin/node', '-e', 'javascript'],
  ])(
    'edits %s code stored in command without moving positional arguments',
    (executable, option, method) => {
      const value = {
        command: [executable, option, 'old code', '', 'from command'],
        args: ['from args', ''],
      };
      expect(executionMethod(value)).toBe(method);
      expect(readShellScript(value)).toBeUndefined();
      expect(readExecutionScript(value)?.text).toBe('old code');
      expect(replaceExecutionScript(value, 'line one\nline two')).toEqual({
        command: [executable, option, 'line one\nline two', '', 'from command'],
        args: ['from args', ''],
      });
      expect(executionArguments(value)).toEqual(['from args', '']);
      expect(replaceExecutionArguments(value, ['replacement'])).toEqual({
        command: value.command,
        args: ['replacement'],
      });
      expect(value.command[2]).toBe('old code');
    },
  );

  it.each([
    ['python', '-c'],
    ['node', '-e'],
  ])(
    'keeps %s invocations with an option in args in Command mode while typing',
    (executable, option) => {
      const value = { command: [executable], args: [option, 'old code', '', 'two words'] };
      expect(executionMethod(value)).toBe('executable');
      expect(readExecutionScript(value)).toBeUndefined();
      expect(replaceExecutionScript(value, 'new code')).toBe(value);
      expect(executionArguments(value)).toEqual(value.args);
      expect(replaceExecutionArguments(value, ['next', ''])).toEqual({
        command: [executable],
        args: ['next', ''],
      });
      expect(
        replaceExecutionArguments({ command: [executable], args: [option] }, ['first']),
      ).toEqual({
        command: [executable],
        args: ['first'],
      });
    },
  );

  it.each([
    ['python', '-c'],
    ['node', '-e'],
  ])(
    'keeps %s script and positional values separate when code starts in args',
    (executable, option) => {
      const value = { command: [executable, option], args: ['old code', '', 'two words'] };
      expect(replaceExecutionScript(value, 'new code')).toEqual({
        command: [executable, option],
        args: ['new code', '', 'two words'],
      });
      expect(executionArguments(value)).toEqual(['', 'two words']);
      expect(replaceExecutionArguments(value, ['replacement'])).toEqual({
        command: [executable, option],
        args: ['old code', 'replacement'],
      });
    },
  );

  it.each([
    { command: ['python', '-u', '-c'], args: ['print(1)'] },
    { command: ['python'], args: ['-u', '-c', 'print(1)'] },
    { command: ['python', '-m'], args: ['http.server'] },
    { command: ['node', '--eval'], args: ['console.log(1)'] },
    { command: ['node'], args: ['--input-type=module', '-e', 'console.log(1)'] },
    { command: ['node', '-c'], args: ['script.js'] },
    { command: ['python', '-e'], args: ['print(1)'] },
    { command: ['/opt/custom/python', '-c'], args: ['print(1)'] },
  ])('retains an unsupported invocation as exact Command tokens: $command', (value) => {
    expect(executionMethod(value)).toBe('executable');
    expect(readExecutionScript(value)).toBeUndefined();
    expect(replaceExecutionScript(value, 'replacement')).toBe(value);
    expect(executionArguments(value)).toEqual(value.args);
  });

  it('restores independent Python, JavaScript and Shell drafts', () => {
    let editor = createExecutionCommandEditor({ command: ['echo'], args: ['kept'] });
    editor = selectExecutionMethod(editor, 'python');
    expect(editor.value).toEqual({ command: ['python', '-c'], args: [''] });
    editor = updateExecutionCommandEditor(editor, {
      command: ['python3', '-c'],
      args: ['print(1)', 'python arg'],
    });
    editor = selectExecutionMethod(editor, 'javascript');
    expect(editor.value).toEqual({ command: ['node', '-e'], args: [''] });
    editor = updateExecutionCommandEditor(editor, {
      command: ['node', '-e', 'console.log(1)', 'extra'],
      args: ['node arg'],
    });
    editor = selectExecutionMethod(editor, 'shell');
    expect(editor.value).toEqual({ command: ['sh', '-c'], args: [''] });
    expect(selectExecutionMethod(editor, 'python').value).toEqual({
      command: ['python3', '-c'],
      args: ['print(1)', 'python arg'],
    });
    expect(selectExecutionMethod(editor, 'javascript').value).toEqual({
      command: ['node', '-e', 'console.log(1)', 'extra'],
      args: ['node arg'],
    });
    expect(selectExecutionMethod(editor, 'executable').value).toEqual({
      command: ['echo'],
      args: ['kept'],
    });
    expect(selectExecutionMethod(editor, 'image').value).toEqual({ command: [], args: [] });
  });

  it.each([
    { command: ['sh', '-c'], args: [''] },
    { command: ['python', '-c'], args: [] },
    { command: ['python', '-c'], args: [' \n '] },
    { command: ['node', '-e', ''], args: ['kept'] },
  ])('reports an empty script without changing its stored values: $command', (value) => {
    const before = structuredClone(value);
    expect(executionCommandInputError(undefined, value)).toContain('before continuing');
    expect(value).toEqual(before);
  });

  it('preserves an unfinished raw command error ahead of script validation', () => {
    const value = { command: ['python', '-c'], args: [''] };
    const input = { text: 'python -c "', error: 'Close the double quote.', value };
    expect(executionCommandInputError(input, value)).toBe(input.error);
    expect(
      executionCommandInputError(undefined, { command: ['python', '-c'], args: ['pass'] }),
    ).toBe('');
  });
});
