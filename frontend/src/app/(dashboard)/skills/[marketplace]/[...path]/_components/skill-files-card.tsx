'use client';

/** @tier 3 — skills/[marketplace]/[...path]-scoped. The Files card: the skill folder as a tree with sizes; a row opens its file. */

import { useMemo, useState } from 'react';
import {
  Button,
  Card,
  CardHeader,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  Icon,
  Stack,
  Text,
  type IconName,
} from '@runkora/hades';
import type { SkillFileEntry } from '@runkora/shared/api';
import { formatBytes, plural } from './skill-format';

interface FileNode {
  kind: 'file';
  name: string;
  file: SkillFileEntry;
}

interface FolderNode {
  kind: 'folder';
  name: string;
  path: string;
  children: TreeNode[];
}

type TreeNode = FileNode | FolderNode;

const CODE_EXTENSIONS = new Set([
  'py',
  'js',
  'mjs',
  'ts',
  'tsx',
  'sh',
  'json',
  'yaml',
  'yml',
  'go',
  'html',
  'css',
]);

/** Hades has no tree, so each level indents by a fixed step, as the current file explorer does. */
const INDENT_REM = 1.25;
/** Width of a size="sm" Icon, the chevron a file row leaves room for. */
const CHEVRON_WIDTH = '1rem';
const indent = (depth: number) => ({ paddingLeft: `${0.75 + depth * INDENT_REM}rem` });

/** SKILL.md first, then folders, then files, each group by name. */
function buildTree(files: SkillFileEntry[]): TreeNode[] {
  const root: FolderNode = { kind: 'folder', name: '', path: '', children: [] };
  for (const file of files) {
    const parts = file.path.split('/');
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
    folder.children.push({ kind: 'file', name: parts[parts.length - 1]!, file });
  }
  sortTree(root);
  return root.children;
}

function rank(node: TreeNode): number {
  if (node.kind === 'file' && node.file.path === 'SKILL.md') return 0;
  return node.kind === 'folder' ? 1 : 2;
}

function sortTree(folder: FolderNode) {
  folder.children.sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name));
  for (const child of folder.children) if (child.kind === 'folder') sortTree(child);
}

function folderBytes(folder: FolderNode): number {
  return folder.children.reduce(
    (sum, child) => sum + (child.kind === 'file' ? child.file.size : folderBytes(child)),
    0,
  );
}

function fileIcon(name: string): IconName {
  const ext = name.includes('.') ? name.slice(name.lastIndexOf('.') + 1).toLowerCase() : '';
  return CODE_EXTENSIONS.has(ext) ? 'file-code' : 'file';
}

interface SkillFilesCardProps {
  /** Every file in the folder, SKILL.md included. */
  files: SkillFileEntry[];
  selectedPath: string;
  onSelect: (path: string) => void;
}

export function SkillFilesCard({ files, selectedPath, onSelect }: SkillFilesCardProps) {
  const tree = useMemo(() => buildTree(files), [files]);
  const total = files.reduce((sum, file) => sum + file.size, 0);
  // Folders start closed so a long folder does not push the Source card away, except the one
  // holding the open file.
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const isOpen = (folder: FolderNode) =>
    open[folder.path] ?? selectedPath.startsWith(`${folder.path}/`);

  const renderNodes = (nodes: TreeNode[], depth: number): React.ReactNode =>
    nodes.map((node) => {
      if (node.kind === 'file') {
        const selected = node.file.path === selectedPath;
        return (
          <Button
            key={node.file.path}
            variant={selected ? 'secondary' : 'ghost'}
            size="tile"
            fullWidth
            aria-current={selected ? 'true' : undefined}
            onClick={() => onSelect(node.file.path)}
          >
            <Row depth={depth} icon={fileIcon(node.name)} selected={selected}>
              <FileName name={node.name} selected={selected} />
              <Size bytes={node.file.size} />
            </Row>
          </Button>
        );
      }

      const expanded = isOpen(node);
      return (
        <Collapsible
          key={node.path}
          open={expanded}
          onOpenChange={(next) => setOpen((o) => ({ ...o, [node.path]: next }))}
        >
          <CollapsibleTrigger asChild>
            <Button variant="ghost" size="tile" fullWidth aria-label={`${node.name} folder`}>
              <Row depth={depth} icon={expanded ? 'folder-open' : 'folder'} chevron={expanded}>
                <FileName name={`${node.name}/`} />
                <Size bytes={folderBytes(node)} />
              </Row>
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
        compact
        title="Files"
        divider
        action={
          <Text as="span" size="caption" color="subtle" tabular>
            {plural(files.length, 'file')} · {formatBytes(total)}
          </Text>
        }
      />
      <Stack gap="none" padding="snug">
        {renderNodes(tree, 0)}
      </Stack>
    </Card>
  );
}

function Row({
  depth,
  icon,
  chevron,
  selected = false,
  children,
}: {
  depth: number;
  icon: IconName;
  /** Undefined for a file, which takes the chevron's place so its icon lines up with a folder's. */
  chevron?: boolean;
  selected?: boolean;
  children: React.ReactNode;
}) {
  return (
    <Stack
      grow
      direction="horizontal"
      align="center"
      gap="snug"
      paddingBlock="snug"
      paddingInline="cozy"
      style={indent(depth)}
    >
      {chevron === undefined ? (
        <Stack shrink style={{ width: CHEVRON_WIDTH }} />
      ) : (
        <Icon name="chevron-right" size="sm" color="muted" rotate={chevron ? 90 : 0} label="" />
      )}
      <Icon name={icon} size="sm" color={selected ? 'accent' : 'muted'} label="" />
      {children}
    </Stack>
  );
}

function FileName({ name, selected = false }: { name: string; selected?: boolean }) {
  return (
    <Stack grow minWidth="none">
      <Text
        as="span"
        size="supporting"
        mono
        truncate
        align="start"
        title={name}
        color={selected ? 'accent' : 'foreground'}
      >
        {name}
      </Text>
    </Stack>
  );
}

function Size({ bytes }: { bytes: number }) {
  return (
    <Text as="span" size="caption" mono color="subtle" tabular>
      {formatBytes(bytes)}
    </Text>
  );
}
