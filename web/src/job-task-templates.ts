import { initialDraft, type Draft } from './api/manifests';

export type JobTaskTemplateId = 'message' | 'http' | 'json';
export type JobTaskTemplateValues = Record<string, string>;
export type JobTaskTemplate = {
  id: JobTaskTemplateId;
  title: string;
  description: string;
};

export const jobTaskTemplates: JobTaskTemplate[] = [
  {
    id: 'message',
    title: 'Print a message',
    description: 'Write a message to the task log.',
  },
  {
    id: 'http',
    title: 'Check an HTTP endpoint',
    description: 'Send a GET request and check the response status.',
  },
  {
    id: 'json',
    title: 'Process JSON',
    description: 'Format JSON or read a top-level field.',
  },
];

export const HTTP_TASK_CODE = `import http.client
import sys
import urllib.error
import urllib.request

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, new_url):
        return None

url, expected, timeout = sys.argv[1:]
expected = int(expected)
timeout = int(timeout)
opener = urllib.request.build_opener(NoRedirect())
try:
    request = urllib.request.Request(url, method="GET")
    with opener.open(request, timeout=timeout) as response:
        status = response.status
except urllib.error.HTTPError as error:
    status = error.code
    error.close()
except (urllib.error.URLError, http.client.HTTPException, OSError, ValueError):
    print("HTTP request failed.", file=sys.stderr)
    sys.exit(1)

print(f"HTTP status: {status}; expected: {expected}")
sys.exit(0 if status == expected else 1)
`;

export const JSON_TASK_CODE = `import json
import sys

source, operation, field = sys.argv[1:]
try:
    value = json.loads(source)
except (json.JSONDecodeError, ValueError):
    print("Invalid JSON input.", file=sys.stderr)
    sys.exit(1)

if operation == "field":
    if not isinstance(value, dict) or field not in value:
        print("The top-level field is missing.", file=sys.stderr)
        sys.exit(1)
    value = value[field]
elif operation != "format":
    print("Unknown JSON operation.", file=sys.stderr)
    sys.exit(1)

try:
    output = json.dumps(value, indent=2, ensure_ascii=False, allow_nan=False)
except (ValueError, OverflowError):
    print("The JSON output is not supported.", file=sys.stderr)
    sys.exit(1)
print(output.encode("utf-8", errors="backslashreplace").decode("utf-8"))
`;

export function templateDefaults(id: JobTaskTemplateId): JobTaskTemplateValues {
  switch (id) {
    case 'message':
      return { message: 'Hello from OrbitJob!' };
    case 'http':
      return { url: 'https://example.com', expectedStatus: '200', requestTimeout: '10' };
    case 'json':
      return {
        input: '{\n  "service": "orbitjob",\n  "status": "ok"\n}',
        operation: 'format',
        field: 'status',
      };
  }
}

export type CompiledJobTaskTemplate = {
  draft?: Draft;
  errors: Record<string, string>;
  behavior: string[];
};

export function literalTaskArgument(value: string): string {
  // Kubernetes expands dollar expressions before passing arguments to the process.
  return value.replaceAll('$', () => '$$');
}

function baseDraft(name: string, namespace: string): Draft {
  return {
    ...initialDraft('ScheduledJob'),
    name,
    namespace,
    schedule: '',
    suspend: false,
    attempts: '1',
    backoff: '0',
    tasks: [],
  };
}

