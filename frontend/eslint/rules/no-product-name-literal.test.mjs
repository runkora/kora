/**
 * RuleTester cases for local/no-product-name-literal. The TypeScript parser is
 * resolved through eslint-config-next, which depends on typescript-eslint, so
 * TSX and literal types are covered without a direct dependency.
 */
import { createRequire } from 'node:module';
import { describe, it } from 'vitest';
import { RuleTester } from 'eslint';
import rule from './no-product-name-literal.mjs';

const require = createRequire(import.meta.url);
const { parser } = createRequire(require.resolve('eslint-config-next'))('typescript-eslint');

RuleTester.describe = describe;
RuleTester.it = it;
RuleTester.itOnly = it.only;

const tester = new RuleTester({
  languageOptions: {
    parser,
    parserOptions: { ecmaFeatures: { jsx: true }, ecmaVersion: 'latest', sourceType: 'module' },
  },
});

const err = (word) => ({ messageId: 'literal', data: { word } });

tester.run('no-product-name-literal', rule, {
  valid: [
    // Identifier-shaped: joined through -, _, . or /.
    { code: "const a = 'kora-base';" },
    { code: "const a = 'orbit-system';" },
    { code: "const a = 'orbit.io';" },
    { code: "const a = 'api.orbit.local';" },
    { code: "const a = 'ORBIT_API_KEY';" },
    { code: "const a = '@runkora/hades';" },
    { code: "const a = 'orbit/authz/allow';" },
    { code: "const a = 'github.com/clear-route/orbit/docs';" },
    { code: "const a = 'orbit.io/inference-provider=true';" },
    { code: 'const a = `${x}/v1/mcp/orbit`;' },
    // Whole words only.
    { code: "const a = 'korat';" },
    { code: "const a = 'orbital mechanics';" },
    { code: "const a = 'decorbit';" },
    // Positions that name things rather than say them.
    { code: "import kora from 'kora';" },
    { code: "export { x } from 'orbit';" },
    { code: "const v = claims['orbit'];" },
    { code: "const o = { 'orbit': 1, kora: 2 };" },
    { code: "type T = 'orbit' | 'kora';" },
    { code: "const o = { orbit: 'a value without the word' };" },
    // Non-string literals and other text.
    { code: 'const n = 42;' },
    { code: 'const el = <p>Control plane for agents.</p>;' },
    { code: 'const el = <Text>Runs on the base image.</Text>;' },
    // Comments are not literals.
    { code: '// kora is the product\nconst a = 1;' },
  ],
  invalid: [
    { code: "const a = 'Log out of kora?';", errors: [err('kora')] },
    { code: "const a = 'Stopped by Orbit';", errors: [err('Orbit')] },
    { code: "const a = 'KORA';", errors: [err('KORA')] },
    { code: "const a = 'kora';", errors: [err('kora')] },
    { code: 'const a = "orbit";', errors: [err('orbit')] },
    // Sentence punctuation does not join: a full stop at the end is prose.
    { code: "const a = 'provisioned in kora.';", errors: [err('kora')] },
    { code: "const a = 'kora. Then more.';", errors: [err('kora')] },
    { code: 'const a = "kora\'s policy engine";', errors: [err('kora')] },
    // Template literal chunks, with and without expressions.
    { code: 'const a = `Message kora…`;', errors: [err('kora')] },
    { code: 'const a = `model_provider = "orbit"\\nmodel = ${m}`;', errors: [err('orbit')] },
    { code: 'const a = `${x} paused this run; kora gave up`;', errors: [err('kora')] },
    // JSX text and JSX attribute strings.
    { code: 'const el = <h1>How can kora help?</h1>;', errors: [err('kora')] },
    {
      code: 'const el = <Field description="kora gives up after this window." />;',
      errors: [err('kora')],
    },
    {
      code: 'const el = <Text>Real-time overview of the kora control plane.</Text>;',
      errors: [err('kora')],
    },
    // One report per literal, however many words it holds.
    { code: "const a = 'kora and orbit';", errors: [err('kora')] },
    // A property value is copy even when the key is not.
    { code: "const o = { description: 'inside kora: a run finishes' };", errors: [err('kora')] },
  ],
});
