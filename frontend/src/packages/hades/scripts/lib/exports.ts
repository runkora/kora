/**
 * The package's component API as the classifier sees it: every name exported
 * from `src/index.ts`, and for every exported type the names of its own
 * members. Read from the TypeScript syntax tree alone, no type checker, so it
 * works on a file set read from git at a release tag as well as from disk.
 *
 * Own members only: a props interface that extends an HTML attribute type or a
 * `VariantProps<>` contributes what it declares itself. A member removed from
 * what a consumer could write is what the classifier needs to see.
 */
import ts from 'typescript';

export interface ExportSurface {
  /** Exported names, sorted. */
  readonly exports: readonly string[];
  /** Own member names per exported type that could be resolved, sorted. */
  readonly props: Readonly<Record<string, readonly string[]>>;
}

/** Reads a package-relative path, or returns null when it does not exist. */
export type FileReader = (relPath: string) => string | null;

const ENTRY = 'src/index.ts';

export function readExportSurface(read: FileReader): ExportSurface {
  const exportsSet = new Set<string>();
  const props: Record<string, string[]> = {};
  for (const entry of collectExports(ENTRY, read, new Set())) {
    exportsSet.add(entry.name);
    if (entry.isType) {
      const members = resolveTypeMembers(entry.name, entry.file, read, new Set());
      if (members) props[entry.name] = members;
    }
  }
  return {
    exports: [...exportsSet].sort(),
    props: Object.fromEntries(Object.entries(props).sort(([a], [b]) => a.localeCompare(b))),
  };
}

interface ExportedName {
  readonly name: string;
  /** The file the name is exported from at the point it was found. */
  readonly file: string;
  readonly isType: boolean;
}

function parse(file: string, text: string): ts.SourceFile {
  return ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, scriptKind(file));
}

function scriptKind(file: string): ts.ScriptKind {
  return file.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
}

function resolveModule(fromFile: string, specifier: string, read: FileReader): string | null {
  if (!specifier.startsWith('.')) return null;
  const base = joinPosix(dirname(fromFile), specifier);
  for (const candidate of [
    base,
    `${base}.ts`,
    `${base}.tsx`,
    `${base}/index.ts`,
    `${base}/index.tsx`,
  ]) {
    if (candidate.endsWith('.ts') || candidate.endsWith('.tsx')) {
      if (read(candidate) !== null) return candidate;
    }
  }
  return null;
}

function collectExports(file: string, read: FileReader, seen: Set<string>): ExportedName[] {
  if (seen.has(file)) return [];
  seen.add(file);
  const text = read(file);
  if (text === null) return [];
  const sf = parse(file, text);
  const out: ExportedName[] = [];
  for (const stmt of sf.statements) {
    if (ts.isExportDeclaration(stmt)) {
      const target =
        stmt.moduleSpecifier && ts.isStringLiteral(stmt.moduleSpecifier)
          ? resolveModule(file, stmt.moduleSpecifier.text, read)
          : null;
      if (stmt.exportClause && ts.isNamedExports(stmt.exportClause)) {
        for (const spec of stmt.exportClause.elements) {
          out.push({
            name: spec.name.text,
            file: target ?? file,
            isType: stmt.isTypeOnly || spec.isTypeOnly,
          });
        }
      } else if (!stmt.exportClause && target) {
        out.push(...collectExports(target, read, seen));
      }
    } else if (hasExportModifier(stmt)) {
      for (const name of declaredNames(stmt)) {
        out.push({
          name,
          file,
          isType: ts.isInterfaceDeclaration(stmt) || ts.isTypeAliasDeclaration(stmt),
        });
      }
    }
  }
  return out;
}

function hasExportModifier(node: ts.Node): boolean {
  return ts.canHaveModifiers(node)
    ? (ts.getModifiers(node) ?? []).some((m) => m.kind === ts.SyntaxKind.ExportKeyword)
    : false;
}

function declaredNames(stmt: ts.Statement): string[] {
  if (ts.isVariableStatement(stmt)) {
    return stmt.declarationList.declarations.flatMap((d) =>
      ts.isIdentifier(d.name) ? [d.name.text] : [],
    );
  }
  if (
    (ts.isFunctionDeclaration(stmt) ||
      ts.isClassDeclaration(stmt) ||
      ts.isInterfaceDeclaration(stmt) ||
      ts.isTypeAliasDeclaration(stmt) ||
      ts.isEnumDeclaration(stmt)) &&
    stmt.name
  ) {
    return [stmt.name.text];
  }
  return [];
}

/**
 * Follows re-exports from `file` until the declaration of `name` is found,
 * then returns its own member names, or null when it cannot be resolved.
 */
function resolveTypeMembers(
  name: string,
  file: string,
  read: FileReader,
  seen: Set<string>,
): string[] | null {
  const key = `${file}#${name}`;
  if (seen.has(key)) return null;
  seen.add(key);
  const text = read(file);
  if (text === null) return null;
  const sf = parse(file, text);
  for (const stmt of sf.statements) {
    if (ts.isInterfaceDeclaration(stmt) && stmt.name.text === name) {
      return memberNames(stmt.members);
    }
    if (ts.isTypeAliasDeclaration(stmt) && stmt.name.text === name) {
      return typeNodeMembers(stmt.type);
    }
    if (
      ts.isExportDeclaration(stmt) &&
      stmt.moduleSpecifier &&
      ts.isStringLiteral(stmt.moduleSpecifier)
    ) {
      const target = resolveModule(file, stmt.moduleSpecifier.text, read);
      if (!target) continue;
      if (stmt.exportClause && ts.isNamedExports(stmt.exportClause)) {
        const spec = stmt.exportClause.elements.find((e) => e.name.text === name);
        if (spec) {
          const localName = spec.propertyName?.text ?? spec.name.text;
          const found = resolveTypeMembers(localName, target, read, seen);
          if (found) return found;
        }
      } else if (!stmt.exportClause) {
        const found = resolveTypeMembers(name, target, read, seen);
        if (found) return found;
      }
    }
  }
  return null;
}

function typeNodeMembers(node: ts.TypeNode): string[] | null {
  if (ts.isTypeLiteralNode(node)) return memberNames(node.members);
  if (ts.isIntersectionTypeNode(node)) {
    const own = node.types.flatMap((t) => (ts.isTypeLiteralNode(t) ? memberNames(t.members) : []));
    return own.length > 0 ? [...new Set(own)].sort() : null;
  }
  return null;
}

function memberNames(members: ts.NodeArray<ts.TypeElement>): string[] {
  const names: string[] = [];
  for (const m of members) {
    if ((ts.isPropertySignature(m) || ts.isMethodSignature(m)) && m.name) {
      if (ts.isIdentifier(m.name) || ts.isStringLiteral(m.name)) names.push(m.name.text);
    }
  }
  return names.sort();
}

function dirname(p: string): string {
  const i = p.lastIndexOf('/');
  return i === -1 ? '.' : p.slice(0, i);
}

function joinPosix(dir: string, rel: string): string {
  const parts = (dir === '.' ? [] : dir.split('/')).concat(rel.split('/'));
  const out: string[] = [];
  for (const part of parts) {
    if (part === '' || part === '.') continue;
    if (part === '..') out.pop();
    else out.push(part);
  }
  return out.join('/');
}