function validURL(value: string): boolean {
  if (!/^https?:\/\/[^/?#]+/i.test(value) || /[\s\\]|\p{Cc}/u.test(value)) return false;
  try {
    const url = new URL(value);
    const authority = value.slice(value.indexOf('://') + 3).split(/[/?#]/, 1)[0];
    return (
      (url.protocol === 'http:' || url.protocol === 'https:') &&
      Boolean(url.hostname) &&
      !url.username &&
      !url.password &&
      !authority.includes('@') &&
      !value.includes('#')
    );
  } catch {
    return false;
  }
}

function integerInRange(value: string, minimum: number, maximum: number): boolean {
  return (
    value.length > 0 && !/\D/.test(value) && Number(value) >= minimum && Number(value) <= maximum
  );
}

function hasNonFiniteNumbers(value: unknown): boolean {
  const remaining = [value];
  while (remaining.length) {
    const item = remaining.pop();
    if (typeof item === 'number' && !Number.isFinite(item)) return true;
    if (item !== null && typeof item === 'object') remaining.push(...Object.values(item));
  }
  return false;
}

export function compileJobTaskTemplate(
  id: JobTaskTemplateId,
  values: JobTaskTemplateValues,
  namespace: string,
): CompiledJobTaskTemplate {
  const fields = { ...templateDefaults(id), ...values };
  const errors: Record<string, string> = {};
  let draft: Draft;
  let behavior: string[];

  switch (id) {
    case 'message': {
      const { message } = fields;
      if (message.length > 8192) errors.message = 'Use at most 8,192 characters.';
      else if (message.includes('\u0000'))
        errors.message = 'Remove null characters from the message.';
      draft = {
        ...baseDraft('print-message', namespace),
        image: 'busybox:1.37.0',
        command: ['printf'],
        args: ['%s\\n', literalTaskArgument(message)],
      };
      behavior = [
        'Print the message literally, followed by a newline.',
        'Write to standard output for collection in run logs.',
      ];
      break;
    }
    case 'http': {
      const { url, expectedStatus, requestTimeout } = fields;
      if (url.length > 2048) errors.url = 'Use at most 2,048 characters.';
      else if (!validURL(url))
        errors.url =
          'Enter an absolute HTTP or HTTPS URL without credentials, a fragment, or whitespace.';
      if (!integerInRange(expectedStatus, 100, 599))
        errors.expectedStatus = 'Enter an integer from 100 to 599.';
      if (!integerInRange(requestTimeout, 1, 120))
        errors.requestTimeout = 'Enter an integer from 1 to 120 seconds.';
      const requestURL = errors.url ? url : new URL(url).href;
      draft = {
        ...baseDraft('check-http-status', namespace),
        image: 'python:3.14-slim',
        command: ['python', '-c'],
        args: [
          HTTP_TASK_CODE,
          literalTaskArgument(requestURL),
          String(Number(expectedStatus)),
          String(Number(requestTimeout)),
        ],
      };
      behavior = [
        `Send one GET request to ${requestURL}, without following redirects.`,
        `Use a request timeout of ${requestTimeout} seconds.`,
        `Log the response status and expected status ${expectedStatus}; do not log the response body.`,
        'Exit successfully only when the response status matches; a mismatch or network error fails the task.',
      ];
      break;
    }
    case 'json': {
      const { input, operation, field } = fields;
      let parsed: unknown;
      if (input.length > 32768) errors.input = 'Use at most 32,768 characters.';
      else if (input.includes('\u0000'))
        errors.input = 'Remove null characters from the JSON input.';
      else {
        try {
          parsed = JSON.parse(input);
          if (hasNonFiniteNumbers(parsed))
            errors.input = 'Use JSON numbers within the finite numeric range.';
        } catch {
          errors.input = 'Enter valid JSON.';
        }
      }
      if (operation !== 'format' && operation !== 'field')
        errors.operation = 'Choose Format JSON or Read a field.';
      if (operation === 'field') {
        if (field.length > 1024) errors.field = 'Use at most 1,024 characters for the field name.';
        else if (field.includes('\u0000'))
          errors.field = 'Remove null characters from the field name.';
        else if (
          !errors.input &&
          (parsed === null ||
            typeof parsed !== 'object' ||
            Array.isArray(parsed) ||
            !Object.hasOwn(parsed, field))
        )
          errors.field = 'Choose a field that exists on the top-level JSON object.';
      }
      draft = {
        ...baseDraft('process-json', namespace),
        image: 'python:3.14-slim',
        command: ['python', '-c'],
        args: [
          JSON_TASK_CODE,
          literalTaskArgument(input),
          operation,
          operation === 'field' ? literalTaskArgument(field) : '',
        ],
      };
      behavior = [
        operation === 'field'
          ? `Read the top-level field ${JSON.stringify(field)} and print its value as JSON.`
          : 'Print the supplied JSON with two-space indentation.',
        'Keep Unicode characters in the output and finish with a newline.',
        'Exit successfully after writing the JSON output; invalid input or a missing field fails the task.',
      ];
      break;
    }
  }

  return Object.keys(errors).length ? { errors, behavior } : { draft, errors, behavior };
}
