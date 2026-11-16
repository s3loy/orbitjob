import { describe, expect, it } from 'vitest';
import {
  compileJobTaskTemplate,
  HTTP_TASK_CODE,
  jobTaskTemplates,
  JSON_TASK_CODE,
  literalTaskArgument,
  templateDefaults,
  type JobTaskTemplateId,
  type JobTaskTemplateValues,
} from './job-task-templates';

function compile(id: JobTaskTemplateId, values: JobTaskTemplateValues = {}) {
  return compileJobTaskTemplate(id, { ...templateDefaults(id), ...values }, 'workspace-jobs');
}

describe('common Job task templates', () => {
  it.each([
    ['message', 'busybox:1.37.0', ['printf']],
    ['http', 'python:3.14-slim', ['python', '-c']],
    ['json', 'python:3.14-slim', ['python', '-c']],
  ] as const)('compiles the %s defaults to a manual Job without retries', (id, image, command) => {
    const result = compile(id);
    expect(result.errors).toEqual({});
    expect(result.behavior.length).toBeGreaterThan(0);
    expect(result.draft).toMatchObject({
      kind: 'ScheduledJob',
      namespace: 'workspace-jobs',
      image,
      command,
      schedule: '',
      suspend: false,
      attempts: '1',
      backoff: '0',
      tasks: [],
    });
    expect(result.draft).not.toHaveProperty('sourceSpec');
    expect(result.draft).not.toHaveProperty('sourceFormSpec');
  });

  it('offers descriptions and complete editable defaults for every template', () => {
    expect(jobTaskTemplates.map(({ id }) => id)).toEqual(['message', 'http', 'json']);
    for (const template of jobTaskTemplates) {
      expect(template.title.length).toBeGreaterThan(0);
      expect(template.description.length).toBeGreaterThan(0);
      expect(compileJobTaskTemplate(template.id, {}, '').draft?.namespace).toBe('');
    }
    expect(templateDefaults('message')).toEqual({ message: 'Hello from OrbitJob!' });
    expect(templateDefaults('http')).toEqual({
      url: 'https://example.com',
      expectedStatus: '200',
      requestTimeout: '10',
    });
    expect(JSON.parse(templateDefaults('json').input)).toEqual({
      service: 'orbitjob',
      status: 'ok',
    });
    expect(templateDefaults('json')).toMatchObject({ operation: 'format', field: 'status' });
  });

  it.each([
    ['', ''],
    [
      '%s %n $(touch /tmp/example); `whoami`\n"\u96ea" \\',
      '%s %n $$(touch /tmp/example); `whoami`\n"\u96ea" \\',
    ],
  ])('passes message data literally as a separate printf argument: %j', (message, argument) => {
    expect(compile('message', { message }).draft?.args).toEqual(['%s\\n', argument]);
  });

  it('keeps HTTP parameters separate from the constant Python source', () => {
    const url = 'https://example.com/a?value=$(whoami);print(%22hello%22)&next=1';
    const result = compile('http', { url, expectedStatus: '404', requestTimeout: '17' });
    expect(result.errors).toEqual({});
    expect(result.draft?.args).toEqual([
      HTTP_TASK_CODE,
      'https://example.com/a?value=$$(whoami);print(%22hello%22)&next=1',
      '404',
      '17',
    ]);
    expect(HTTP_TASK_CODE).not.toContain(url);
    expect(result.behavior.join(' ')).toContain('without following redirects');
    expect(result.behavior.join(' ')).toContain('do not log the response body');
  });

  it.each([
    '',
    'example.com',
    'https:example.com',
    'ftp://example.com',
    'https://',
    'https:///example.com',
    'https://user:password@example.com',
    'https://@example.com',
    'https://example.com/#section',
    'https://example.com/#',
    ' https://example.com',
    'https://example.com/space here',
    'https://example.com/\npath',
    'https://example.com/\u0000path',
    'https://example.com/\u007fpath',
    'https://example.com/\u0085path',
    'https://example.com\\other',
    'https://example.com:65536',
    'https://example.com:wrong',
    'https://[invalid]',
  ])('rejects an invalid HTTP URL without producing a draft: %j', (url) => {
    const values = { url, expectedStatus: '200', requestTimeout: '10' };
    const result = compile('http', values);
    expect(result.draft).toBeUndefined();
    expect(result.errors.url).toBeTruthy();
    expect(values.url).toBe(url);
  });

  it.each([
    ['http://localhost:8080/health', 'http://localhost:8080/health'],
    ['https://[::1]:443/', 'https://[::1]/'],
    ['HTTPS://example.com/%20?q=%23', 'https://example.com/%20?q=%23'],
    [
      'https://\u4f8b\u5b50.\u6d4b\u8bd5/\u8def\u5f84?\u67e5\u8be2=\u96ea',
      'https://xn--fsqu00a.xn--0zwm56d/%E8%B7%AF%E5%BE%84?%E6%9F%A5%E8%AF%A2=%E9%9B%AA',
    ],
  ])('normalizes a valid URL to ASCII for the HTTP request: %s', (url, requestURL) => {
    const result = compile('http', { url });
    expect(result.draft?.args[1]).toBe(requestURL);
    expect(result.behavior.join(' ')).toContain(requestURL);
  });

  it.each([
    ['expectedStatus', '99'],
    ['expectedStatus', '600'],
    ['expectedStatus', '200.0'],
    ['expectedStatus', ' 200'],
    ['expectedStatus', '2e2'],
    ['expectedStatus', ''],
    ['expectedStatus', '200; exit 0'],
    ['requestTimeout', '0'],
    ['requestTimeout', '121'],
    ['requestTimeout', '-1'],
    ['requestTimeout', '1.5'],
    ['requestTimeout', '10\n'],
    ['requestTimeout', ''],
  ])('rejects %s=%j instead of clamping it', (field, value) => {
    const result = compile('http', { [field]: value });
    expect(result.draft).toBeUndefined();
    expect(result.errors[field]).toBeTruthy();
  });

  it.each([
    ['100', '1'],
    ['599', '120'],
  ])('accepts HTTP integer bounds %s and %s', (expectedStatus, requestTimeout) => {
    expect(compile('http', { expectedStatus, requestTimeout }).errors).toEqual({});
  });

  it('compiles numeric values canonically without Python integer-string limits', () => {
    const result = compile('http', {
      expectedStatus: `${'0'.repeat(4400)}200`,
      requestTimeout: '00010',
    });
    expect(result.errors).toEqual({});
    expect(result.draft?.args.slice(2)).toEqual(['200', '10']);
  });

  it.each(['null', 'true', '7', '"\u96ea"', '[1, {"status": "ok"}]'])(
    'formats any valid JSON value: %s',
    (input) => {
      expect(compile('json', { input }).draft?.args).toEqual([JSON_TASK_CODE, input, 'format', '']);
    },
  );

  it.each(['', '{', '{"status":}', 'NaN', 'undefined', '{"value": 1,}'])(
    'rejects invalid JSON without producing a draft: %j',
    (input) => {
      const result = compile('json', { input });
      expect(result.draft).toBeUndefined();
      expect(result.errors.input).toBeTruthy();
    },
  );

  it.each(['', '__proto__', 'constructor', 'status.with.dots', '\u96ea\n"; print(1); #'])(
    'reads an own top-level JSON key literally: %j',
    (field) => {
      const input = JSON.stringify({ [field]: { value: '\u96ea' } });
      const result = compile('json', { input, operation: 'field', field });
      expect(result.errors).toEqual({});
      expect(result.draft?.args).toEqual([JSON_TASK_CODE, input, 'field', field]);
      expect(JSON_TASK_CODE).not.toContain(input);
    },
  );

  it.each([
    ['{}', 'toString'],
    ['{}', '__proto__'],
    ['[]', 'length'],
    ['["value"]', '0'],
    ['null', 'status'],
    ['"status"', 'status'],
    ['{"nested":{"status":"ok"}}', 'nested.status'],
  ])('requires an own top-level object field for %s and %s', (input, field) => {
    const result = compile('json', { input, operation: 'field', field });
    expect(result.draft).toBeUndefined();
    expect(result.errors.field).toBeTruthy();
  });

  it('rejects an unknown JSON operation', () => {
    const result = compile('json', { operation: 'evaluate' });
    expect(result.draft).toBeUndefined();
    expect(result.errors.operation).toBeTruthy();
  });

  it.each(['1e400', '-1e400', '{"nested":[{"value":1e400}]}'])(
    'rejects nonfinite JSON numbers at any depth: %s',
    (input) => {
      const result = compile('json', { input });
      expect(result.draft).toBeUndefined();
      expect(result.errors.input).toContain('finite');
    },
  );

  it.each(['a'.repeat(1025), '\u0000', 'missing'])(
    'ignores the unused field in JSON format mode: %j',
    (field) => {
      const result = compile('json', { field });
      expect(result.errors).toEqual({});
      expect(result.draft?.args[3]).toBe('');
    },
  );

  it.each([
    ['$', '$$'],
    ['$$', '$$$$'],
    ['$(HOME)', '$$(HOME)'],
    ['$$(HOME)', '$$$$(HOME)'],
    ['price $5; ${HOME}; $(echo hello)', 'price $$5; $${HOME}; $$(echo hello)'],
  ])(
    'escapes Kubernetes expansion while preserving the intended argument %j',
    (value, argument) => {
      expect(literalTaskArgument(value)).toBe(argument);
      expect(compile('message', { message: value }).draft?.args[1]).toBe(argument);
      const input = JSON.stringify({ [value]: value });
      const result = compile('json', { input, operation: 'field', field: value });
      expect(result.draft?.args).toEqual([
        JSON_TASK_CODE,
        JSON.stringify({ [argument]: argument }),
        'field',
        argument,
      ]);
    },
  );

  it.each([
    ['message', { message: 'hello\u0000world' }, 'message'],
    ['http', { url: 'https://example.com/\u0000' }, 'url'],
    ['json', { input: '{"value":"\u0000"}' }, 'input'],
    ['json', { input: '{"\\u0000":true}', operation: 'field', field: '\u0000' }, 'field'],
  ] as const)('rejects null characters in %s argv values', (id, values, field) => {
    const result = compile(id, values);
    expect(result.draft).toBeUndefined();
    expect(result.errors[field]).toBeTruthy();
  });

  it('accepts JSON text containing an escaped null character', () => {
    const input = '{"value":"\\u0000"}';
    const result = compile('json', { input, operation: 'field', field: 'value' });
    expect(result.errors).toEqual({});
    expect(result.draft?.args[1]).toBe(input);
  });

  it('enforces message, URL, JSON, and field size limits without truncation', () => {
    const message = '\u96ea'.repeat(8192);
    expect(compile('message', { message }).draft?.args[1]).toBe(message);
    expect(compile('message', { message: message + '\u96ea' }).errors.message).toBeTruthy();

    const url = 'https://example.com/'.padEnd(2048, 'a');
    expect(compile('http', { url }).draft?.args[1]).toBe(url);
    expect(compile('http', { url: url + 'a' }).errors.url).toBeTruthy();

    const input = JSON.stringify('a'.repeat(32766));
    expect(input.length).toBe(32768);
    expect(compile('json', { input }).draft?.args[1]).toBe(input);
    expect(compile('json', { input: input + ' ' }).errors.input).toBeTruthy();

    const field = 'a'.repeat(1024);
    const object = JSON.stringify({ [field]: true });
    expect(compile('json', { input: object, operation: 'field', field }).draft?.args[3]).toBe(
      field,
    );
    expect(compile('json', { operation: 'field', field: field + 'a' }).errors.field).toBeTruthy();
  });

  it('returns independent defaults, drafts, errors, and behavior on each compilation', () => {
    const defaults = templateDefaults('message');
    defaults.message = 'changed';
    expect(templateDefaults('message').message).toBe('Hello from OrbitJob!');

    const first = compile('http');
    first.draft!.command[0] = 'changed';
    first.draft!.args[1] = 'changed';
    first.draft!.tasks.push({
      name: 'changed',
      job: '',
      dependencies: [],
      combination: '',
      rules: [],
    });
    first.errors.url = 'changed';
    first.behavior.push('changed');
    const next = compile('http');
    expect(next.draft?.command).toEqual(['python', '-c']);
    expect(next.draft?.args[1]).toBe('https://example.com/');
    expect(next.draft?.tasks).toEqual([]);
    expect(next.errors).toEqual({});
    expect(next.behavior).not.toContain('changed');
  });
});
