import { describe, expect, it } from 'vitest';
import type { ToolCall } from '../_types';
import { describeTool, policyDenial } from './tool-activity';

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

describe('policyDenial', () => {
  it('names the host a command reached for', () => {
    const tool = call(
      'Bash',
      { command: 'curl -s https://api.github.com/repos/grafana/helm-charts/releases' },
      { result: '{"code":"forbidden","message":"egress denied"}' },
    );
    expect(policyDenial(tool)).toEqual({ blocked: 'api.github.com' });
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
    ['webSearch', { query: 'kora spiffe' }, 'web', 'Searched the web', 'kora spiffe'],
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
});

describe('numeric argument formatting', () => {
  it('prefixes # only for keys ending in number or id', () => {
    expect(describeTool('Custom', { issue_number: 7 }).target).toBe('#7');
    expect(describeTool('Custom', { user_id: 9 }).target).toBe('#9');
    expect(describeTool('Custom', { number_of_items: 5 }).target).toBe('5');
  });
});
