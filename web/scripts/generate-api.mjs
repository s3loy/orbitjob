import fs from 'node:fs/promises';
import openapiTS, { astToString } from 'openapi-typescript';
import ts from 'typescript';

const output = new URL('../src/api/schema.d.ts', import.meta.url);
const ast = await openapiTS(new URL('../../api/openapi.yaml', import.meta.url), {
  // Server defaults do not make an optional request field mandatory.
  defaultNonNullable: false,
  transform(schema) {
    // The lossless transport retains oversized JSON integers as decimal strings.
    if (schema.type === 'integer' && schema.format === 'int64') {
      return ts.factory.createUnionTypeNode([
        ts.factory.createKeywordTypeNode(ts.SyntaxKind.NumberKeyword),
        ts.factory.createKeywordTypeNode(ts.SyntaxKind.StringKeyword),
        ...(schema.nullable ? [ts.factory.createLiteralTypeNode(ts.factory.createNull())] : []),
      ]);
    }
  },
});
const result = '// Generated from api/openapi.yaml; run npm run generate.\n' + astToString(ast);
if (process.argv.includes('--check')) {
  if ((await fs.readFile(output, 'utf8')) !== result) {
    throw new Error('API type drift: run npm run generate');
  }
  console.log('API types match the authoritative schema');
} else {
  await fs.mkdir(new URL('../src/api/', import.meta.url), { recursive: true });
  await fs.writeFile(output, '');
  const lines = result.split(/(?<=\n)/);
  for (let start = 0; start < lines.length; start += 100) {
    const bytes = Buffer.from(lines.slice(start, start + 100).join(''));
    for (let offset = 0; offset < bytes.length; offset += 8192) {
      await fs.appendFile(output, bytes.subarray(offset, offset + 8192));
    }
  }
}
