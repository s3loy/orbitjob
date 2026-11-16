import { useId, useRef, useState } from 'react';
import { Button, TextArea } from '@radix-ui/themes';
import {
  createExecutionCommandEditor,
  executionArguments,
  executionCommandInputError,
  executionMethod,
  formatExecutionCommandLine,
  parseExecutionCommandLine,
  readExecutionScript,
  reconcileExecutionCommandEditor,
  replaceExecutionArguments,
  replaceExecutionScript,
  selectExecutionMethod,
  updateExecutionCommandEditor,
  type ExecutionCommand,
  type ExecutionCommandInput,
  type ExecutionMethod,
} from './execution-command';
import './execution-command.css';

type Props = {
  command: string[];
  args: string[];
  input?: ExecutionCommandInput;
  disabled?: boolean;
  onChange(command: string[], args: string[], input?: ExecutionCommandInput): void;
};

function matchingInput(input: ExecutionCommandInput | undefined, value: ExecutionCommand) {
  return input &&
    input.value.command.length === value.command.length &&
    input.value.command.every((token, index) => token === value.command[index]) &&
    input.value.args.length === value.args.length &&
    input.value.args.every((token, index) => token === value.args[index])
    ? input
    : undefined;
}

export function ExecutionCommandFields(props: Props) {
  const { command, args, input, disabled = false, onChange } = props;
  const id = useId();
  const advanced = useRef<HTMLDetailsElement>(null);
  const [retained, setRetained] = useState(() => createExecutionCommandEditor({ command, args }));
  const [localInput, setLocalInput] = useState<ExecutionCommandInput>();
  const editor = reconcileExecutionCommandEditor(retained, { command, args });
  if (editor !== retained) {
    setRetained(editor);
    if (localInput) setLocalInput(undefined);
  }
  const value = editor.value;
  const method = executionMethod(value);
  const script = readExecutionScript(value);
  const scriptLabel =
    script?.method === 'python'
      ? 'Python script'
      : script?.method === 'javascript'
        ? 'JavaScript'
        : 'Shell script';
  const arguments_ = executionArguments(value);
  const currentInput = matchingInput(Object.hasOwn(props, 'input') ? input : localInput, value);
  const commandError = executionCommandInputError(currentInput, value);
  const commandLine =
    currentInput?.text ??
    (value.command.length === 1 && value.command[0] === '' && !value.args.length
      ? ''
      : formatExecutionCommandLine(value));
  const complexCommand = value.command.length > 1;
  const extraCommandTokens =
    script?.location === 'command' ? Math.max(0, value.command.length - script.index - 1) : 0;

  function change(next: ExecutionCommand) {
    if (disabled) return;
    const updated = updateExecutionCommandEditor(editor, next);
    setRetained(updated);
    setLocalInput(undefined);
    onChange(updated.value.command, updated.value.args, undefined);
  }

  function changeCommandLine(text: string) {
    if (disabled) return;
    const parsed = parseExecutionCommandLine(text);
    const updated = updateExecutionCommandEditor(editor, parsed.error ? value : parsed.value);
    const nextInput = { text, error: parsed.error, value: updated.value };
    setRetained(updated);
    setLocalInput(nextInput);
    onChange(updated.value.command, updated.value.args, nextInput);
  }

  function changeMethod(next: ExecutionMethod) {
    if (disabled) return;
    const updated = selectExecutionMethod(editor, next);
    setRetained(updated);
    setLocalInput(undefined);
    onChange(updated.value.command, updated.value.args, undefined);
  }

  function editExactTokens() {
    if (!advanced.current) return;
    advanced.current.open = true;
    advanced.current.querySelector<HTMLTextAreaElement>('textarea')?.focus();
  }

  return (
    <fieldset
      className="execution-command-fields"
      disabled={disabled}
      aria-label="Execution command"
    >
      <label className="form-field execution-method" htmlFor={`${id}-method`}>
        Execution method
        <select
          id={`${id}-method`}
          aria-label="Execution method"
          value={method}
          disabled={disabled}
          onChange={(event) => changeMethod(event.target.value as ExecutionMethod)}
        >
          <option value="image">Use image default</option>
          <option value="executable">Command</option>
          <option value="shell">Shell script</option>
          <option value="python">Python script</option>
          <option value="javascript">JavaScript</option>
        </select>
      </label>
      {method === 'image' && (
        <p className="execution-command-hint">
          Use the image&apos;s default command. Override its arguments in Exact command and
          arguments.
        </p>
      )}

      {method === 'executable' && (
        <div className="execution-command-program">
          <label className="form-field" htmlFor={`${id}-command-line`}>
            Command
            <TextArea
              id={`${id}-command-line`}
              aria-label="Command"
              aria-describedby={`${id}-command-help${commandError ? ` ${id}-command-error` : ''}`}
              aria-invalid={Boolean(commandError)}
              className="execution-command-line"
              rows={2}
              value={commandLine}
              readOnly={complexCommand}
              disabled={disabled}
              spellCheck={false}
              placeholder="python report.py --format 'two words'"
              onChange={(event) => changeCommandLine(event.target.value)}
            />
          </label>
          <p id={`${id}-command-help`} className="execution-command-hint">
            Quotes group an argument. Use Shell script for pipes or multiple commands.
          </p>
          {commandError && (
            <p id={`${id}-command-error`} className="execution-command-error" role="alert">
              {commandError}
            </p>
          )}
          {complexCommand && (
            <div className="execution-command-imported">
              <p className="execution-command-note">
                This command uses multiple stored command tokens. Edit exact tokens to preserve
                their placement before arguments.
              </p>
              <Button
                type="button"
                size="1"
                variant="outline"
                disabled={disabled}
                onClick={editExactTokens}
              >
                Edit exact tokens
              </Button>
            </div>
          )}
        </div>
      )}

      {script && (
        <div className="execution-command-program">
          {script.method === 'shell' && (
            <label className="form-field execution-shell" htmlFor={`${id}-shell`}>
              Shell executable
              <select
                id={`${id}-shell`}
                aria-label="Shell executable"
                value={script.executable}
                disabled={disabled}
                onChange={(event) =>
                  change({
                    command: [event.target.value, ...value.command.slice(1)],
                    args: value.args,
                  })
                }
              >
                <option value="sh">sh</option>
                <option value="/bin/sh">/bin/sh</option>
              </select>
            </label>
          )}
          <label className="form-field" htmlFor={`${id}-script`}>
            {scriptLabel}
            <TextArea
              id={`${id}-script`}
              aria-label={scriptLabel}
              aria-describedby={`${id}-script-help${commandError ? ` ${id}-script-error` : ''}`}
              aria-invalid={Boolean(commandError)}
              className="execution-script-input"
              rows={5}
              value={script.text}
              disabled={disabled}
              spellCheck={false}
              placeholder={
                script.method === 'python'
                  ? 'print("Hello from OrbitJob")'
                  : script.method === 'javascript'
                    ? 'console.log("Hello from OrbitJob");'
                    : 'echo "Starting job"\n/app/run'
              }
              onChange={(event) => change(replaceExecutionScript(value, event.target.value))}
            />
          </label>
          <p id={`${id}-script-help`} className="execution-command-hint">
            Runs with{' '}
            <code>
              {script.executable} {script.option}
            </code>
            . The image must include this
            {script.method === 'shell' ? ' shell' : ' interpreter'}. Add positional arguments in
            Exact command and arguments.
          </p>
          {commandError && (
            <p id={`${id}-script-error`} className="execution-command-error" role="alert">
              {commandError}
            </p>
          )}
          {extraCommandTokens > 0 && (
            <p className="execution-command-note">
              Additional command tokens are preserved before arguments. Review them in Exact command
              and arguments.
            </p>
          )}
        </div>
      )}

      <details className="execution-command-advanced" ref={advanced}>
        <summary>Exact command and arguments</summary>
        <div className="execution-command-advanced-content">
          <section className="execution-command-tokens" aria-labelledby={`${id}-tokens`}>
            <h3 id={`${id}-tokens`}>Command tokens</h3>
            <p className="execution-command-hint">
              The first token names the executable. Remaining command tokens come before arguments.
              Each entry is preserved exactly, including empty values.
            </p>
            {!value.command.length && (
              <p className="execution-command-hint">
                No command override. The image default is used.
              </p>
            )}
            {value.command.map((token, index) => (
              <div className="execution-token-row" key={index}>
                <label className="execution-token-field" htmlFor={`${id}-command-${index}`}>
                  Command token {index + 1}
                  <TextArea
                    id={`${id}-command-${index}`}
                    aria-label={`Command token ${index + 1}`}
                    rows={1}
                    value={token}
                    disabled={disabled}
                    spellCheck={false}
                    placeholder="Empty command token"
                    onChange={(event) =>
                      change({
                        command: value.command.map((item, position) =>
                          position === index ? event.target.value : item,
                        ),
                        args: value.args,
                      })
                    }
                  />
                </label>
                <Button
                  type="button"
                  size="1"
                  variant="ghost"
                  disabled={disabled}
                  aria-label={`Remove command token ${index + 1}`}
                  onClick={() =>
                    change({
                      command: value.command.filter((_, position) => position !== index),
                      args: value.args,
                    })
                  }
                >
                  Remove
                </Button>
              </div>
            ))}
            <Button
              type="button"
              size="1"
              variant="outline"
              disabled={disabled}
              onClick={() => change({ command: [...value.command, ''], args: value.args })}
            >
              Add command token
            </Button>
          </section>

          <section className="execution-arguments" aria-labelledby={`${id}-arguments`}>
            <h3 id={`${id}-arguments`}>
              {script
                ? `${script.method === 'shell' ? 'Shell positional' : 'Script'} arguments`
                : 'Arguments'}
            </h3>
            <p className="execution-command-hint">
              {script
                ? script.method === 'shell'
                  ? 'These values follow the script and any additional command tokens. The first value after the script sets $0, then $1, $2, and so on.'
                  : 'These values follow the script and any additional command tokens. Each entry is preserved as one argument.'
                : 'Each entry is one argument. Spaces, quotes and empty values are preserved.'}
            </p>
            {arguments_.map((argument, index) => (
              <div className="execution-token-row" key={index}>
                <label className="execution-token-field" htmlFor={`${id}-argument-${index}`}>
                  Argument {index + 1}
                  <TextArea
                    id={`${id}-argument-${index}`}
                    aria-label={`Argument ${index + 1}`}
                    rows={1}
                    value={argument}
                    disabled={disabled}
                    spellCheck={false}
                    placeholder="Empty argument"
                    onChange={(event) =>
                      change(
                        replaceExecutionArguments(
                          value,
                          arguments_.map((item, position) =>
                            position === index ? event.target.value : item,
                          ),
                        ),
                      )
                    }
                  />
                </label>
                <Button
                  type="button"
                  size="1"
                  variant="ghost"
                  disabled={disabled}
                  aria-label={`Remove argument ${index + 1}`}
                  onClick={() =>
                    change(
                      replaceExecutionArguments(
                        value,
                        arguments_.filter((_, position) => position !== index),
                      ),
                    )
                  }
                >
                  Remove
                </Button>
              </div>
            ))}
            <Button
              type="button"
              size="1"
              variant="outline"
              disabled={disabled}
              onClick={() => change(replaceExecutionArguments(value, [...arguments_, '']))}
            >
              Add argument
            </Button>
          </section>
        </div>
      </details>
    </fieldset>
  );
}
