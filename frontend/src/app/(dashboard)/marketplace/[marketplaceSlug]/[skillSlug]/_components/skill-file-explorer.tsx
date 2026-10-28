'use client';

/** @tier 3 — marketplace/[marketplaceSlug]/[skillSlug]-scoped. Folder tree of a Skill's files; picking a file selects it for the preview. */

import { useMemo, useState } from 'react';
import {
  Button,
  Card,
  CardContent,
  CardHeader,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  Icon,
  Separator,
  Stack,
  Text,
} from '@orbit/hades';
import type { IconColor, IconName } from '@orbit/hades';
import type { SkillFile } from '@orbit/shared/api';

/** A file of the Skill, with its path relative to the Skill's folder. */
export interface ExplorerFile {
  relativePath: string;
  file: SkillFile;
}

interface FileNode {
  kind: 'file';
  name: string;
  entry: ExplorerFile;
}

interface FolderNode {
  kind: 'folder';
  name: string;
  path: string;
  children: TreeNode[];
}

type TreeNode = FileNode | FolderNode;

const INDENT_REM = 1.25;
/** Width of a size="sm" Icon, so a file row lines up with a folder row's chevron. */
const CHEVRON_WIDTH = '1rem';

const CODE_EXTENSIONS = new Set([
  'py',
  'js',
  'mjs',
  'ts',
  'tsx',
  'jsx',
  'html',
  'css',
  'sh',
  'json',
  'yaml',
  'yml',
  'go',
  'rb',
  'toml',
  'xml',
]);

/** Builds the folder tree: folders before files, each group sorted by name. */
export function buildFileTree(files: ExplorerFile[]): TreeNode[] {
  const root: FolderNode = { kind: 'folder', name: '', path: '', children: [] };
  for (const entry of files) {
    const parts = entry.relativePath.split('/');
    let folder = root;
    for (const part of parts.slice(0, -1)) {
      const path = folder.path ? `${folder.path}/${part}` : part;
      let next = folder.children.find(
        (c): c is FolderNode => c.kind === 'folder' && c.name === part,
      );
      if (!next) {
        next = { kind: 'folder', name: part, path, children: [] };
        folder.children.push(next);
      }
      folder = next;
    }
    folder.children.push({ kind: 'file', name: parts[parts.length - 1], entry });
  }
  sortTree(root);
  return root.children;
}

function sortTree(folder: FolderNode) {
  folder.children.sort((a, b) =>
    a.kind === b.kind ? a.name.localeCompare(b.name) : a.kind === 'folder' ? -1 : 1,
  );
  for (const child of folder.children) {
    if (child.kind === 'folder') sortTree(child);
  }
}

/** The UTF-8 size of the content, as "512 B" or "10.1 KB". */
export function formatSize(content: string): string {
  const bytes = new TextEncoder().encode(content).length;
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function fileIcon(name: string): { name: IconName; color: IconColor } {
  const ext = name.includes('.') ? name.slice(name.lastIndexOf('.') + 1).toLowerCase() : '';
  if (ext === 'md') return { name: 'file-text', color: 'accent' };
  if (CODE_EXTENSIONS.has(ext)) return { name: 'file-code', color: 'status-degraded' };
  return { name: 'file', color: 'muted' };
}

const indent = (depth: number) => ({ paddingLeft: `${0.75 + depth * INDENT_REM}rem` });

interface SkillFileExplorerProps {
  files: ExplorerFile[];
  selectedPath: string;
  onSelect: (relativePath: string) => void;
}

export function SkillFileExplorer({ files, selectedPath, onSelect }: SkillFileExplorerProps) {
  const tree = useMemo(() => buildFileTree(files), [files]);
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const [expanded, setExpanded] = useState(true);

  const renderNodes = (nodes: TreeNode[], depth: number) =>
    nodes.map((node) => {
      if (node.kind === 'file') {
        const icon = fileIcon(node.name);
        const selected = node.entry.relativePath === selectedPath;
        return (
          <Button
            key={node.entry.relativePath}
            variant="ghost"
            size="tile"
            fullWidth
            selected={selected}
            aria-current={selected ? 'true' : undefined}
            onClick={() => onSelect(node.entry.relativePath)}
          >
            <Stack
              grow
              direction="horizontal"
              align="center"
              gap="snug"
              paddingBlock="snug"
              paddingInline="cozy"
              style={indent(depth)}
            >
              {/* Takes the chevron's place so file icons line up with folder icons. */}
              <Stack style={{ width: CHEVRON_WIDTH, flexShrink: 0 }} />
              <Icon name={icon.name} size="sm" color={icon.color} label="" />
              <Stack grow minWidth="none">
                <Text
                  as="span"
                  size="supporting"
                  mono
                  truncate
                  align="start"
                  color={selected ? 'accent' : 'foreground'}
                >
                  {node.name}
                </Text>
              </Stack>
              <Text as="span" size="caption" mono color="subtle">
                {formatSize(node.entry.file.content)}
              </Text>
            </Stack>
          </Button>
        );
      }

      const isOpen = !!open[node.path];
      return (
        <Collapsible
          key={node.path}
          open={isOpen}
          onOpenChange={(next) => setOpen((o) => ({ ...o, [node.path]: next }))}
        >
          <CollapsibleTrigger asChild>
            <Button variant="ghost" size="tile" fullWidth>
              <Stack
                grow
                direction="horizontal"
                align="center"
                gap="snug"
                paddingBlock="snug"
                paddingInline="cozy"
                style={indent(depth)}
              >
                <Icon
                  name="chevron-right"
                  size="sm"
                  color="muted"
                  rotate={isOpen ? 90 : 0}
                  label=""
                />
                <Icon
                  name={isOpen ? 'folder-open' : 'folder'}
                  size="sm"
                  color="status-degraded"
                  label=""
                />
                <Text as="span" size="supporting" mono truncate align="start">
                  {node.name}
                </Text>
              </Stack>
            </Button>
          </CollapsibleTrigger>
          <CollapsibleContent>
            <Stack gap="none">{renderNodes(node.children, depth + 1)}</Stack>
          </CollapsibleContent>
        </Collapsible>
      );
    });

  return (
    <Card column>
      <CardHeader
        title="File Explorer"
        divider
        action={
          <Text as="span" size="caption" mono color="subtle">
            {files.length} file{files.length === 1 ? '' : 's'}
          </Text>
        }
      />
      <Collapsible open={expanded} onOpenChange={setExpanded}>
        <CollapsibleContent>
          <CardContent flush>
            <Stack gap="none" padding="snug">
              {renderNodes(tree, 0)}
            </Stack>
          </CardContent>
        </CollapsibleContent>
        <Separator />
        <CollapsibleTrigger asChild>
          <Button
            variant="ghost"
            fullWidth
            shape="square"
            aria-label={expanded ? 'Hide files' : 'Show files'}
          >
            <Icon name={expanded ? 'chevron-up' : 'chevron-down'} size="sm" color="muted" />
          </Button>
        </CollapsibleTrigger>
      </Collapsible>
    </Card>
  );
}
