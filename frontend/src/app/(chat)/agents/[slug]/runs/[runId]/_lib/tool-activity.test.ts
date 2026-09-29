import { describe, expect, it } from 'vitest';
import type { ToolCall } from '../_types';
import {
  describeTool,
  formatDuration,
  isFailure,
  outcome,
  policyDenial,
  spanDuration,
  summarize,
} from './tool-activity';

function call(
  name: string,
  input: Record<string, unknown>,
  over: Partial<ToolCall> = {},
): ToolCall {
  return { id: `${name}-${Math.random()}`, name, input, status: 'done', result: '', ...over };
}

describe('describeTool', () => {
  it.each([
    ['Bash', { command: 'go test ./...\necho done' }, 'command', 'Ran', 'go test ./...'],
    ['Read', { file_path: '/w/AGENTS.md' }, 'read', 'Read', '/w/AGENTS.md'],
    ['Edit', { file_path: '/w/a.go' }, 'write', 'Edited', '/w/a.go'],
    ['Grep', { pattern: 'TODO', path: 'src' }, 'search', 'Searched', 'TODO in src'],
    [
      'Agent',
      { description: 'find big files', prompt: 'long' },
      'agent',
      'Subagent',
      'find big files',
    ],
    [
      'mcp__github__get_pull_request',
      { owner: 'clear-route', pull_number: 42 },
      'mcp',
      'GitHub · get pull request',
      'clear-route #42',
    ],
    ['mcp__runtime__provision_sandbox', {}, 'platform', 'provision sandbox', ''],
  ])('%s', (name, input, kind, label, target) => {
    expect(describeTool(name, input)).toMatchObject({ kind, label, target });
  });
});

describe('outcome', () => {
  it.each([
    [call('Bash', {}, { result: 'ok\n' }), 'exit 0'],
    [call('Bash', {}, { status: 'error', result: 'Exit code 2\nboom' }), 'exit 2'],
    [call('Bash', {}, { result: '(Bash completed with no output)' }), 'empty'],
    [call('Read', {}, { result: 'a\nb\nc\n' }), '3 lines'],
    [call('Grep', {}, { result: 'x.go:1\n' }), '1 match'],
    [call('Glob', {}, { result: 'a\nb' }), '2 files'],
    [call('mcp__github__list', {}, { result: '[1,2,3]' }), '3 items'],
    [
      call(
        'Bash',
        {},
        {
          status: 'error',
          result: '<tool_use_error>Error: No such tool available: Bash.</tool_use_error>',
        },
      ),
      'not available',
    ],
    [call('Bash', {}, { status: 'running' }), ''],
  ])('%#', (tool, want) => {
    expect(outcome(tool)).toBe(want);
  });
});

describe('policyDenial', () => {
  it('names the host a command reached for', () => {
    const tool = call(
      'Bash',
      { command: 'curl -s https://api.github.com/repos/grafana/helm-charts/releases' },
      { result: '{"code":"forbidden","message":"egress denied"}' },
    );
    expect(policyDenial(tool)).toEqual({ blocked: 'api.github.com' });
    expect(isFailure(tool)).toBe(true);
  });

  it('names the MCP tool the gateway refused', () => {
    const tool = call(
      'mcp__github__merge',
      {},
      { status: 'error', result: 'tool not permitted (deny)' },
    );
    expect(policyDenial(tool)).toEqual({ blocked: 'GitHub · merge' });
  });

  it('ignores ordinary results', () => {
    expect(policyDenial(call('Bash', {}, { result: 'all good' }))).toBeNull();
  });
});

describe('summarize', () => {
  it('counts calls by what they did', () => {
    const tools = [
      call('Bash', {}),
      call('Bash', {}),
      call('Read', {}),
      call('mcp__github__a', {}),
      call('mcp__github__b', {}),
    ];
    expect(summarize(tools)).toBe('Ran 2 commands, read 1 file, called GitHub twice');
  });
});

describe('durations', () => {
  it('formats short and long spans', () => {
    expect(formatDuration(400)).toBe('0.4s');
    expect(formatDuration(42_000)).toBe('42s');
    expect(formatDuration(65_000)).toBe('1m 05s');
  });

  it('spans a group from the first start to the last end', () => {
    const tools = [
      call('Bash', {}, { startedAt: 1_000, endedAt: 5_000 }),
      call('Read', {}, { startedAt: 2_000, endedAt: 9_000 }),
    ];
    expect(spanDuration(tools)).toBe(8_000);
  });
});

describe('describeTool across runtimes', () => {
  it.each([
    // Codex
    [
      'commandExecution',
      { command: 'ls /usr | wc -l', cwd: '/w' },
      'command',
      'Ran',
      'ls /usr | wc -l',
    ],
    [
      'fileChange',
      { changes: [{ path: 'a.go' }, { path: 'b.go' }, { path: 'c.go' }] },
      'write',
      'Changed files',
      'a.go, b.go +1',
    ],
    [
      'spawnAgent',
      { prompt: 'count files\nthen report', receiver_thread_ids: [] },
      'agent',
      'Subagent',
      'count files',
    ],
    ['wait', { prompt: null, receiver_thread_ids: ['t1'] }, 'other', 'Waited for', '1 subagent'],
    ['webSearch', { query: 'orbit spiffe' }, 'web', 'Searched the web', 'orbit spiffe'],
    ['mcp__runtime__provision_sandbox', {}, 'platform', 'provision sandbox', ''],
    // OpenCode
    ['bash', { command: 'echo a', cwd: '/w' }, 'command', 'Ran', 'echo a'],
    ['read', { filePath: '/etc/hostname' }, 'read', 'Read', '/etc/hostname'],
    ['write', { filePath: '/w/a.txt', content: 'x' }, 'write', 'Wrote', '/w/a.txt'],
    ['edit', { filePath: '/w/a.go', oldString: 'a', newString: 'b' }, 'write', 'Edited', '/w/a.go'],
    ['glob', { pattern: '**/*.go' }, 'search', 'Found files', '**/*.go'],
    ['grep', { pattern: 'TODO', path: 'src' }, 'search', 'Searched', 'TODO in src'],
    [
      'task',
      { description: 'Count entries', prompt: 'Run ls', subagent_type: 'general' },
      'agent',
      'Subagent',
      'Count entries',
    ],
    ['runtime_provision_sandbox', {}, 'platform', 'provision sandbox', ''],
  ])('%s', (name, input, kind, label, target) => {
    expect(describeTool(name, input)).toMatchObject({ kind, label, target });
  });

  it('summarises object arguments instead of printing [object Object]', () => {
    const d = describeTool('mcp__github__create_issue', {
      repo: { owner: 'o', name: 'n' },
      labels: ['a', 'b'],
      draft: true,
    });
    expect(d.target).toBe('repo: {owner, name} labels: 2 items');
    expect(d.target).not.toContain('[object Object]');
    expect(describeTool('someTool', { nested: { a: 1 } }).target).toBe('nested: {a}');
  });

  it('gives Codex and OpenCode shell calls the same outcome chips', () => {
    for (const name of ['commandExecution', 'bash']) {
      expect(outcome(call(name, { command: 'ls' }, { result: 'a\n' }))).toBe('exit 0');
      expect(
        outcome(call(name, { command: 'make' }, { status: 'error', result: 'Exit code 2\nboom' })),
      ).toBe('exit 2');
    }
    expect(outcome(call('read', { filePath: '/x' }, { result: 'a\nb\n' }))).toBe('2 lines');
  });
});
